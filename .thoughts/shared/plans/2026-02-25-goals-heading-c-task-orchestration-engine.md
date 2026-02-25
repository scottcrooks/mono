# Task Orchestration Engine (Goals C) Implementation Plan

## Overview

Implement `docs/goals.md` section **C. Task Orchestration Engine** on top of the current `services.yaml` model and newly-added impact analysis primitives, while replacing arbitrary per-project command maps with archetype templates as the single source of truth.

## Current State Analysis

Section B capability exists (`affected`, `status`, changed/impacted computation), but there is no execution engine whose unit is `(project, task)` with dependency-ordered scheduling, configurable parallelism, cache semantics, or execution summary.

- Goals section C requirements are defined in `docs/goals.md:67-102`.
- CLI currently falls back to direct service command fanout (`runServiceCommand`) for unknown top-level verbs in `internal/cli/run.go:26-40` and `internal/cli/registry.go:52-85`.
- Current fallback execution is sequential, service-level, and fail-fast; it is not task-graph aware (`internal/cli/registry.go:77-83`).
- `status` currently previews only check tasks from impacted services and does not execute anything (`internal/cli/status.go:25-36`, `internal/cli/impact.go:235-259`).
- `services.example.yml` currently encodes arbitrary command strings per entry (`services.example.yml:5-50`), which duplicates behavior and prevents runtime-level standardization.

### Implementation Checkpoint (2026-02-25)

Most of Goals C is now implemented in `internal/cli`:

- Recognized task verbs are routed into an orchestration path (`internal/cli/run.go:35-40`).
- Task identity, supported vocabulary, resolution, archetype templates, DAG building, parallel scheduling, cache, and summary output exist (`internal/cli/tasks.go`, `internal/cli/task_resolution.go`, `internal/cli/task_templates.go`, `internal/cli/task_graph.go`, `internal/cli/task_executor.go`, `internal/cli/task_cache.go`, `internal/cli/task_output.go`).
- `--no-cache` and `--concurrency` parsing are implemented (`internal/cli/task_flags.go:14-47`).
- Goal C-focused tests exist and pass in targeted runs (`internal/cli/task_resolution_test.go`, `internal/cli/task_templates_test.go`, `internal/cli/task_executor_test.go`, `internal/cli/task_cache_test.go`, `internal/cli/task_output_test.go`).

Current known gaps against the intent of section C:

- Legacy arbitrary command maps are still present in schema/runtime fallback (`Service.Commands` and `runServiceCommand`) and remain available for non-task verbs (`internal/cli/registry.go:44-89`, `internal/cli/registry.go:119-159`).
- Template semantics still need hardening to match intended task meaning and role constraints (for example, permissiveness of `package` on package kind) (`internal/cli/task_templates.go:23-44`).
- Full `make test` is currently blocked by an unrelated metadata test failure (`internal/cli/metadata_test.go:67` observed during verification), so Goal C completion should be validated via targeted task-engine tests plus full-suite rerun after that issue is addressed.

## Desired End State

After this plan is implemented:

1. `mono` has a reusable orchestration core with node identity `(service, task)`.
2. Supported MVP task names are `build`, `lint`, `typecheck`, `test`, `package`, `deploy`.
3. Task command resolution is archetype-driven (single source of truth), not arbitrary per-project command text.
4. `dev` is defined only for service projects; package projects do not require `dev`.
5. Task execution respects dependency ordering (`dep:task` before dependent `:task`) and runs independent nodes in parallel with bounded concurrency.
6. Local cache can skip work using deterministic coarse keys; `--no-cache` forces execution.
7. CLI output is segmented by `(service, task)` and ends with deterministic summary counts and non-zero exit on failures.

### Key Discoveries
- Current `Service` schema has only `name/path/depends/commands` and lacks explicit archetype typing (`internal/cli/registry.go:45-50`).
- Dependency closure logic and deterministic sorting patterns already exist and can be reused for graph construction semantics (`internal/cli/impact.go:154-268`).
- `runServiceCommand` is the primary integration seam for replacing sequential fanout with graph-aware task execution for recognized task verbs (`internal/cli/registry.go:52-85`).
- Manifest examples imply two runtimes (`go`, `react`) plus a separate role (`kind: service|package`), but currently encode behavior through repeated command blocks (`services.example.yml:2-50`).
- `make` targets already provide standardized verification entrypoints (`Makefile:39-63`).

