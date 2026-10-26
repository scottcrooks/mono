package output

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func progressTestOptions(writer io.Writer) ProgressOptions {
	return ProgressOptions{Writer: writer, Mode: ModeInteractive, IsTTY: func() bool { return true }, Term: func() string { return "xterm" }, Width: func() (int, error) { return 100, nil }, TickInterval: time.Hour}
}

func TestFormatProgressLineAndDuration(t *testing.T) {
	line := formatProgressLine(ProgressView{Percent: 50, Elapsed: 65 * time.Second, Remaining: 3600 * time.Second}, 100)
	if !strings.Contains(line, "[############") || !strings.Contains(line, "elapsed 1:05") || !strings.Contains(line, "remaining 1:00:00") || !strings.Contains(line, "50%") {
		t.Fatalf("unexpected progress line: %q", line)
	}
	if got := formatProgressLine(ProgressView{Kind: ProgressInstall, Elapsed: 5 * time.Second}, 80); got != "Installing dependencies elapsed 0:05" {
		t.Fatalf("install line: %q", got)
	}
	if got := formatProgressLine(ProgressView{Kind: ProgressInstall}, 10); got != "" {
		t.Fatalf("short install line should disable: %q", got)
	}
	if got := formatProgressDuration(59 * time.Second); got != "0:59" {
		t.Fatalf("duration: %s", got)
	}
}

func TestInstallProgressElapsedUsesInjectedClockAndNoETA(t *testing.T) {
	var output bytes.Buffer
	now := time.Unix(0, 0)
	options := progressTestOptions(&output)
	options.Now = func() time.Time { return now }
	line := NewProgressLine(options)
	line.Update(ProgressView{Kind: ProgressInstall})
	first := output.String()
	now = now.Add(65 * time.Second)
	line.Update(ProgressView{Kind: ProgressInstall})
	line.Close()
	text := output.String()
	if !strings.Contains(first, "elapsed 0:00") || !strings.Contains(text, "elapsed 1:05") || strings.Contains(text, "remaining") || strings.Contains(text, "ETA") {
		t.Fatalf("install status should show elapsed only: %q", text)
	}
}

func TestProgressLineGatesPipePlainDumbAndForcedColorPipe(t *testing.T) {
	for name, configure := range map[string]func(*ProgressOptions){
		"plain":             func(o *ProgressOptions) { o.Mode = ModePlain },
		"pipe":              func(o *ProgressOptions) { o.IsTTY = func() bool { return false } },
		"dumb":              func(o *ProgressOptions) { o.Term = func() string { return "dumb" } },
		"forced color pipe": func(o *ProgressOptions) { o.Mode = ModeInteractive; o.IsTTY = func() bool { return false } },
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			options := progressTestOptions(&output)
			configure(&options)
			line := NewProgressLine(options)
			line.Update(ProgressView{Percent: 10})
			line.Close()
			if output.Len() != 0 {
				t.Fatalf("ineligible renderer wrote controls: %q", output.String())
			}
		})
	}
}

func TestProgressLinePrintOwnsTranscriptAndCloseIsIdempotent(t *testing.T) {
	var output bytes.Buffer
	line := NewProgressLine(progressTestOptions(&output))
	line.Update(ProgressView{Percent: 25, Elapsed: time.Second, Remaining: time.Second})
	line.Print(func() { _, _ = io.WriteString(&output, "TRANSCRIPT\n") })
	line.Close()
	line.Close()
	text := output.String()
	clear := strings.Index(text, "\r\x1b[2K")
	replay := strings.Index(text, "TRANSCRIPT")
	if clear < 0 || replay < clear || !strings.Contains(text[replay:], "[######") {
		t.Fatalf("unexpected output ordering: %q", text)
	}
	if strings.Count(text, "TRANSCRIPT") != 1 || !strings.HasSuffix(text, "\n") {
		t.Fatalf("close/output count: %q", text)
	}
}

