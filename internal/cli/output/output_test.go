package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrinterPlainFormatting(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	p := NewPrinterWithMode(&out, &errOut, ModePlain)

	p.Section("build")
	p.StepStart("api:test", "start")
	p.StepInfo("api:test", "info")
	p.StepOK("api:test", "completed")
	p.StepWarn("api:test", "cached")
	p.StepErr("api:test", "failed")
	p.Summary("Task summary: succeeded=1 failed=1 skipped=0")

	stdout := out.String()
	stderr := errOut.String()
	for _, want := range []string{"==> build", "==> [api:test] start", "[info] [api:test] info", "[ok] [api:test] completed", "[warn] [api:test] cached", "Task summary:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in stdout, got %q", want, stdout)
		}
	}
	if !strings.Contains(stderr, "[err] [api:test] failed") {
		t.Fatalf("expected error line in stderr, got %q", stderr)
	}
}
