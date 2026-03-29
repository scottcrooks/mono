package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/scottcrooks/mono/internal/cli/output"
)

var runTaskRequestWithConfig = RunOrchestratedTaskRequestWithConfig

type taskExecutor struct {
	cache   taskCache
	printer output.Printer
}

type readyTask struct {
	resolved ResolvedTaskNode
	cacheKey string
}

func newTaskExecutor() taskExecutor {
	return taskExecutor{
		cache:   newTaskCache(),
		printer: output.DefaultPrinter(),
	}
}

func DefaultTaskConcurrency() int {
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

func RunOrchestratedTask(command string, args []string) error {
	task, ok := ParseTaskName(command)
	if !ok {
		return fmt.Errorf("unsupported task %q", command)
	}

	serviceArgs, opts, err := parseTaskInvocationArgs(args[2:], DefaultTaskConcurrency())
	if err != nil {
		return err
	}
	if opts.Integration && task != TaskTest {
		return fmt.Errorf("--integration is only supported with %q", TaskTest)
	}

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	targetServices, exactServices, err := resolveTaskTargetServices(cfg, task, serviceArgs, opts)
	if err != nil {
		return err
	}
	if len(targetServices) == 0 {
		printNoTaskTargets(task, opts.All)
		return nil
	}

	results, err := runTaskRequestWithConfig(cfg, TaskRequest{
		Task:          task,
		Services:      targetServices,
		ExactServices: exactServices,
		Integration:   opts.Integration,
	}, opts)
	PrintTaskSummary(results)
	if err != nil {
		return err
	}
	return nil
}

func RunOrchestratedTaskRequestWithConfig(cfg *Config, req TaskRequest, opts TaskRunOptions) ([]TaskRunResult, error) {
	resolution, err := resolveTaskRequest(cfg, req)
	if err != nil {
		return nil, err
	}

	graph, err := buildTaskGraph(cfg, resolution.Nodes)
	if err != nil {
		return nil, err
	}

	executor := newTaskExecutor()
	return executor.execute(context.Background(), graph, opts)
}

func (e taskExecutor) execute(ctx context.Context, graph *taskGraph, opts TaskRunOptions) ([]TaskRunResult, error) {
	results := make([]TaskRunResult, 0, len(graph.nodes))
	resultByNode := make(map[TaskNode]TaskRunResult, len(graph.nodes))
	effectiveCacheKeys := make(map[TaskNode]string, len(graph.nodes))

	remaining := make(map[TaskNode]ResolvedTaskNode, len(graph.nodes))
	inDegree := make(map[TaskNode]int, len(graph.inDegree))
	for node, resolved := range graph.nodes {
		inDegree[node] = graph.inDegree[node]
		if resolved.SkipReason != "" {
			e.printer.StepWarn(node.String(), "skipped: "+resolved.SkipReason)
			result := TaskRunResult{Node: node, Status: TaskStatusSkipped, SkipReason: resolved.SkipReason}
			results = append(results, result)
			resultByNode[node] = result
			continue
		}
		remaining[node] = resolved
	}

	for node := range resultByNode {
		for _, next := range graph.edges[node] {
			inDegree[next]--
		}
	}

	for len(remaining) > 0 {
		ready := make([]ResolvedTaskNode, 0)
		for node, resolved := range remaining {
			if inDegree[node] == 0 {
				ready = append(ready, resolved)
			}
		}
		if len(ready) == 0 {
			return results, fmt.Errorf("executor deadlock: no ready nodes")
		}
		sort.Slice(ready, func(i, j int) bool {
			return ready[i].Node.Service < ready[j].Node.Service
		})

		readyTasks := make([]readyTask, 0, len(ready))
		preBatch := make([]TaskRunResult, 0)
		preFailed := false
		for _, resolved := range ready {
			node := resolved.Node
			baseKey := ""
			if taskUsesCache(node.Task) {
				var err error
				baseKey, err = buildTaskCacheKey(resolved.Service, node.Task, resolved.Command)
				if err != nil {
					preBatch = append(preBatch, TaskRunResult{Node: node, Status: TaskStatusFailed, Err: err})
					preFailed = true
					delete(remaining, node)
					continue
				}
			}
			depKeys := make([]string, 0, len(graph.reverse[node]))
			for _, dep := range graph.reverse[node] {
				key := effectiveCacheKeys[dep]
				if key == "" {
					continue
				}
				depKeys = append(depKeys, key)
			}
			readyTasks = append(readyTasks, readyTask{
				resolved: resolved,
				cacheKey: composeExecutionCacheKey(baseKey, depKeys),
			})
		}

		batch := e.runReadyBatch(ctx, readyTasks, opts)
		failed := preFailed
		for _, result := range preBatch {
			results = append(results, result)
			resultByNode[result.Node] = result
		}
		for _, result := range batch {
			results = append(results, result)
			resultByNode[result.Node] = result
			delete(remaining, result.Node)
			for _, task := range readyTasks {
				if task.resolved.Node == result.Node {
					effectiveCacheKeys[result.Node] = task.cacheKey
					break
				}
			}
			if result.Status == TaskStatusFailed {
				failed = true
				if !continueOnFailure(result.Node.Task) {
					continue
				}
			}
			for _, next := range graph.edges[result.Node] {
				inDegree[next]--
			}
		}
		if failed && !continueOnFailure(ready[0].Node.Task) {
			for node := range remaining {
				e.printer.StepWarn(node.String(), "skipped: blocked by earlier task failure")
				result := TaskRunResult{Node: node, Status: TaskStatusSkipped, SkipReason: "blocked by earlier task failure"}
				results = append(results, result)
				delete(remaining, node)
			}
			break
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Node.String() < results[j].Node.String()
	})

	summary := summarizeTaskResults(results)
	if summary.Failed > 0 {
		return results, fmt.Errorf("%d task(s) failed", summary.Failed)
	}
	return results, nil
}

func (e taskExecutor) runReadyBatch(ctx context.Context, ready []readyTask, opts TaskRunOptions) []TaskRunResult {
	if len(ready) == 0 {
		return nil
	}

	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	sem := make(chan struct{}, opts.Concurrency)
	results := make([]TaskRunResult, len(ready))
	var wg sync.WaitGroup

	for i := range ready {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = e.runNode(ctx, ready[idx], opts)
		}(i)
	}

	wg.Wait()
	return results
}