func TestProgressLinePrintBeforeFirstViewDoesNotDrawEmptyPlan(t *testing.T) {
	var output bytes.Buffer
	line := NewProgressLine(progressTestOptions(&output))
	line.Print(func() { _, _ = io.WriteString(&output, "warning\n") })
	line.Close()
	if got := output.String(); got != "warning\n" {
		t.Fatalf("normal output drew an unstarted progress line: %q", got)
	}
}

func TestProgressLineWriterAndWidthFailuresDisableRendering(t *testing.T) {
	var output bytes.Buffer
	options := progressTestOptions(&output)
	options.Width = func() (int, error) { return 2, nil }
	line := NewProgressLine(options)
	line.Update(ProgressView{Percent: 1})
	line.Close()
	if output.Len() != 0 {
		t.Fatalf("too narrow terminal wrote output: %q", output.String())
	}

	failed := &errorWriter{}
	options = progressTestOptions(failed)
	line = NewProgressLine(options)
	line.Update(ProgressView{Percent: 1})
	line.Print(func() { failed.callback = true })
	line.Close()
	if !failed.callback {
		t.Fatal("writer failure interrupted normal output callback")
	}
}

func TestProgressLineDisablesWhenTerminalSupportDisappears(t *testing.T) {
	var output bytes.Buffer
	tty := true
	width := 80
	options := progressTestOptions(&output)
	options.IsTTY = func() bool { return tty }
	options.Width = func() (int, error) { return width, nil }
	line := NewProgressLine(options)
	line.Update(ProgressView{Percent: 20})
	if output.Len() == 0 {
		t.Fatal("initial progress line was not drawn")
	}
	width = 1
	line.Update(ProgressView{Percent: 21})
	before := output.Len()
	line.Update(ProgressView{Percent: 22})
	line.Close()
	if output.Len() != before {
		t.Fatal("renderer kept writing after width support disappeared")
	}
	if !strings.Contains(output.String(), "\x1b[2K") {
		t.Fatalf("active line was not cleared: %q", output.String())
	}
}

func TestProgressLineTicksAndStopsAfterClose(t *testing.T) {
	var output synchronizedBuffer
	options := progressTestOptions(&output)
	options.TickInterval = 5 * time.Millisecond
	line := NewProgressLine(options)
	line.Update(ProgressView{Percent: 20})
	initial := output.Len()
	time.Sleep(25 * time.Millisecond)
	if output.Len() <= initial {
		t.Fatal("ticker did not redraw active progress")
	}
	line.Close()
	closed := output.Len()
	line.Update(ProgressView{Percent: 30})
	time.Sleep(10 * time.Millisecond)
	if output.Len() != closed {
		t.Fatal("renderer wrote after close")
	}
}

type synchronizedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *synchronizedBuffer) WriteString(value string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.WriteString(value)
}

func (b *synchronizedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Len()
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

type errorWriter struct{ callback bool }

func (*errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestProgressLineSerializesConcurrentTranscripts(t *testing.T) {
	var output bytes.Buffer
	line := NewProgressLine(progressTestOptions(&output))
	line.Update(ProgressView{Percent: 10})
	var wg sync.WaitGroup
	for _, value := range []string{"ONE", "TWO"} {
		value := value
		wg.Add(1)
		go func() {
			defer wg.Done()
			line.Print(func() {
				_, _ = io.WriteString(&output, value+"-START ")
				time.Sleep(time.Millisecond)
				_, _ = io.WriteString(&output, value+"-END\n")
			})
		}()
	}
	wg.Wait()
	line.Close()
	text := output.String()
	if !(strings.Index(text, "ONE-START ONE-END") < strings.Index(text, "TWO-START TWO-END") || strings.Index(text, "TWO-START TWO-END") < strings.Index(text, "ONE-START ONE-END")) {
		t.Fatalf("transcripts interleaved: %q", text)
	}
}
