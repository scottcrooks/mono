package runtimecmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
	"golang.org/x/term"
)

type devSelection struct {
	background []Service
	tui        *Service
}

func classifyDevServices(services []Service) (devSelection, error) {
	selection := devSelection{}
	var tuis []string
	for i := range services {
		svc := services[i]
		if svc.Archetype == "ts-ink" || svc.Archetype == "go-tui" {
			selection.tui = &svc
			tuis = append(tuis, svc.Name)
			continue
		}
		selection.background = append(selection.background, svc)
	}
	if len(tuis) > 1 {
		return devSelection{}, fmt.Errorf("dev supports one TUI at a time; selected multiple TUIs: %s", strings.Join(tuis, ", "))
	}
	return selection, nil
}

func preflightDevSelection(selection devSelection, stdin, stdout, stderr *os.File) error {
	if selection.tui == nil {
		return nil
	}
	return preflightTUI(stdin, stdout, stderr)
}

func preflightTUI(stdin, stdout, stderr *os.File) error {
	if !processGroupsSupported() {
		return fmt.Errorf("TUI dev takeover is unsupported on this platform")
	}
	for _, item := range []struct {
		name string
		file *os.File
	}{{"stdin", stdin}, {"stdout", stdout}, {"stderr", stderr}} {
		name, file := item.name, item.file
		if file == nil || !term.IsTerminal(int(file.Fd())) {
			return fmt.Errorf("TUI dev takeover requires terminal %s; run mono dev from an interactive terminal", name)
		}
	}
	return nil
}

type devTUIRuntime struct {
	stdin          *os.File
	stdout         *os.File
	stderr         *os.File
	root           string
	isTerminal     func(*os.File) bool
	snapshot       func(*os.File) (func() error, error)
	commandFactory func(context.Context, []string) (*exec.Cmd, error)
	createLog      func(string) (io.WriteCloser, string, error)
	signals        <-chan os.Signal
}

func runTUIDev(background []Service, tui Service) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	return runTUIDevWithRuntime(background, tui, devTUIRuntime{
		stdin:          os.Stdin,
		stdout:         os.Stdout,
		stderr:         os.Stderr,
		root:           root,
		isTerminal:     func(f *os.File) bool { return f != nil && term.IsTerminal(int(f.Fd())) },
		snapshot:       snapshotTerminal,
		commandFactory: commandFromParts,
		signals:        signals,
	})
}