func (e taskExecutor) runNode(ctx context.Context, task readyTask, opts TaskRunOptions) TaskRunResult {
	node := task.resolved.Node
	if !taskUsesCache(node.Task) {
		cmdString := commandForExecution(task.resolved.Service, node, task.resolved.Command, opts)
		if err := runTaskCommand(ctx, e.printer, task.resolved.Service, node, cmdString); err != nil {
			return TaskRunResult{Node: node, Status: TaskStatusFailed, Err: err}
		}
		return TaskRunResult{Node: node, Status: TaskStatusSucceeded}
	}

	cacheKey := task.cacheKey
	entry, hit, err := e.cache.load(cacheKey)
	if err != nil {
		return TaskRunResult{Node: node, Status: TaskStatusFailed, Err: err}
	}
	if !opts.NoCache && hit && entry.Key != "" {
		e.printer.Summary(fmt.Sprintf("[cached] [%s] skipped", node.String()))
		return TaskRunResult{Node: node, Status: TaskStatusSkipped, SkipReason: "cached", Cached: true}
	}
	if reason := cacheMissReason(opts.NoCache, hit); reason != "" {
		e.printer.StepWarn(node.String(), "cache miss: "+reason)
	}

	cmdString := commandForExecution(task.resolved.Service, node, task.resolved.Command, opts)
	if err := runTaskCommand(ctx, e.printer, task.resolved.Service, node, cmdString); err != nil {
		return TaskRunResult{Node: node, Status: TaskStatusFailed, Err: err}
	}

	storeErr := e.cache.store(taskCacheEntry{
		Key:       cacheKey,
		Service:   node.Service,
		Task:      node.Task,
		CreatedAt: time.Now().UTC(),
	})
	if storeErr != nil {
		fmt.Fprintf(os.Stderr, "[%s] warning: failed to write cache entry: %v\n", node, storeErr)
	}

	return TaskRunResult{Node: node, Status: TaskStatusSucceeded}
}

func taskUsesCache(task TaskName) bool {
	return task != TaskAudit
}

func continueOnFailure(task TaskName) bool {
	return task == TaskAudit
}

func commandForExecution(svc Service, node TaskNode, command string, opts TaskRunOptions) string {
	if !opts.NoCache {
		return command
	}
	if node.Task != TaskTest || svc.Archetype != "go" {
		return command
	}
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "go test") || strings.Contains(trimmed, "-count=") {
		return command
	}
	return trimmed + " -count=1"
}

func composeExecutionCacheKey(baseKey string, dependencyKeys []string) string {
	if len(dependencyKeys) == 0 {
		return baseKey
	}
	keys := append([]string(nil), dependencyKeys...)
	sort.Strings(keys)
	h := sha256.New()
	_, _ = h.Write([]byte(baseKey))
	_, _ = h.Write([]byte{'\n'})
	for _, key := range keys {
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func runTaskCommand(ctx context.Context, printer output.Printer, svc Service, node TaskNode, cmdString string) error {
	printer.StepStart(node.String(), "start")
	absPath, err := filepath.Abs(svc.Path)
	if err != nil {
		return err
	}

	parts := strings.Fields(cmdString)
	cmd, err := commandFromParts(ctx, parts)
	if err != nil {
		return fmt.Errorf("[%s] %w", node, err)
	}
	cmd.Dir = absPath
	cmd.Stdout = output.NewPrefixWriter(fmt.Sprintf("[%s]", node), os.Stdout)
	cmd.Stderr = output.NewPrefixWriter(fmt.Sprintf("[%s]", node), os.Stderr)
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		printer.StepErr(node.String(), "failed")
		return err
	}
	printer.StepOK(node.String(), "completed")
	return nil
}
