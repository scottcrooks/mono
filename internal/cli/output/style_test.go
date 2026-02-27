package output

import "testing"

func TestApplyStylePlainLeavesTextUnchanged(t *testing.T) {
	if got := ApplyStyle(ModePlain, StyleSuccess, "ok"); got != "ok" {
		t.Fatalf("expected unchanged text, got %q", got)
	}
}

func TestApplyStyleInteractiveAddsANSI(t *testing.T) {
	got := ApplyStyle(ModeInteractive, StyleSuccess, "ok")
	if got == "ok" {
		t.Fatalf("expected ansi wrapper, got plain text")
	}
	if got[len(got)-len(ansiReset):] != ansiReset {
		t.Fatalf("expected reset suffix, got %q", got)
	}
}
