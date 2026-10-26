package tasks

import (
	"math"
	"sync"
	"time"
)

type taskProgressItem struct {
	ID    string
	Key   taskTimingKey
	Phase int
}

type taskProgressState uint8

const (
	taskProgressPlanned taskProgressState = iota
	taskProgressRunning
	taskProgressSettled
)

type taskProgressNode struct {
	item      taskProgressItem
	weight    time.Duration
	known     bool
	state     taskProgressState
	startedAt time.Time
}

type taskProgressSnapshot struct {
	Percent   int
	Elapsed   time.Duration
	Remaining time.Duration
	Learning  bool
	Settled   int
	Total     int
}

type taskProgressModel struct {
	mu          sync.Mutex
	nodes       map[string]*taskProgressNode
	concurrency int
	startedAt   time.Time
	clock       func() time.Time
	maxPercent  int
}

func newTaskProgressModel(items []taskProgressItem, history map[taskTimingKey][]time.Duration, concurrency int, clock func() time.Time) *taskProgressModel {
	if concurrency < 1 {
		concurrency = 1
	}
	if clock == nil {
		clock = time.Now
	}
	model := &taskProgressModel{
		nodes:       make(map[string]*taskProgressNode, len(items)),
		concurrency: concurrency,
		startedAt:   clock(),
		clock:       clock,
	}
	var knownTotal float64
	knownCount := 0
	for _, item := range items {
		if estimate, ok := meanTaskDuration(history[item.Key]); ok {
			if estimate < time.Millisecond {
				estimate = time.Millisecond
			}
			model.nodes[item.ID] = &taskProgressNode{item: item, weight: estimate, known: true}
			knownTotal += float64(estimate)
			knownCount++
		} else {
			model.nodes[item.ID] = &taskProgressNode{item: item}
		}
	}
	unknownWeight := time.Second
	if knownCount > 0 {
		unknownWeight = boundedProgressDuration(knownTotal / float64(knownCount))
		if unknownWeight < time.Millisecond {
			unknownWeight = time.Millisecond
		}
	}
	for _, node := range model.nodes {
		if !node.known {
			node.weight = unknownWeight
		}
	}
	return model
}

func (m *taskProgressModel) Start(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	node := m.nodes[id]
	if node == nil || node.state != taskProgressPlanned {
		return false
	}
	node.state = taskProgressRunning
	node.startedAt = m.clock()
	return true
}

func (m *taskProgressModel) Settle(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	node := m.nodes[id]
	if node == nil || node.state == taskProgressSettled {
		return false
	}
	node.state = taskProgressSettled
	return true
}

func (m *taskProgressModel) Snapshot() taskProgressSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	if now.Before(m.startedAt) {
		now = m.startedAt
	}
	totalWeight := 0.0
	credited := 0.0
	settled := 0
	phaseRemaining := make(map[int]float64)
	phaseUnfinished := make(map[int]int)
	phaseLearning := make(map[int]bool)
	for _, node := range m.nodes {
		weight := float64(node.weight)
		totalWeight += weight
		switch node.state {
		case taskProgressSettled:
			credited += weight
			settled++
		case taskProgressPlanned:
			if node.known {
				phaseRemaining[node.item.Phase] += weight
			} else {
				phaseLearning[node.item.Phase] = true
			}
			phaseUnfinished[node.item.Phase]++
		case taskProgressRunning:
			elapsed := now.Sub(node.startedAt)
			if elapsed < 0 {
				elapsed = 0
			}
			if node.known {
				credit := math.Min(float64(elapsed), weight)
				credited += credit
				phaseRemaining[node.item.Phase] += math.Max(weight-float64(elapsed), 0)
			} else {
				phaseLearning[node.item.Phase] = true
			}
			phaseUnfinished[node.item.Phase]++
		}
	}
	percent := 0
	if totalWeight > 0 {
		percent = int(math.Floor(credited * 100 / totalWeight))
	}
	if percent > 100 {
		percent = 100
	}
	if settled < len(m.nodes) && percent > 99 {
		percent = 99
	}
	if percent < m.maxPercent {
		percent = m.maxPercent
	} else {
		m.maxPercent = percent
	}
	remaining := 0.0
	learning := false
	for phase, count := range phaseUnfinished {
		if phaseLearning[phase] {
			learning = true
			continue
		}
		parallel := m.concurrency
		if count < parallel {
			parallel = count
		}
		if parallel < 1 {
			parallel = 1
		}
		remaining += phaseRemaining[phase] / float64(parallel)
	}
	return taskProgressSnapshot{
		Percent:   percent,
		Elapsed:   now.Sub(m.startedAt),
		Remaining: boundedProgressDuration(remaining),
		Learning:  learning,
		Settled:   settled,
		Total:     len(m.nodes),
	}
}

func boundedProgressDuration(value float64) time.Duration {
	if value >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(value)
}
