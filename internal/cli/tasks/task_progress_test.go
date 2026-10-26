package tasks

import (
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

func TestTaskProgressLargeValidEstimatesDoNotOverflow(t *testing.T) {
	key := timingKey("api", "test", false)
	model := newTaskProgressModel([]taskProgressItem{{ID: "a", Key: key}, {ID: "b", Key: key}, {ID: "unknown"}}, map[taskTimingKey][]time.Duration{key: {time.Duration(math.MaxInt64)}}, 1, nil)
	if weight := model.nodes["unknown"].weight; weight != time.Duration(math.MaxInt64) {
		t.Fatalf("fallback weight overflowed: %s", weight)
	}
	model.Settle("unknown")
	if got := model.Snapshot(); got.Learning || got.Remaining != time.Duration(math.MaxInt64) {
		t.Fatalf("aggregate ETA overflowed: %+v", got)
	}
}

type progressEventWriter chan string

func (w progressEventWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func TestTaskSessionTicksRefreshRunningEstimates(t *testing.T) {
	var seconds atomic.Int64
	key := timingKey("api", "test", false)
	model := newTaskProgressModel([]taskProgressItem{{ID: "running", Key: key}}, map[taskTimingKey][]time.Duration{key: {4 * time.Second}}, 1, func() time.Time { return time.Unix(seconds.Load(), 0) })
	events := make(progressEventWriter, 1000)
	line := output.NewProgressLine(output.ProgressOptions{Writer: events, Mode: output.ModeInteractive, IsTTY: func() bool { return true }, Term: func() string { return "xterm" }, Width: func() (int, error) { return 100, nil }, TickInterval: 5 * time.Millisecond})
	session := &TaskProgressSession{model: model, line: line}
	defer session.Close()
	session.start("running")
	seconds.Store(2)
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for {
		select {
		case event := <-events:
			if strings.Contains(event, "elapsed 0:02 remaining 0:02 50%") {
				return
			}
		case <-timeout.C:
			t.Fatal("timer redraw did not refresh elapsed time, running percentage and ETA")
		}
	}
}

func TestTaskSessionSettlesFuturePhasePlannedSkipsImmediately(t *testing.T) {
	withWorkingDir(t, t.TempDir())
	cfg := &Config{Services: []Service{{Name: "api", Path: "api", Kind: "service", Archetype: "go"}}}
	first, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskFix, Services: []string{"api"}, ExactServices: true})
	if err != nil {
		t.Fatal(err)
	}
	later, err := PrepareTaskRun(cfg, TaskRequest{Task: TaskFormat, Services: []string{"api"}, ExactServices: true, FileTargets: map[string][]string{}})
	if err != nil {
		t.Fatal(err)
	}
	session := NewTaskProgressSession([]*PreparedTaskRun{first, later}, TaskRunOptions{Concurrency: 1})
	defer session.Close()
	if got := session.model.Snapshot(); got.Total != 2 || got.Settled != 1 || got.Percent != 50 {
		t.Fatalf("future planned skip remained unfinished: %+v", got)
	}
}

type progressFakeClock struct{ now time.Time }