func runTUIDevWithRuntime(background []Service, tui Service, rt devTUIRuntime) error {
	if !processGroupsSupported() {
		return fmt.Errorf("TUI dev takeover is unsupported on this platform")
	}
	for _, item := range []struct {
		name string
		file *os.File
	}{{"stdin", rt.stdin}, {"stdout", rt.stdout}, {"stderr", rt.stderr}} {
		name, file := item.name, item.file
		if file == nil || rt.isTerminal == nil || !rt.isTerminal(file) {
			return fmt.Errorf("TUI dev takeover requires terminal %s; run mono dev from an interactive terminal", name)
		}
	}
	if rt.snapshot == nil || rt.commandFactory == nil || rt.root == "" {
		return fmt.Errorf("TUI dev takeover is not fully configured")
	}

	terminalRestore, err := rt.snapshot(rt.stdin)
	if err != nil {
		return fmt.Errorf("snapshot terminal state: %w", err)
	}
	if terminalRestore == nil {
		return fmt.Errorf("snapshot terminal state returned no restore function")
	}

	prepared, err := prepareDevChildren(background, &tui, rt)
	if err != nil {
		return errors.Join(err, terminalRestore())
	}
	createLog := rt.createLog
	if createLog == nil {
		createLog = func(root string) (io.WriteCloser, string, error) {
			return createDevSessionLog(root)
		}
	}
	logFile, logPath, err := createLog(rt.root)
	if err != nil {
		return errors.Join(err, terminalRestore())
	}
	logSink := newSessionLog(logFile)
	closeLog := func() error {
		if logFile == nil {
			return nil
		}
		err := logFile.Close()
		logFile = nil
		return err
	}
	printer := output.NewPrinter(rt.stdout, rt.stderr)
	printer.StepInfo("dev", "TUI session log: "+logPath)
	writeSessionLog(logSink, "[mono] session started; log: %s\n", logPath)
	if err := logSink.Err(); err != nil {
		restoreErr := terminalRestore()
		closeErr := closeLog()
		printer.StepInfo("dev", "TUI session log: "+logPath)
		return errors.Join(fmt.Errorf("write TUI session log: %w", err), restoreErr, closeErr)
	}

	children := make([]*devChild, 0, len(prepared))
	backgroundChildren := make([]*devChild, 0, len(background))
	var foreground *devChild
	events := make(chan devChildResult, len(prepared))
	for i, item := range prepared {
		isTUI := i == len(prepared)-1
		child := &devChild{service: item.service, cmd: item.cmd, isTUI: isTUI}
		if isTUI {
			child.cmd.Stdin = rt.stdin
			child.cmd.Stdout = rt.stdout
			child.cmd.Stderr = rt.stderr
			foreground = child
		} else {
			child.stdout = output.NewPrefixWriter("["+child.service.Name+"]", logSink)
			child.stderr = output.NewPrefixWriter("["+child.service.Name+"]", logSink)
			child.cmd.Stdout = child.stdout
			child.cmd.Stderr = child.stderr
		}
		children = append(children, child)
		if !isTUI {
			backgroundChildren = append(backgroundChildren, child)
		}
	}

	for _, child := range backgroundChildren {
		writeSessionLog(logSink, "[mono] starting background service %s\n", child.service.Name)
		if err := logSink.Err(); err != nil {
			restoreErr := terminalRestore()
			closeErr := closeLog()
			printer.StepInfo("dev", "TUI session log: "+logPath)
			return errors.Join(fmt.Errorf("write TUI session log: %w", err), restoreErr, closeErr)
		}
		if err := child.cmd.Start(); err != nil {
			cleanupErr := stopDevChildren(children[:len(backgroundChildren)], nil, events, logSink)
			restoreErr := terminalRestore()
			writeSessionLog(logSink, "[mono] session ended after startup failure\n")
			closeErr := closeLog()
			printer.StepInfo("dev", "TUI session log: "+logPath)
			return errors.Join(fmt.Errorf("[%s] failed to start: %w", child.service.Name, err), cleanupErr, restoreErr, logSink.Err(), closeErr)
		}
		startDevChild(child, events, logSink)
		if err := logSink.Err(); err != nil {
			cleanupErr := stopDevChildren(children[:len(backgroundChildren)], nil, events, logSink)
			restoreErr := terminalRestore()
			closeErr := closeLog()
			printer.StepInfo("dev", "TUI session log: "+logPath)
			return errors.Join(fmt.Errorf("write TUI session log: %w", err), cleanupErr, restoreErr, closeErr)
		}
	}

	writeSessionLog(logSink, "[mono] starting TUI service %s\n", tui.Name)
	if err := logSink.Err(); err != nil {
		cleanupErr := stopDevChildren(backgroundChildren, nil, events, logSink)
		restoreErr := terminalRestore()
		closeErr := closeLog()
		printer.StepInfo("dev", "TUI session log: "+logPath)
		return errors.Join(fmt.Errorf("write TUI session log: %w", err), cleanupErr, restoreErr, closeErr)
	}
	if err := foreground.cmd.Start(); err != nil {
		cleanupErr := stopDevChildren(backgroundChildren, nil, events, logSink)
		restoreErr := terminalRestore()
		writeSessionLog(logSink, "[mono] session ended after TUI startup failure\n")
		closeErr := closeLog()
		printer.StepInfo("dev", "TUI session log: "+logPath)
		return errors.Join(fmt.Errorf("[%s] failed to start: %w", tui.Name, err), cleanupErr, restoreErr, logSink.Err(), closeErr)
	}
	startDevChild(foreground, events, logSink)

	completed := make(map[*devChild]devChildResult, len(children))
	var failure error
	interrupted := false
	var receivedSignal os.Signal
	stop := false
	for !stop {
		select {
		case result := <-events:
			completed[result.child] = result
			if result.child.isTUI {
				if result.err != nil {
					if expectedSignalExit(result.err) {
						interrupted = true
					} else {
						failure = fmt.Errorf("[%s] exited with error: %w", result.child.service.Name, result.err)
					}
				}
				stop = true
			} else if result.err != nil {
				failure = fmt.Errorf("[%s] exited with error: %w", result.child.service.Name, result.err)
				stop = true
			}
		case receivedSignal = <-rt.signals:
			writeSessionLog(logSink, "[mono] received %s; stopping session\n", receivedSignal)
			interrupted = true
			stop = true
		case err := <-logSink.Failures():
			failure = fmt.Errorf("write TUI session log: %w", err)
			stop = true
		}
	}

	var tuiSignal *syscall.Signal
	if !interrupted || receivedSignal == syscall.SIGTERM {
		interrupt := syscall.SIGINT
		if interrupted {
			interrupt = syscall.SIGTERM
		}
		tuiSignal = &interrupt
	}
	cleanupErr := stopDevChildrenWithSignal(children, completed, events, logSink, tuiSignal)
	restoreErr := terminalRestore()
	writeSessionLog(logSink, "[mono] session ended\n")
	closeErr := closeLog()
	if !interrupted {
		if failure != nil {
			printer.StepErr("dev", failure.Error())
		}
		if cleanupErr != nil {
			printer.StepErr("dev", "cleanup failed: "+cleanupErr.Error())
		}
		if restoreErr != nil {
			printer.StepErr("dev", "terminal restore failed: "+restoreErr.Error())
		}
		if closeErr != nil {
			printer.StepErr("dev", "session log close failed: "+closeErr.Error())
		}
		if logErr := logSink.Err(); logErr != nil {
			printer.StepErr("dev", "session log failed: "+logErr.Error())
		}
	}
	printer.StepInfo("dev", "TUI session log: "+logPath)
	if interrupted {
		return errors.Join(cleanupErr, restoreErr, closeErr, logSink.Err())
	}
	return errors.Join(failure, cleanupErr, restoreErr, closeErr, logSink.Err())
}

