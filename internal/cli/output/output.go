package output

import (
	"fmt"
	"io"
	"os"
)

// Printer prints normalized CLI status output with mode-aware styling.
type Printer interface {
	Mode() Mode
	Blank()
	Section(title string)
	StepStart(scope, msg string)
	StepInfo(scope, msg string)
	StepOK(scope, msg string)
	StepWarn(scope, msg string)
	StepErr(scope, msg string)
	Summary(msg string)
}

type terminalPrinter struct {
	out  io.Writer
	err  io.Writer
	mode Mode
}

func DefaultPrinter() Printer {
	return NewPrinter(os.Stdout, os.Stderr)
}

func NewPrinter(out io.Writer, err io.Writer) Printer {
	return NewPrinterWithMode(out, err, DetectMode(out))
}

func NewPrinterWithMode(out io.Writer, err io.Writer, mode Mode) Printer {
	return &terminalPrinter{out: out, err: err, mode: mode}
}

func (p *terminalPrinter) Mode() Mode {
	return p.mode
}

func (p *terminalPrinter) Blank() {
	fmt.Fprintln(p.out)
}

func (p *terminalPrinter) Section(title string) {
	fmt.Fprintln(p.out, ApplyStyle(p.mode, StyleInfo, "==> "+title))
}

func (p *terminalPrinter) StepStart(scope, msg string) {
	fmt.Fprintln(p.out, ApplyStyle(p.mode, StyleInfo, "==> "+formatScope(scope, msg)))
}

func (p *terminalPrinter) StepInfo(scope, msg string) {
	prefix := "[info] "
	if p.mode == ModeInteractive {
		prefix = "ℹ "
	}
	fmt.Fprintln(p.out, ApplyStyle(p.mode, StyleInfo, prefix+formatScope(scope, msg)))
}

func (p *terminalPrinter) StepOK(scope, msg string) {
	prefix := "[ok] "
	if p.mode == ModeInteractive {
		prefix = "✓ "
	}
	fmt.Fprintln(p.out, ApplyStyle(p.mode, StyleSuccess, prefix+formatScope(scope, msg)))
}

func (p *terminalPrinter) StepWarn(scope, msg string) {
	prefix := "[warn] "
	if p.mode == ModeInteractive {
		prefix = "⚠ "
	}
	fmt.Fprintln(p.out, ApplyStyle(p.mode, StyleWarn, prefix+formatScope(scope, msg)))
}

func (p *terminalPrinter) StepErr(scope, msg string) {
	prefix := "[err] "
	if p.mode == ModeInteractive {
		prefix = "✗ "
	}
	fmt.Fprintln(p.err, ApplyStyle(p.mode, StyleError, prefix+formatScope(scope, msg)))
}

func (p *terminalPrinter) Summary(msg string) {
	fmt.Fprintln(p.out, ApplyStyle(p.mode, StyleMuted, msg))
}

func formatScope(scope, msg string) string {
	if scope == "" {
		return msg
	}
	if msg == "" {
		return "[" + scope + "]"
	}
	return "[" + scope + "] " + msg
}
