package tasks

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func TestConvertGoTestJSON(t *testing.T) {
	data := "" +
		`{"Action":"run","Package":"example.com/p","Test":"TestPass"}` + "\n" +
		`{"Action":"output","Package":"example.com/p","Test":"TestPass","Output":"pass output\n"}` + "\n" +
		`{"Action":"pass","Package":"example.com/p","Test":"TestPass","Elapsed":0.25}` + "\n" +
		`{"Action":"run","Package":"example.com/p","Test":"TestFail"}` + "\n" +
		`{"Action":"output","Package":"example.com/p","Test":"TestFail","Output":"assertion failed\n"}` + "\n" +
		`{"Action":"fail","Package":"example.com/p","Test":"TestFail","Elapsed":0.5}` + "\n" +
		`{"Action":"run","Package":"example.com/p","Test":"TestSkip"}` + "\n" +
		`{"Action":"skip","Package":"example.com/p","Test":"TestSkip","Elapsed":0.01}` + "\n" +
		`{"Action":"fail","Package":"example.com/p","Elapsed":0.76}` + "\n"
	report, err := convertGoTestJSON([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Suites) != 1 || report.Suites[0].Tests != 3 || report.Suites[0].Failures != 1 || report.Suites[0].Skipped != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	encoded, err := xml.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("assertion failed")) || !bytes.Contains(encoded, []byte("pass output")) {
		t.Fatalf("report lost test output: %s", encoded)
	}
}

func TestConvertGoTestJSONRejectsIncompleteEvents(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not json\n"), []byte(`{"Action":"run","Package":"p","Test":"T"}`)} {
		if _, err := convertGoTestJSON(data); err == nil {
			t.Fatalf("expected invalid stream rejection for %q", data)
		}
	}
}

func TestConvertGoTestJSONReportsPackageBuildFailure(t *testing.T) {
	data := []byte("{\"Action\":\"output\",\"Package\":\"example.com/bad\",\"Output\":\"syntax error\\n\"}\n{\"Action\":\"fail\",\"Package\":\"example.com/bad\",\"Elapsed\":0.1}\n")
	report, err := convertGoTestJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if report.Suites[0].Failures != 1 || report.Suites[0].Cases[0].Name != "package build or setup" || !strings.Contains(report.Suites[0].Cases[0].Failure.Body, "syntax error") {
		t.Fatalf("package-level failure missing from report: %+v", report)
	}
}

func TestWriteGoTestJUnitRejectsWriteErrors(t *testing.T) {
	svc := Service{Name: "fixture", Path: "/tmp/fixture"}
	data := []byte("{\"Action\":\"pass\",\"Package\":\"p\"}\n")
	if err := writeGoTestJUnit(filepath.Join(t.TempDir(), "missing"), svc, data); err == nil {
		t.Fatal("expected output write failure")
	}
}

func TestRunTaskCommandWritesJUnitForFailedGoTest(t *testing.T) {
	dir := t.TempDir()
	reports := filepath.Join(dir, "reports")
	if err := os.Mkdir(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCACHE", filepath.Join(dir, "gocache"))
	if err := os.Mkdir(filepath.Join(dir, "gocache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/reportfixture\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `package reportfixture
import "testing"
func TestPass(t *testing.T) { t.Log("pass detail") }
func TestSkip(t *testing.T) { t.Skip("skip detail") }
func TestFail(t *testing.T) { t.Error("failure detail") }
`
	if err := os.WriteFile(filepath.Join(dir, "report_test.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := Service{Name: "fixture", Path: dir, Archetype: "go"}
	node := TaskNode{Service: "fixture", Task: TaskTest}
	var stdout, stderr bytes.Buffer
	cmd := commandForExecution(svc, node, "go test ./...", nil, TaskRunOptions{NoCache: true, JUnitDir: reports})
	if !strings.Contains(cmd, "go test -json") || !strings.Contains(cmd, "-count=1") {
		t.Fatalf("unexpected command: %s", cmd)
	}
	err := runTaskCommand(context.Background(), output.NewPrinterWithMode(&stdout, &stderr, output.ModePlain), svc, node, cmd, &stdout, &stderr, TaskRunOptions{JUnitDir: reports})
	if err == nil {
		t.Fatal("expected Go test failure")
	}
	entries, readErr := os.ReadDir(reports)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("report entries=%v err=%v", entries, readErr)
	}
	data, readErr := os.ReadFile(filepath.Join(reports, entries[0].Name()))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var report junitReport
	if err := xml.Unmarshal(data, &report); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, data)
	}
	if len(report.Suites) != 1 || report.Suites[0].Tests != 3 || report.Suites[0].Failures != 1 || report.Suites[0].Skipped != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestConcurrentGoTestReportsDoNotOverwrite(t *testing.T) {
	base := t.TempDir()
	reports := filepath.Join(base, "reports")
	if err := os.Mkdir(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, name := range []string{"alpha", "beta"} {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir := filepath.Join(base, name)
			if err := os.Mkdir(dir, 0o755); err != nil {
				errCh <- err
				return
			}
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(fmt.Sprintf("module example.com/%s\n\ngo 1.26\n", name)), 0o600); err != nil {
				errCh <- err
				return
			}
			if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte("package "+name+"\nfunc Value() int { return 1 }\n"), 0o600); err != nil {
				errCh <- err
				return
			}
			svc := Service{Name: name, Path: dir, Archetype: "go"}
			node := TaskNode{Service: name, Task: TaskTest}
			var out bytes.Buffer
			cmd := commandForExecution(svc, node, "go test ./...", nil, TaskRunOptions{NoCache: true, JUnitDir: reports})
			errCh <- runTaskCommand(context.Background(), output.NewPrinterWithMode(&out, &out, output.ModePlain), svc, node, cmd, &out, &out, TaskRunOptions{JUnitDir: reports})
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(reports)
	if err != nil || len(entries) != 2 {
		t.Fatalf("reports=%v err=%v", entries, err)
	}
}