type preparedDevChild struct {
	service Service
	cmd     *exec.Cmd
}

func prepareDevChildren(background []Service, tui *Service, rt devTUIRuntime) ([]preparedDevChild, error) {
	services := append(append([]Service(nil), background...), *tui)
	prepared := make([]preparedDevChild, 0, len(services))
	for i, svc := range services {
		command, ok := resolveDevCommand(svc)
		if !ok {
			return nil, fmt.Errorf("no 'dev' command defined for service %s (no default for archetype %q)", svc.Name, svc.Archetype)
		}
		servicePath := svc.Path
		if !filepath.IsAbs(servicePath) {
			servicePath = filepath.Join(rt.root, servicePath)
		}
		absPath, err := filepath.Abs(servicePath)
		if err != nil {
			return nil, fmt.Errorf("[%s] resolve path: %w", svc.Name, err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("[%s] inspect service directory: %w", svc.Name, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("[%s] service path is not a directory: %s", svc.Name, absPath)
		}
		cmd, err := rt.commandFactory(context.Background(), strings.Fields(command))
		if err != nil {
			return nil, fmt.Errorf("[%s] %w", svc.Name, err)
		}
		cmd.Dir = absPath
		cmd.Env, err = devEnvironment(rt.root, absPath, cmd.Environ())
		if err != nil {
			return nil, fmt.Errorf("[%s] %w", svc.Name, err)
		}
		cmd.WaitDelay = time.Second
		if i < len(services)-1 {
			if err := configureBackgroundProcess(cmd); err != nil {
				return nil, fmt.Errorf("[%s] configure process group: %w", svc.Name, err)
			}
		}
		prepared = append(prepared, preparedDevChild{service: svc, cmd: cmd})
	}
	return prepared, nil
}

type devChild struct {
	service Service
	cmd     *exec.Cmd
	isTUI   bool
	stdout  *output.PrefixWriter
	stderr  *output.PrefixWriter
}

type devChildResult struct {
	child *devChild
	err   error
}

func startDevChild(child *devChild, events chan<- devChildResult, logSink *sessionLog) {
	go func() {
		err := child.cmd.Wait()
		if child.stdout != nil {
			if flushErr := child.stdout.Flush(); flushErr != nil {
				err = errors.Join(err, flushErr)
			}
		}
		if child.stderr != nil {
			if flushErr := child.stderr.Flush(); flushErr != nil {
				err = errors.Join(err, flushErr)
			}
		}
		writeSessionLog(logSink, "[mono] service %s exited: %s\n", child.service.Name, exitDescription(err))
		events <- devChildResult{child: child, err: err}
	}()
}

func exitDescription(err error) string {
	if err == nil {
		return "success"
	}
	return err.Error()
}

func stopDevChildren(children []*devChild, completed map[*devChild]devChildResult, events <-chan devChildResult, logSink *sessionLog) error {
	interrupt := syscall.SIGINT
	return stopDevChildrenWithSignal(children, completed, events, logSink, &interrupt)
}

func stopDevChildrenWithSignal(children []*devChild, completed map[*devChild]devChildResult, events <-chan devChildResult, logSink *sessionLog, tuiSignal *syscall.Signal) error {
	if completed == nil {
		completed = make(map[*devChild]devChildResult, len(children))
	}
	var failures []error
	recordResultError := func(result devChildResult) {
		if result.err != nil && !expectedSignalExit(result.err) {
			failures = append(failures, fmt.Errorf("[%s] exited with error: %w", result.child.service.Name, result.err))
		}
	}
	for {
		select {
		case result := <-events:
			completed[result.child] = result
			recordResultError(result)
		default:
			goto drained
		}
	}

drained:
	var pending []*devChild
	for _, child := range children {
		if child.cmd.Process == nil {
			continue
		}
		_, done := completed[child]
		if done && (child.isTUI || !devProcessGroupExists(child.cmd.Process.Pid)) {
			continue
		}
		if child.isTUI && tuiSignal == nil {
			pending = append(pending, child)
			continue
		}
		childSignal := syscall.SIGINT
		if child.isTUI {
			childSignal = *tuiSignal
		}
		if err := signalDevChild(child, childSignal); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			failures = append(failures, fmt.Errorf("signal %s: %w", child.service.Name, err))
		}
		pending = append(pending, child)
	}
	if len(pending) > 0 {
		timer := time.NewTimer(5 * time.Second)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for len(pending) > 0 {
			pending = liveDevChildren(pending, completed)
			if len(pending) == 0 {
				break
			}
			select {
			case result := <-events:
				completed[result.child] = result
				recordResultError(result)
			case <-ticker.C:
			case <-timer.C:
				goto force
			}
		}
		timer.Stop()
	}
	return errors.Join(append(failures, logSink.Err())...)

force:
	for _, child := range pending {
		if err := signalDevChild(child, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			failures = append(failures, fmt.Errorf("kill %s: %w", child.service.Name, err))
		}
	}
	unreaped := 0
	for _, child := range pending {
		if _, done := completed[child]; !done {
			unreaped++
		}
	}
	for unreaped > 0 {
		result := <-events
		if _, done := completed[result.child]; !done {
			completed[result.child] = result
			unreaped--
		}
	}
	return errors.Join(append(failures, logSink.Err())...)
}

func liveDevChildren(children []*devChild, completed map[*devChild]devChildResult) []*devChild {
	live := children[:0]
	for _, child := range children {
		_, done := completed[child]
		if !done || (!child.isTUI && devProcessGroupExists(child.cmd.Process.Pid)) {
			live = append(live, child)
		}
	}
	return live
}

func createDevSessionLog(root string) (*os.File, string, error) {
	logDir := filepath.Join(root, ".cache", "dev")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create dev log directory: %w", err)
	}
	file, err := os.CreateTemp(logDir, "session-*.log")
	if err != nil {
		return nil, "", fmt.Errorf("create dev session log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf("secure dev session log: %w", err)
	}
	absPath, err := filepath.Abs(file.Name())
	if err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf("resolve dev session log path: %w", err)
	}
	return file, absPath, nil
}

type sessionLog struct {
	mu       sync.Mutex
	writer   io.Writer
	err      error
	failures chan error
}

func newSessionLog(writer io.Writer) *sessionLog {
	return &sessionLog{writer: writer, failures: make(chan error, 1)}
}

func (l *sessionLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil && l.err == nil {
		l.err = err
		l.failures <- err
	}
	return n, err
}

func (l *sessionLog) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *sessionLog) Failures() <-chan error { return l.failures }

func writeSessionLog(logSink *sessionLog, format string, args ...any) {
	_, _ = fmt.Fprintf(logSink, format, args...)
}

func snapshotTerminal(file *os.File) (func() error, error) {
	state, err := term.GetState(int(file.Fd()))
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(int(file.Fd()), state) }, nil
}
