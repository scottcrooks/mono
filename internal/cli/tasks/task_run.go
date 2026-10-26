package tasks

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

// PreparedTaskRun is a resolved task graph that can be executed later. Its
// contents remain opaque so request resolution stays owned by tasks.
type PreparedTaskRun struct {
	graph       *taskGraph
	integration bool
}

func PrepareTaskRun(cfg *Config, req TaskRequest) (*PreparedTaskRun, error) {
	resolution, err := resolveTaskRequest(cfg, req)
	if err != nil {
		return nil, err
	}
	graph, err := buildTaskGraph(cfg, resolution.Nodes)
	if err != nil {
		return nil, err
	}
	return &PreparedTaskRun{graph: graph, integration: req.Integration}, nil
}

// TaskProgressSession owns one immutable history snapshot and one display
// across one or more prepared task graphs.
type TaskProgressSession struct {
	model    *taskProgressModel
	line     *output.ProgressLine
	store    taskTimingStore
	ids      map[*PreparedTaskRun]map[TaskNode]string
	warnings map[string]bool
	warnMu   sync.Mutex
}

func NewTaskProgressSession(prepared []*PreparedTaskRun, opts TaskRunOptions) *TaskProgressSession {
	return newTaskProgressSession(prepared, opts, nil)
}

func newTaskProgressSession(prepared []*PreparedTaskRun, opts TaskRunOptions, line *output.ProgressLine) *TaskProgressSession {
	store := newTaskTimingStore("")
	history, loadErr := store.load()
	items := make([]taskProgressItem, 0)
	ids := make(map[*PreparedTaskRun]map[TaskNode]string, len(prepared))
	for phase, run := range prepared {
		if run == nil || run.graph == nil {
			continue
		}
		runIDs := make(map[TaskNode]string, len(run.graph.nodes))
		for node := range run.graph.nodes {
			id := fmt.Sprintf("%d:%s", phase, node.String())
			runIDs[node] = id
			items = append(items, taskProgressItem{ID: id, Key: taskTimingKey{Service: node.Service, Task: string(node.Task), Integration: run.integration}, Phase: phase})
		}
		ids[run] = runIDs
	}
	model := newTaskProgressModel(items, history, opts.Concurrency, time.Now)
	for _, run := range prepared {
		if run == nil || run.graph == nil {
			continue
		}
		for node, resolved := range run.graph.nodes {
			if resolved.SkipReason != "" {
				model.Settle(ids[run][node])
			}
		}
	}
	if line == nil && opts.BufferOutput {
		line = output.NewProgressLine(output.ProgressOptions{Writer: os.Stderr, Mode: output.DetectMode(os.Stdout)})
	}
	session := &TaskProgressSession{model: model, line: line, store: store, ids: ids, warnings: make(map[string]bool)}
	if loadErr != nil {
		session.warnTiming("load", loadErr)
	}
	session.update()
	return session
}

func (s *TaskProgressSession) start(id string) {
	if s != nil && s.model.Start(id) {
		s.update()
	}
}

func (s *TaskProgressSession) settle(id string) {
	if s != nil && s.model.Settle(id) {
		s.update()
	}
}

func (s *TaskProgressSession) executed(key taskTimingKey, duration time.Duration) {
	if s == nil {
		return
	}
	if err := s.store.append(map[taskTimingKey][]time.Duration{key: {duration}}); err != nil {
		category := "save"
		if strings.HasPrefix(err.Error(), "lock timing history:") {
			category = "lock"
		}
		s.warnTiming(category, err)
	}
}

func (s *TaskProgressSession) warnTiming(category string, err error) {
	s.warnMu.Lock()
	if s.warnings[category] {
		s.warnMu.Unlock()
		return
	}
	s.warnings[category] = true
	s.warnMu.Unlock()
	s.Print(func() {
		fmt.Fprintf(os.Stderr, "[warn] timing history %s failed; estimates may be unavailable: %v\n", category, err)
	})
}

func (s *TaskProgressSession) update() {
	if s == nil || s.line == nil {
		return
	}
	if s.TotalNodes() == 0 {
		return
	}
	s.line.UpdateFrom(func() output.ProgressView {
		snapshot := s.model.Snapshot()
		return output.ProgressView{Kind: output.ProgressTasks, Percent: snapshot.Percent, Elapsed: snapshot.Elapsed, Remaining: snapshot.Remaining, Learning: snapshot.Learning}
	})
}

// TotalNodes reports the complete set registered before execution begins.
func (s *TaskProgressSession) TotalNodes() int {
	if s == nil || s.model == nil {
		return 0
	}
	return len(s.model.nodes)
}

// Elapsed reports time since the shared task session began.
func (s *TaskProgressSession) Elapsed() time.Duration {
	if s == nil || s.model == nil {
		return 0
	}
	return s.model.Snapshot().Elapsed
}

func (s *TaskProgressSession) Print(callback func()) {
	if s != nil && s.line != nil {
		s.line.Print(callback)
		return
	}
	if callback != nil {
		callback()
	}
}

func (s *TaskProgressSession) Close() {
	if s != nil && s.line != nil {
		s.line.Close()
	}
}

func RunPreparedTaskRun(prepared *PreparedTaskRun, opts TaskRunOptions) ([]TaskRunResult, error) {
	if prepared == nil || prepared.graph == nil {
		return nil, fmt.Errorf("prepared task run is nil")
	}
	session := opts.Progress
	if session == nil {
		session = NewTaskProgressSession([]*PreparedTaskRun{prepared}, opts)
		defer session.Close()
	}
	opts.Progress = session
	opts.progressIDs = session.ids[prepared]
	opts.Integration = prepared.integration
	executor := newTaskExecutor()
	return executor.execute(context.Background(), prepared.graph, opts)
}