## What We're NOT Doing

- Manifest rename/schema migration to `mono.yaml` (section A).
- Full CI plan emitter (`mono plan --format json`) from section E.
- Helm deployment contract and policy enforcement from sections F/G.
- Remote cache or remote execution (explicit non-goals in `docs/goals.md:224-232`).
- Reintroducing arbitrary per-project command script maps after archetype templates are defined.

## Implementation Approach

Build a dedicated orchestration package in `internal/cli` that is decoupled into:

1. **Resolution layer**: normalize target services + requested task into explicit `(service, task)` nodes.
2. **Template layer**: map archetype + role (`service` vs `package`) to canonical task commands.
3. **Graph layer**: build DAG edges from service `depends` relationships for the same task name.
4. **Execution layer**: topological scheduling with worker concurrency and deterministic output ordering.
5. **Cache layer**: local filesystem cache keyed by task/project digest + selected env + lockfiles.
6. **CLI integration layer**: route recognized task verbs through orchestrator while preserving legacy behavior for non-task verbs.

This sequence keeps C1/C2 stable first, then adds C3 execution correctness, then C4/C5 production quality behavior.

Given the current checkpoint, phases 1-3 below are effectively implemented. The remaining work is hardening/alignment.

## Phase 1: Task Model and Resolution Contract (C1 + C2)

### Overview

Introduce explicit task identity and archetype-based task resolution for current `services.yaml` entries.

### Changes Required

#### 1. Add orchestration domain types and archetype schema
**File**: `internal/cli/tasks.go` (new)
**File**: `internal/cli/registry.go`
**File**: `services.example.yml`
**Changes**:
- Add task constants for MVP vocabulary: `build`, `lint`, `typecheck`, `test`, `package`, `deploy`.
- Add `TaskNode` (`Service`, `Task`) and `TaskRequest` (`Task`, optional service filters).
- Add validation helpers for allowed tasks and task/service selection errors.
- Extend service model with explicit `archetype` (runtime) and `kind` (service/package), replacing free-form command maps as the primary task source.
- Update example manifest entries to use `archetype: go|react` and only keep `dev` where `kind=service`.

```go
type TaskName string

type TaskNode struct {
    Service string
    Task    TaskName
}
```

#### 2. Implement default task resolution from archetype templates
**File**: `internal/cli/task_resolution.go` (new)
**File**: `internal/cli/task_templates.go` (new)
**Changes**:
- Resolve service set (`all` vs explicit args).
- Resolve command implementation by `archetype -> task template` mapping.
- Enforce `dev` availability by project kind: required/available for services, omitted for packages.
- For unsupported tasks in an archetype, mark node as skipped-with-reason instead of hard failure.
- Keep deterministic ordering by service then task.

#### 3. Add resolution and template-focused tests
**File**: `internal/cli/task_resolution_test.go` (new)
**File**: `internal/cli/task_templates_test.go` (new)
**Changes**:
- Table tests for recognized task names.
- Tests for explicit service selection, unknown service failures, and sorted deterministic node output.
- Tests for archetype template expansion and `dev` gating (service vs package).

### Success Criteria

#### Automated Verification
- [ ] Resolution tests pass: `go test ./internal/cli -run TestTaskResolution`
- [ ] Full tests pass: `make test`
- [ ] Lint passes: `make lint`
- [ ] Build succeeds: `make build`

#### Manual Verification
- [ ] `mono lint` resolves all configured services into `(service,lint)` nodes.
- [ ] `mono lint pythia` resolves only requested service.
- [ ] Service entries expose `dev`; package entries do not require or expose `dev`.
- [ ] Unsupported task names still follow existing fallback behavior (no regression).

**Implementation Note**: After this phase and automated checks pass, pause for manual confirmation before moving to DAG execution.

---

## Phase 2: DAG Planner and Parallel Scheduler (C3)

### Overview

Execute resolved task nodes in dependency order with bounded concurrency.

### Changes Required

