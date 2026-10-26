package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func timingKey(service, task string, integration bool) taskTimingKey {
	return taskTimingKey{Service: service, Task: task, Integration: integration}
}

func TestTaskTimingStoreRoundTripWindowAndVariantIsolation(t *testing.T) {
	store := newTaskTimingStore(t.TempDir())
	regular := timingKey("api", "test", false)
	integration := timingKey("api", "test", true)
	for i := 1; i <= 11; i++ {
		if err := store.append(map[taskTimingKey][]time.Duration{regular: {time.Duration(i) * time.Second}}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if err := store.append(map[taskTimingKey][]time.Duration{integration: {2 * time.Second}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded[regular]; len(got) != 10 || got[0] != 2*time.Second || got[9] != 11*time.Second {
		t.Fatalf("unexpected rolling window: %v", got)
	}
	if got := loaded[integration]; !reflect.DeepEqual(got, []time.Duration{2 * time.Second}) {
		t.Fatalf("integration variant mixed: %v", got)
	}
	mean, ok := meanTaskDuration(loaded[regular])
	if !ok || mean != 6*time.Second+500*time.Millisecond {
		t.Fatalf("mean = %v, %v", mean, ok)
	}
}

func TestTaskTimingStoreColdStartAndValidation(t *testing.T) {
	store := newTaskTimingStore(t.TempDir())
	if got, err := store.load(); err != nil || len(got) != 0 {
		t.Fatalf("cold load = %v, %v", got, err)
	}
	for _, raw := range []string{
		`{broken`,
		`{"version":1,"timings":[{"service":"api","task":"test","integration":false,"durationsMs":[9223372036854775807]}]}`,
		`{"version":1,"timings":[{"service":"api","task":"test","integration":false,"durationsMs":[-1]}]}`,
		`{"version":1,"timings":[{"service":"api","task":"test","integration":false,"durationsMs":[]},{"service":"api","task":"test","integration":false,"durationsMs":[1]}]}`,
	} {
		if err := os.WriteFile(store.path(), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.load(); err == nil {
			t.Fatalf("expected invalid history error for %s", raw)
		}
	}
	unknown := []byte(`{"version":99,"timings":[],"futureField":true}`)
	if err := os.WriteFile(store.path(), unknown, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Fatal("unknown version was accepted")
	}
	if err := store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {time.Second}}); err == nil {
		t.Fatal("unknown version should not be overwritten")
	}
	if got, err := os.ReadFile(store.path()); err != nil || !reflect.DeepEqual(got, unknown) {
		t.Fatalf("unknown history changed: %q, %v", got, err)
	}
	if err := os.WriteFile(store.path(), []byte(`broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {0}}); err != nil {
		t.Fatalf("recover corrupt file: %v", err)
	}
	loaded, err := store.load()
	if err != nil || len(loaded[timingKey("api", "test", false)]) != 1 {
		t.Fatalf("recovered history = %v, %v", loaded, err)
	}
}

type failingTaskTimingOps struct {
	base                                        taskTimingFileOps
	readErr, renameErr, createTempErr, writeErr error
}

func (o failingTaskTimingOps) ReadFile(path string) ([]byte, error) {
	if o.readErr != nil && filepath.Base(path) == taskTimingFile {
		return nil, o.readErr
	}
	return o.base.ReadFile(path)
}
func (o failingTaskTimingOps) CreateTemp(dir, pattern string) (taskTimingTempFile, error) {
	if o.createTempErr != nil {
		return nil, o.createTempErr
	}
	f, err := o.base.CreateTemp(dir, pattern)
	if err != nil || o.writeErr == nil {
		return f, err
	}
	return failingTimingTempFile{taskTimingTempFile: f, writeErr: o.writeErr}, nil
}
func (o failingTaskTimingOps) Rename(oldPath, newPath string) error {
	if o.renameErr != nil {
		return o.renameErr
	}
	return o.base.Rename(oldPath, newPath)
}
func (o failingTaskTimingOps) Remove(path string) error { return o.base.Remove(path) }

type failingTimingTempFile struct {
	taskTimingTempFile
	writeErr error
}

func (f failingTimingTempFile) Write([]byte) (int, error) { return 0, f.writeErr }

func TestTaskTimingStoreAdvisoryReadAndRenameFailures(t *testing.T) {
	root := t.TempDir()
	store := newTaskTimingStore(root)
	store.ops = failingTaskTimingOps{base: osTaskTimingFileOps{}, readErr: errors.New("injected read failure")}
	if _, err := store.load(); err == nil {
		t.Fatal("expected read failure")
	}
	store.ops = osTaskTimingFileOps{}
	if err := store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {time.Second}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path())
	if err != nil {
		t.Fatal(err)
	}
	store.ops = failingTaskTimingOps{base: osTaskTimingFileOps{}, renameErr: errors.New("injected rename failure")}
	if err := store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {2 * time.Second}}); err == nil {
		t.Fatal("expected rename failure")
	}
	after, err := os.ReadFile(store.path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed replacement changed history: before=%s after=%s", before, after)
	}
	for _, ops := range []failingTaskTimingOps{
		{base: osTaskTimingFileOps{}, createTempErr: errors.New("injected create failure")},
		{base: osTaskTimingFileOps{}, writeErr: errors.New("injected write failure")},
	} {
		store.ops = ops
		if err := store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {3 * time.Second}}); err == nil {
			t.Fatal("expected injected save failure")
		}
		current, err := os.ReadFile(store.path())
		if err != nil || !reflect.DeepEqual(before, current) {
			t.Fatalf("save failure changed history: %v", err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= len(taskTimingFile+".tmp-") && entry.Name()[:len(taskTimingFile+".tmp-")] == taskTimingFile+".tmp-" {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}

func TestTaskTimingStoreLockTimeout(t *testing.T) {
	root := t.TempDir()
	release, err := acquireTaskTimingLock(root, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	store := newTaskTimingStore(root)
	started := time.Now()
	err = store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {time.Second}})
	if err == nil {
		t.Fatal("expected held lock timeout")
	}
	if time.Since(started) < 900*time.Millisecond {
		t.Fatalf("lock returned too early: %s", time.Since(started))
	}
}

func TestTaskTimingStoreProcessLockTimeout(t *testing.T) {
	taskTimingProcessLock.Lock()
	defer taskTimingProcessLock.Unlock()
	store := newTaskTimingStore(t.TempDir())
	completed := make(chan error, 1)
	go func() {
		completed <- store.append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {time.Second}})
	}()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("expected held process lock timeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process-local lock exceeded the update timeout")
	}
}

func TestTaskTimingConcurrentProcessHelper(t *testing.T) {
	if os.Getenv("MONO_TIMING_HELPER") != "1" {
		return
	}
	root := os.Getenv("MONO_TIMING_ROOT")
	gate := os.Getenv("MONO_TIMING_GATE")
	for {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	value, _ := strconv.Atoi(os.Getenv("MONO_TIMING_VALUE"))
	if err := newTaskTimingStore(root).append(map[taskTimingKey][]time.Duration{timingKey("api", "test", false): {time.Duration(value) * time.Second}}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskTimingStoreMergesConcurrentProcesses(t *testing.T) {
	root := t.TempDir()
	gate := filepath.Join(root, "go")
	commands := make([]*exec.Cmd, 0, 2)
	outputs := make([]bytes.Buffer, 2)
	for i, value := range []string{"1", "2"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTaskTimingConcurrentProcessHelper$")
		cmd.Env = append(os.Environ(), "MONO_TIMING_HELPER=1", "MONO_TIMING_ROOT="+root, "MONO_TIMING_GATE="+gate, "MONO_TIMING_VALUE="+value)
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper: %v: %s", err, outputs[i].String())
		}
	}
	loaded, err := newTaskTimingStore(root).load()
	if err != nil {
		t.Fatal(err)
	}
	got := loaded[timingKey("api", "test", false)]
	if len(got) != 2 || !(got[0] == time.Second && got[1] == 2*time.Second || got[0] == 2*time.Second && got[1] == time.Second) {
		t.Fatalf("lost concurrent observation: got %v", got)
	}
	data, err := os.ReadFile(filepath.Join(root, taskTimingFile))
	if err != nil {
		t.Fatal(err)
	}
	var parsed taskTimingFileData
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Version != taskTimingVersion {
		t.Fatalf("invalid persisted JSON: %v, %s", err, data)
	}
}
