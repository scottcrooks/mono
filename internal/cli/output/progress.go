package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type ProgressKind uint8

const (
	ProgressTasks ProgressKind = iota
	ProgressInstall
)

type ProgressView struct {
	Kind      ProgressKind
	Percent   int
	Elapsed   time.Duration
	Remaining time.Duration
	Learning  bool
}

type ProgressOptions struct {
	Writer       io.Writer
	Mode         Mode
	IsTTY        func() bool
	Term         func() string
	Width        func() (int, error)
	TickInterval time.Duration
	Now          func() time.Time
}

type ProgressLine struct {
	mu             sync.Mutex
	options        ProgressOptions
	viewSource     func() ProgressView
	installStarted time.Time
	drawn          bool
	disabled       bool
	closed         bool
	stop           chan struct{}
	done           chan struct{}
	close          sync.Once
}

func NewProgressLine(options ProgressOptions) *ProgressLine {
	if options.Writer == nil {
		options.Writer = os.Stderr
	}
	if options.IsTTY == nil {
		options.IsTTY = func() bool {
			file, ok := options.Writer.(*os.File)
			return ok && term.IsTerminal(int(file.Fd()))
		}
	}
	if options.Term == nil {
		options.Term = func() string { return os.Getenv("TERM") }
	}
	if options.Width == nil {
		options.Width = func() (int, error) {
			file, ok := options.Writer.(*os.File)
			if !ok {
				return 0, fmt.Errorf("writer has no terminal size")
			}
			width, _, err := term.GetSize(int(file.Fd()))
			return width, err
		}
	}
	if options.TickInterval <= 0 {
		options.TickInterval = 200 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	line := &ProgressLine{options: options, stop: make(chan struct{}), done: make(chan struct{})}
	go line.tick()
	return line
}

// TerminalProgressSupported reports whether the writer currently supports a
// fixed interactive line. Width and support are checked again during redraws.
func TerminalProgressSupported(writer io.Writer, mode Mode) bool {
	if mode != ModeInteractive || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return false
	}
	width, _, err := term.GetSize(int(file.Fd()))
	return err == nil && width > 0
}

func (p *ProgressLine) tick() {
	ticker := time.NewTicker(p.options.TickInterval)
	defer ticker.Stop()
	defer close(p.done)
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.mu.Lock()
			if !p.closed && !p.disabled && p.drawn {
				p.drawLocked()
			}
			p.mu.Unlock()
		}
	}
}

func (p *ProgressLine) Update(view ProgressView) {
	p.UpdateFrom(func() ProgressView { return view })
}

// UpdateFrom samples the current task state under the line lock on every
// redraw, including timer ticks and redraws after normal output.
func (p *ProgressLine) UpdateFrom(source func() ProgressView) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.disabled {
		return
	}
	p.viewSource = source
	p.drawLocked()
}

// Print gives normal output exclusive ownership of the terminal line. The
// callback runs while the ticker and other callbacks are suspended.
func (p *ProgressLine) Print(callback func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		if callback != nil {
			callback()
		}
		return
	}
	p.clearLocked()
	if callback != nil {
		callback()
	}
	if !p.disabled {
		p.drawLocked()
	}
}

func (p *ProgressLine) Close() {
	p.close.Do(func() {
		close(p.stop)
		<-p.done
		p.mu.Lock()
		defer p.mu.Unlock()
		hadLine := p.drawn
		p.clearLocked()
		if hadLine && !p.disabled {
			if _, err := io.WriteString(p.options.Writer, "\n"); err != nil {
				p.disabled = true
			}
		}
		p.closed = true
	})
}

func (p *ProgressLine) drawLocked() {
	if p.viewSource == nil {
		return
	}
	if !p.capableLocked() {
		p.clearLocked()
		p.disabled = true
		return
	}
	width, err := p.options.Width()
	if err != nil || width < 1 {
		p.clearLocked()
		p.disabled = true
		return
	}
	view := p.viewSource()
	if view.Kind == ProgressInstall {
		if p.installStarted.IsZero() {
			p.installStarted = p.options.Now()
		}
		view.Elapsed = p.options.Now().Sub(p.installStarted)
	}
	line := formatProgressLine(view, width)
	if line == "" {
		p.clearLocked()
		p.disabled = true
		return
	}
	if p.drawn {
		if _, err := io.WriteString(p.options.Writer, "\r\x1b[2K"); err != nil {
			p.disabled = true
			p.drawn = false
			return
		}
	}
	if _, err := io.WriteString(p.options.Writer, "\r"+line); err != nil {
		p.disabled = true
		p.drawn = false
		return
	}
	p.drawn = true
}

func (p *ProgressLine) capableLocked() bool {
	return p.options.Mode == ModeInteractive && p.options.IsTTY() && !strings.EqualFold(strings.TrimSpace(p.options.Term()), "dumb")
}

func (p *ProgressLine) clearLocked() {
	if !p.drawn || p.disabled {
		return
	}
	if _, err := io.WriteString(p.options.Writer, "\r\x1b[2K"); err != nil {
		p.disabled = true
	}
	p.drawn = false
}

func formatProgressLine(view ProgressView, width int) string {
	if view.Kind == ProgressInstall {
		line := fmt.Sprintf("Installing dependencies elapsed %s", formatProgressDuration(view.Elapsed))
		if len(line) > width {
			return ""
		}
		return line
	}
	eta := formatProgressDuration(view.Remaining)
	if view.Learning {
		eta = "learning"
	}
	label := fmt.Sprintf(" elapsed %s remaining %s %d%%", formatProgressDuration(view.Elapsed), eta, view.Percent)
	available := width - len(label)
	if available < 4 {
		return ""
	}
	barWidth := available - 3
	if barWidth > 24 {
		barWidth = 24
	}
	if barWidth < 1 {
		return ""
	}
	filled := barWidth * view.Percent / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := "[" + strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled) + "]"
	return bar + label
}

func formatProgressDuration(duration time.Duration) string {
	seconds := int64(duration / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