#### 1. Add task DAG builder
**File**: `internal/cli/task_graph.go` (new)
**Changes**:
- Build adjacency and in-degree maps for `TaskNode`.
- For same task type, add edge `dep:task -> svc:task` when `svc.Depends` includes `dep`.
- Detect cycles and return clear path-oriented errors.

#### 2. Add scheduler/executor
**File**: `internal/cli/task_executor.go` (new)
**Changes**:
- Implement topological scheduling with worker pool and configurable `--concurrency` (default sensible value like `runtime.NumCPU()` capped).
- Execute command strings through existing `commandFromParts` and working-directory behavior currently in `runCommand`.
- Track node outcomes: succeeded, failed, skipped.
- Preserve deterministic launch ordering among ready nodes.

#### 3. Integrate recognized task verbs with orchestrator
**File**: `internal/cli/run.go`
**Changes**:
- Before fallback to `runServiceCommand`, detect recognized task verbs.
- Route recognized task executions to orchestrator entrypoint.
- Keep non-task verbs using existing fallback path.

#### 4. Add graph/execution tests
**File**: `internal/cli/task_executor_test.go` (new)
**Changes**:
- Unit tests for topological ordering and independent-node parallelism.
- Tests for cycle detection messaging.
- Command-invocation tests using temp repos/configs similar to impact test helpers.

### Success Criteria

#### Automated Verification
- [ ] DAG and executor tests pass: `go test ./internal/cli -run TestTaskExecutor`
- [ ] Run-path regression tests pass: `go test ./internal/cli -run TestRun`
- [ ] Full checks pass: `make check`

#### Manual Verification
- [ ] For a dependency chain (`lib -> api`), `mono build api` executes `lib:build` before `api:build`.
- [ ] Independent services run concurrently when `--concurrency` > 1.
- [ ] Failed task returns non-zero process exit and halts dependent tasks.

**Implementation Note**: After phase completion and checks pass, pause for manual confirmation before adding cache behavior.

---

## Phase 3: Local Cache and Output UX (C4 + C5)

### Overview

Add coarse local cache with diagnostics and finalize operator-friendly execution output.

### Changes Required

#### 1. Add cache key and storage primitives
**File**: `internal/cli/task_cache.go` (new)
**Changes**:
- Store cache metadata under repo-local `.cache/mono/tasks/`.
- Key includes:
  - task name,
  - hash of project files under `svc.Path`,
  - lockfile hashes (e.g. `go.sum`, `package-lock.json`, `pnpm-lock.yaml` when present),
  - selected env var whitelist.
- Cache API: `Load`, `Store`, `ReasonForMiss`.

#### 2. Add CLI flags for cache and concurrency
**File**: `internal/cli/task_flags.go` (new), `internal/cli/registry.go`
**Changes**:
- Parse `--no-cache` and `--concurrency` for recognized task verbs.
- Update usage text and examples accordingly.

#### 3. Implement segmented logs and terminal summary
**File**: `internal/cli/task_output.go` (new), `internal/cli/task_executor.go`
**Changes**:
- Prefix logs by `[service:task]`.
- End-of-run summary with counts for `succeeded`, `failed`, `skipped (cached)`.
- Include concise cache miss reason text when task executes despite cache presence.

#### 4. Add cache/output tests
**File**: `internal/cli/task_cache_test.go` (new), `internal/cli/task_output_test.go` (new)
**Changes**:
- Tests for cache-hit skip, `--no-cache` behavior, and stable key generation.
- Tests for summary output formatting and failure exit semantics.

### Success Criteria

#### Automated Verification
- [ ] Cache tests pass: `go test ./internal/cli -run TestTaskCache`
- [ ] Output tests pass: `go test ./internal/cli -run TestTaskOutput`
- [ ] Full checks pass: `make check`
- [ ] CLI binary builds and basic smoke passes: `make build && make smoke`

#### Manual Verification
- [ ] Re-running same task on unchanged tree reports cache hits and skips execution.
- [ ] `--no-cache` forces execution even when cache entries exist.
- [ ] Output clearly separates logs per `(service, task)` and final summary is actionable.
- [ ] Failure in one task produces non-zero exit and correct summary counts.

**Implementation Note**: After this phase and automated checks pass, pause for final manual confirmation before starting section D work.

