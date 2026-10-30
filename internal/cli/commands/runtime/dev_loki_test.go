package runtimecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type capturedLokiPush struct {
	Streams []struct {
		Stream map[string]string `json:"stream"`
		Values [][2]string       `json:"values"`
	} `json:"streams"`
}

type lokiReceiver struct {
	mu     sync.Mutex
	values map[string][]string
	count  int
	errors []string
}

func (r *lokiReceiver) serve(w http.ResponseWriter, req *http.Request) {
	var push capturedLokiPush
	var problem string
	if req.Method != http.MethodPost || req.URL.Path != "/loki/api/v1/push" || req.Header.Get("Content-Type") != "application/json" {
		problem = "wrong request method, path or content type"
	}
	if err := json.NewDecoder(req.Body).Decode(&push); err != nil {
		problem = err.Error()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	if problem != "" {
		r.errors = append(r.errors, problem)
	}
	if len(push.Streams) != 1 || len(push.Streams[0].Stream) != 1 || push.Streams[0].Stream["job"] == "" || len(push.Streams[0].Values) != 1 {
		r.errors = append(r.errors, "invalid stream shape")
		w.WriteHeader(400)
		return
	}
	item := push.Streams[0].Values[0]
	ts, err := strconv.ParseInt(item[0], 10, 64)
	if err != nil || ts < time.Now().Add(-time.Minute).UnixNano() || ts > time.Now().Add(time.Minute).UnixNano() {
		r.errors = append(r.errors, "invalid timestamp "+item[0])
	}
	job := push.Streams[0].Stream["job"]
	r.values[job] = append(r.values[job], item[1])
	w.WriteHeader(http.StatusNoContent)
}
func (r *lokiReceiver) assert(t *testing.T, expected map[string][]string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.errors) != 0 {
		t.Fatalf("bad requests: %v", r.errors)
	}
	for _, lines := range r.values {
		sort.Strings(lines)
	}
	for _, lines := range expected {
		sort.Strings(lines)
	}
	if !reflect.DeepEqual(r.values, expected) {
		t.Fatalf("captured %v, want %v", r.values, expected)
	}
}

func TestLokiLineWriterConcurrentStreams(t *testing.T) {
	receiver := &lokiReceiver{values: make(map[string][]string)}
	server := httptest.NewServer(http.HandlerFunc(receiver.serve))
	defer server.Close()
	sink := newLokiSink(server.URL)
	var wg sync.WaitGroup
	for _, job := range []string{"alpha", "beta"} {
		for _, stream := range []string{"out", "err"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := &lokiLineWriter{sink: sink, job: job}
				_, _ = w.Write([]byte(stream + "-spl"))
				_, _ = w.Write([]byte("it\n\n" + stream + "-fragment\r"))
				w.Flush()
				w.Flush()
			}()
		}
	}
	wg.Wait()
	want := []string{"out-split", "", "out-fragment\r", "err-split", "", "err-fragment\r"}
	receiver.assert(t, map[string][]string{"alpha": append([]string(nil), want...), "beta": append([]string(nil), want...)})
}

const devLogFixture = `package main
import ("fmt"; "os")
func main() { fmt.Fprint(os.Stdout, "out-"); fmt.Fprint(os.Stdout, "line\n\nout-tail"); fmt.Fprint(os.Stderr, "err-line\nerr-tail") }
`

func TestDevLokiDispatchAndTerminalStreams(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			receiver := &lokiReceiver{values: make(map[string][]string)}
			server := httptest.NewServer(http.HandlerFunc(receiver.serve))
			defer server.Close()
			root := t.TempDir()
			t.Chdir(root)
			manifest := fmt.Sprintf("telemetry: %v\nservices:\n", enabled)
			for _, name := range []string{"alpha", "beta"} {
				if err := os.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(name, "main.go"), []byte(devLogFixture), 0600); err != nil {
					t.Fatal(err)
				}
				manifest += fmt.Sprintf("  - name: %s\n    path: %s\n    kind: service\n    archetype: go\n    dev: go run main.go\n", name, name)
			}
			if err := os.WriteFile("services.yaml", []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			oldHook := ensureTelemetryForDevHook
			ensureTelemetryForDevHook = func() (string, error) { return server.URL, nil }
			defer func() { ensureTelemetryForDevHook = oldHook }()
			stdout, stderr := openTestStream(t, "stdout"), openTestStream(t, "stderr")
			oldOut, oldErr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = stdout, stderr
			defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
			if err := (&devCommand{}).Run([]string{"mono", "dev"}); err != nil {
				t.Fatal(err)
			}
			out, errOut := readTestFile(t, stdout.Name()), readTestFile(t, stderr.Name())
			for _, job := range []string{"alpha", "beta"} {
				for _, part := range []string{"[" + job + "] out-line\n", "[" + job + "] \n"} {
					if !strings.Contains(out, part) {
						t.Errorf("stdout missing %q: %q", part, out)
					}
				}
				if !strings.Contains(errOut, "["+job+"] err-line\n") {
					t.Errorf("stderr missing %s: %q", job, errOut)
				}
				if enabled && (!strings.Contains(out, "["+job+"] out-tail\n") || !strings.Contains(errOut, "["+job+"] err-tail\n")) {
					t.Errorf("fragments missing from terminal: %q %q", out, errOut)
				}
				if strings.Contains(out, "["+job+"] err-line") || strings.Contains(errOut, "["+job+"] out-line") {
					t.Errorf("output delivered to wrong terminal stream")
				}
			}
			expected := map[string][]string{}
			if enabled {
				for _, job := range []string{"alpha", "beta"} {
					expected[job] = []string{"out-line", "", "out-tail", "err-line", "err-tail"}
				}
			}
			receiver.assert(t, expected)
		})
	}
}

func TestDevLokiFlushOnCancellationAndDeliveryFailure(t *testing.T) {
	receiver := &lokiReceiver{values: make(map[string][]string)}
	server := httptest.NewServer(http.HandlerFunc(receiver.serve))
	defer server.Close()
	root := t.TempDir()
	t.Chdir(root)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf stdout-fragment\nprintf stderr-fragment >&2\ntouch started\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runServiceDev(ctx, Service{Name: "cancelled", Path: ".", Dev: "go run fixture"}, newLokiSink(server.URL))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat("started"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	receiver.assert(t, map[string][]string{"cancelled": {"stdout-fragment", "stderr-fragment"}})

	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer failed.Close()
	w := &lokiLineWriter{sink: newLokiSink(failed.URL), job: "job"}
	if n, err := w.Write([]byte("line\n")); err != nil || n != 5 {
		t.Fatalf("delivery failure changed write: %d, %v", n, err)
	}
}
