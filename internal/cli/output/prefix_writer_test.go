package output

import (
	"bytes"
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
