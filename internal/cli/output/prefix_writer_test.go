package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrefixWriterPrefixesLinesAndFlushesRemainder(t *testing.T) {
	var out bytes.Buffer
	pw := NewPrefixWriter("[svc]", &out)

	_, _ = pw.Write([]byte("line1\nline2"))
	if got := out.String(); got != "[svc] line1\n" {
		t.Fatalf("unexpected output after first write: %q", got)
	}

	if err := pw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if got := out.String(); got != "[svc] line1\n[svc] line2\n" {
		t.Fatalf("unexpected final output: %q", got)
	}
}

func TestNewServicePrefixWriterUsesColorInInteractiveMode(t *testing.T) {
	var out bytes.Buffer
	pw := NewServicePrefixWriter("mallos", &out, ModeInteractive)

	_, _ = pw.Write([]byte("line\n"))
	got := out.String()
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("expected ANSI color prefix, got %q", got)
	}
	if !strings.Contains(got, "[mallos]") || !strings.Contains(got, " line\n") {
		t.Fatalf("expected service prefix and line, got %q", got)
	}
}

func TestNewServicePrefixWriterPlainModeHasNoAnsi(t *testing.T) {
	var out bytes.Buffer
	pw := NewServicePrefixWriter("mallos", &out, ModePlain)

	_, _ = pw.Write([]byte("line\n"))
	got := out.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("did not expect ANSI color prefix in plain mode, got %q", got)
	}
	if got != "[mallos] line\n" {
		t.Fatalf("unexpected plain output: %q", got)
	}
}

func TestColorizeServicePrefixDeterministic(t *testing.T) {
	first := colorizeServicePrefix("[mallos]", "mallos", ModeInteractive)
	second := colorizeServicePrefix("[mallos]", "mallos", ModeInteractive)
	if first != second {
		t.Fatalf("expected deterministic colorized prefix: %q != %q", first, second)
	}
}