func (c *progressFakeClock) Now() time.Time          { return c.now }
func (c *progressFakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestTaskProgressWeightedRunningAndMonotonic(t *testing.T) {
	clock := &progressFakeClock{now: time.Unix(0, 0)}
	a, b := timingKey("api", "lint", false), timingKey("web", "test", false)
	model := newTaskProgressModel([]taskProgressItem{{ID: "a", Key: a}, {ID: "b", Key: b}}, map[taskTimingKey][]time.Duration{a: {2 * time.Second}, b: {4 * time.Second}}, 2, clock.Now)
	model.Start("a")
	clock.Advance(time.Second)
	first := model.Snapshot()
	if first.Percent != 16 || first.Remaining != 2500*time.Millisecond {
		t.Fatalf("first snapshot: %+v", first)
	}
	clock.Advance(4 * time.Second)
	overrun := model.Snapshot()
	if overrun.Percent != 33 || overrun.Percent < first.Percent {
		t.Fatalf("overrun snapshot: %+v", overrun)
	}
	model.Settle("a")
	settled := model.Snapshot()
	if settled.Percent < overrun.Percent || settled.Percent >= 100 {
		t.Fatalf("settle was not monotonic/bounded: %+v", settled)
	}
	model.Start("b")
	clock.Advance(4 * time.Second)
	model.Settle("b")
	complete := model.Snapshot()
	if complete.Percent != 100 || complete.Settled != 2 || complete.Remaining != 0 {
		t.Fatalf("completion: %+v", complete)
	}
	if model.Settle("b") {
		t.Fatal("duplicate settlement accepted")
	}
}

func TestTaskProgressColdStartUsesTaskCountsAndLearningETA(t *testing.T) {
	clock := &progressFakeClock{now: time.Unix(0, 0)}
	model := newTaskProgressModel([]taskProgressItem{{ID: "a", Key: timingKey("a", "test", false)}, {ID: "b", Key: timingKey("b", "test", false)}}, nil, 4, clock.Now)
	model.Start("a")
	clock.Advance(5 * time.Second)
	if got := model.Snapshot(); got.Percent != 0 || !got.Learning {
		t.Fatalf("cold running snapshot: %+v", got)
	}
	model.Settle("a")
	if got := model.Snapshot(); got.Percent != 50 || !got.Learning {
		t.Fatalf("one cold task settled: %+v", got)
	}
	model.Settle("b")
	if got := model.Snapshot(); got.Percent != 100 || got.Learning || got.Remaining != 0 {
		t.Fatalf("cold completion: %+v", got)
	}
}

func TestTaskProgressMixedHistoryUsesWeightedKnownAndCountBasedUnknown(t *testing.T) {
	clock := &progressFakeClock{now: time.Unix(0, 0)}
	known, unknown := timingKey("api", "lint", false), timingKey("web", "lint", false)
	model := newTaskProgressModel([]taskProgressItem{{ID: "known", Key: known}, {ID: "unknown", Key: unknown}}, map[taskTimingKey][]time.Duration{known: {2 * time.Second}}, 2, clock.Now)
	model.Start("known")
	clock.Advance(time.Second)
	if got := model.Snapshot(); got.Percent != 25 || !got.Learning {
		t.Fatalf("mixed running snapshot: %+v", got)
	}
	model.Settle("unknown")
	if got := model.Snapshot(); got.Percent != 75 || got.Learning || got.Remaining != time.Second {
		t.Fatalf("mixed settled snapshot: %+v", got)
	}
}

func TestTaskProgressPhaseETAAndVariantIsolation(t *testing.T) {
	clock := &progressFakeClock{now: time.Unix(0, 0)}
	regular, integration := timingKey("api", "test", false), timingKey("api", "test", true)
	history := map[taskTimingKey][]time.Duration{regular: {4 * time.Second}, integration: {8 * time.Second}, timingKey("unselected", "test", false): {time.Hour}}
	items := []taskProgressItem{{ID: "unit", Key: regular, Phase: 0}, {ID: "integration", Key: integration, Phase: 1}}
	model := newTaskProgressModel(items, history, 4, clock.Now)
	if got := model.Snapshot(); got.Remaining != 12*time.Second {
		t.Fatalf("phase ETA included unrelated history or ignored phase boundary: %+v", got)
	}
	model.Start("unit")
	clock.Advance(2 * time.Second)
	if got := model.Snapshot(); got.Remaining != 10*time.Second {
		t.Fatalf("running phase ETA: %+v", got)
	}
}

func TestTaskProgressConcurrencyChangesETA(t *testing.T) {
	clock := &progressFakeClock{now: time.Unix(0, 0)}
	a, b := timingKey("api", "lint", false), timingKey("web", "lint", false)
	items := []taskProgressItem{{ID: "a", Key: a}, {ID: "b", Key: b}}
	history := map[taskTimingKey][]time.Duration{a: {4 * time.Second}, b: {4 * time.Second}}
	serial := newTaskProgressModel(items, history, 1, clock.Now).Snapshot()
	parallel := newTaskProgressModel(items, history, 2, clock.Now).Snapshot()
	if serial.Remaining != 8*time.Second || parallel.Remaining != 4*time.Second {
		t.Fatalf("concurrency ETA serial=%s parallel=%s", serial.Remaining, parallel.Remaining)
	}
}

func TestTaskProgressZeroDurationAndEmptyPlan(t *testing.T) {
	clock := &progressFakeClock{now: time.Unix(0, 0)}
	key := timingKey("api", "fix", false)
	model := newTaskProgressModel([]taskProgressItem{{ID: "zero", Key: key}}, map[taskTimingKey][]time.Duration{key: {0}}, 0, clock.Now)
	if model.nodes["zero"].weight != time.Millisecond {
		t.Fatalf("zero estimate floor = %s", model.nodes["zero"].weight)
	}
	model.Settle("zero")
	if got := model.Snapshot(); got.Percent != 100 {
		t.Fatalf("zero duration completion: %+v", got)
	}
	empty := newTaskProgressModel(nil, nil, 0, clock.Now).Snapshot()
	if empty.Total != 0 || empty.Percent != 0 || empty.Remaining != 0 {
		t.Fatalf("empty snapshot: %+v", empty)
	}
}