---

## Phase 4: Hardening and Goal-Intent Alignment

### Overview

Close remaining deltas between the implemented engine and Goal C intent, without regressing current CLI behavior.

### Changes Required

#### 1. Tighten template task policy by role
**File**: `internal/cli/task_templates.go`
**File**: `internal/cli/task_templates_test.go`
**Changes**:
- Enforce explicit service-only task policy where required by goals (notably `deploy`, and `package` if retained as service-only for this repo policy).
- Replace placeholder template commands with explicit, intentional defaults per archetype and task semantics.
- Extend tests to assert role-specific availability matrix.

#### 2. Delineate orchestrated tasks from legacy custom commands
**File**: `internal/cli/registry.go`
**File**: `internal/cli/run.go`
**File**: `internal/cli/registry_test.go` (new)
**Changes**:
- Keep fallback behavior for non-task verbs, but make boundaries explicit and tested.
- Ensure no task-verb path can accidentally read legacy per-service command maps.
- Document behavior in CLI usage text.

#### 3. Verification stabilization
**File**: `internal/cli/metadata_test.go` (existing)
**Changes**:
- Resolve/contain unrelated metadata test instability so `make test` can be used again as final gate for Goal C.
- Add targeted regression assertions to prevent recurrence.

### Success Criteria

#### Automated Verification
- [ ] Task template policy tests pass: `go test ./internal/cli -run TestTaskTemplates`
- [ ] Task executor/resolution/cache/output tests pass: `go test ./internal/cli -run TestTask`
- [ ] Full suite passes: `make test`
- [ ] Full quality gate passes: `make check`

#### Manual Verification
- [ ] `mono deploy <package-project>` is rejected or skipped with explicit reason per policy.
- [ ] `mono package <package-project>` behavior matches agreed policy and docs.
- [ ] Non-task custom command path still works for explicitly configured legacy commands.
- [ ] CLI usage and observed behavior are consistent for task vs non-task verbs.

**Implementation Note**: After this phase and automated checks pass, pause for human confirmation on policy choices before moving to section D.

---

## Testing Strategy

### Unit Tests
- Task vocabulary validation and resolution behavior.
- DAG construction and cycle detection.
- Scheduler correctness (ordering, parallel readiness, failure propagation).
- Cache key determinism and miss-reason generation.

### Integration Tests
- Temp git/config repo setup with service dependency chains.
- End-to-end CLI invocation for `mono build`, `mono lint`, `mono test` with service filters.
- Cache behavior across repeated runs and modified lockfile/source scenarios.

### Manual Testing Steps
1. Create/update `services.yaml` with `lib -> api` dependency and `build/lint/test` commands.
2. Run `mono build api` and verify ordering + summary.
3. Run the same command twice and verify second run is cached.
4. Run with `--no-cache` and verify forced execution.
5. Introduce failing command in one service and verify non-zero exit + failure summary.

## Performance Considerations

- Scheduler operations should remain linear in nodes + edges.
- Limit repeated filesystem hashing via cache metadata snapshots when safe.
- Keep log streaming direct (avoid buffering entire command output in memory).
- Use bounded worker pools to avoid over-saturating developer machines.

## Migration Notes

- No manifest migration required for this phase; implementation consumes current `services.yaml` schema.
- Existing command behavior for non-task verbs remains unchanged.
- This orchestration core becomes the foundation for D (`mono check`) and E (`mono plan`) in later phases.
- Archetype templates remain the single source of truth for standard tasks; per-project arbitrary commands stay out of the task engine path.

## References

- Goals C requirements: `docs/goals.md:67-102`
- Existing command fallback path: `internal/cli/run.go:26-40`
- Existing sequential fanout behavior: `internal/cli/registry.go:52-85`
- Existing service schema: `internal/cli/registry.go:45-50`
- Existing impacted/status utility functions: `internal/cli/impact.go:235-259`, `internal/cli/status.go:25-36`
- Existing manifest command duplication to replace: `services.example.yml:5-50`
- Prior implementation status research: `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
- Related change-detection plan already completed/in-progress: `.thoughts/shared/plans/2026-02-25-impacted-change-detection-implementation.md`
