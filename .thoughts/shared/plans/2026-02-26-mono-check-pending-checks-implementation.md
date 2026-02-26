# Mono Check Pending Checks Implementation Plan

## Overview

Implement a first-class `mono check` command that executes all pending PR-gate checks (`lint`, `typecheck`, `test`) for impacted services, reusing the existing impact detection and task orchestration engine.

## Current State Analysis

The codebase already computes the exact "planned check tasks" preview but does not execute them:

- `mono status` prints changed services, impacted services, and "Planned check tasks" from `buildCheckTaskPreview` (`internal/cli/status.go:29`, `internal/cli/status.go:76`, `internal/cli/impact.go:235`).
- Pending-check vocabulary is already defined as `lint/typecheck/test` (`internal/cli/impact.go:236`).
- Orchestrated task execution exists for single task verbs (for example `mono lint`, `mono test`) with caching/concurrency (`internal/cli/run.go:35`, `internal/cli/task_executor.go:41`).
- `mono check` is not a registered command and currently falls through to legacy command-map fanout (`internal/cli/run.go:43`, `internal/cli/registry.go:57`).
- Goals explicitly call for `mono check` to run impacted checks (`docs/goals.md:109`), and `status` already describes what would run for that command (`docs/goals.md:63`).

## Desired End State

After implementation:

1. `mono check` computes impacted services using existing impact logic.
2. It runs pending checks in deterministic phase order: `lint`, then `typecheck`, then `test`.
3. Each phase runs through the existing orchestrator (cache, concurrency, dependency-aware scheduling).
4. Services that do not support a given check are skipped consistently with current task-template behavior.
5. If there are no impacted services, command exits `0` with clear no-op output.
6. CLI usage/help and tests cover the new command contract.

### Key Discoveries:
- `status` and `impact` already provide the pending-check source of truth (`internal/cli/status.go:29`, `internal/cli/impact.go:235`).
- Current task routing is command-name based (`parseTaskName`), so `check` must be an explicit top-level command (`internal/cli/tasks.go:52`, `internal/cli/run.go:26`).
- Existing orchestrator already supports required runtime controls (`--no-cache`, `--concurrency`) (`internal/cli/task_flags.go:14`).
- Regression guardrails for task-vs-legacy dispatch already exist and should be extended for `check` (`internal/cli/registry_test.go:9`).

## What We're NOT Doing

- Adding `--all` semantics or changing targeted execution behavior for other commands.
- Implementing watch-mode/recheck loops (`mono dev` scope).
- Adding new task kinds beyond `lint`, `typecheck`, `test`.
- Changing manifest format or replacing `services.yaml`.

## Implementation Approach

Create a dedicated `check` command that:

1. Resolves impacted services via `buildImpactReport`.
2. Translates impacted services + pending tasks into executable task phases.
3. Reuses existing orchestration internals per phase (instead of duplicating executor logic).
4. Aggregates phase results into one final exit code and summary.

This keeps behavior aligned with current `status` preview while minimizing new engine surface area.

## Phase 1: Define Check Command Contract and Shared Planner

### Overview

Add a reusable planner that converts impacted services into concrete pending-check task batches.

### Changes Required:

#### 1. Add check command scaffold and argument parser
**File**: `internal/cli/check.go` (new)
**Changes**:
- Register `check` as an explicit command.
- Parse flags:
  - `--base <ref>`
  - `--no-cache`
  - `--concurrency <n>`
- Reject unknown flags with clear usage text.

```go
type checkCommand struct{}

func (c *checkCommand) Run(args []string) error
```

#### 2. Add pending-check plan builder
**File**: `internal/cli/impact.go`
**File**: `internal/cli/check.go` (new helper if preferred)
**Changes**:
- Reuse impacted set from `buildImpactReport`.
- Reuse `buildCheckTaskPreview` to preserve "pending" semantics.
- Produce deterministic execution plan structure:
  - impacted service list
  - per-task service targets for `lint`, `typecheck`, `test`

#### 3. Update usage/help text
**File**: `internal/cli/registry.go`
**Changes**:
- Add `check [--base <ref>] [--no-cache] [--concurrency N]`.
- Add examples for normal and base-ref invocation.

### Success Criteria:

#### Automated Verification:
- [ ] New parser/planner tests pass: `go test ./internal/cli -run TestCheckCommand`
- [ ] Existing impact/status tests still pass: `go test ./internal/cli -run 'Test(Status|Impact)'`
- [ ] Full test suite passes: `make test`

#### Manual Verification:
- [ ] `mono check --help` (via top-level help text) describes flags correctly.
- [ ] Planner output (via tests/logging) matches existing `mono status` pending rows.
- [ ] Invalid args (for example `--concurrency 0`) fail with actionable errors.

**Implementation Note**: After phase completion and automated checks pass, pause for manual confirmation before wiring execution.

---

## Phase 2: Execute Pending Checks Through Existing Orchestrator

### Overview

Run planned phases (`lint`, `typecheck`, `test`) for impacted services by invoking the existing task engine path with shared options.

### Changes Required:

#### 1. Extract reusable orchestrated-task runner
**File**: `internal/cli/task_executor.go`
**Changes**:
- Factor `runOrchestratedTask` into:
  - command-string entrypoint (existing behavior), and
  - internal typed entrypoint that accepts `TaskRequest` + `TaskRunOptions`.
- Keep legacy behavior unchanged for direct task verbs.

#### 2. Implement `check` phase execution
**File**: `internal/cli/check.go` (new)
**Changes**:
- Build impact report for requested base.
- If no impacted services: print no-op message and exit success.
- For each phase in order:
  - resolve task for only services pending that phase,
  - execute via shared task runner,
  - stop on first failed phase and return non-zero.
- Print concise phase headers and final summary.

#### 3. Ensure pending-service targeting is exact
**File**: `internal/cli/task_resolution.go` (if needed)
**Changes**:
- Add an explicit "exact services" mode for check execution so `mono check` runs only the services shown as pending by `mono status`.
- Preserve current behavior for direct `mono <task> <service...>` paths unless intentionally changed.

### Success Criteria:

#### Automated Verification:
- [ ] Check execution tests pass: `go test ./internal/cli -run TestCheckExecution`
- [ ] Run-path regressions pass: `go test ./internal/cli -run 'TestRun|TestRunTaskVerb'`
- [ ] Full quality gate passes: `make check`

#### Manual Verification:
- [ ] `mono check` runs only pending checks for impacted services.
- [ ] Execution order is `lint -> typecheck -> test`.
- [ ] `mono check --base main` changes impact baseline as expected.
- [ ] Task cache and concurrency flags behave the same as direct task commands.

**Implementation Note**: After this phase and automated checks pass, pause for manual confirmation before final UX/test hardening.

---

## Phase 3: Testing and UX Hardening

### Overview

Lock behavior with deterministic tests and ensure status/check parity remains stable over time.

### Changes Required:

#### 1. Add command-level integration tests
**File**: `internal/cli/check_test.go` (new)
**Changes**:
- Temp-repo scenarios (reuse impact test helpers) for:
  - no impacted services,
  - impacted service with all three checks supported,
  - impacted package where `typecheck` is skipped,
  - failure propagation (non-zero exit).

#### 2. Add parity regression test against status planner
**File**: `internal/cli/status_test.go` or `internal/cli/check_test.go`
**Changes**:
- Assert `check` planning source and `status` planned rows stay consistent for same repo state.

#### 3. Document command in goals tracking
**File**: `docs/goals.md`
**Changes**:
- Mark/check off D1 checklist items once implemented and verified.

### Success Criteria:

#### Automated Verification:
- [ ] New check tests pass: `go test ./internal/cli -run TestCheck`
- [ ] Existing status/impact tests still pass: `go test ./internal/cli -run 'TestStatus|TestImpact'`
- [ ] Full suite passes: `make test`
- [ ] Full gate passes: `make check`

#### Manual Verification:
- [ ] `mono status` and `mono check` agree on what is pending.
- [ ] Logs are clear enough to identify which service/task failed.
- [ ] No regressions for legacy non-task command dispatch.

**Implementation Note**: After this phase and all automated checks pass, pause for final human confirmation before moving to next goals section.

---

## Testing Strategy

### Unit Tests:
- Arg parsing for `check` flags and invalid input paths.
- Pending-plan generation from impacted + task support matrix.
- Task-runner wiring from `check` into orchestrated execution internals.

### Integration Tests:
- Git-based impact scenarios with branch/base changes.
- Mixed service kinds (`service` vs `package`) to validate skip behavior.
- Failure and early-stop behavior across multi-phase checks.

### Manual Testing Steps:
1. Create feature branch change under one service path and run `mono status --base main`.
2. Run `mono check --base main` and confirm executed service/task set matches status preview.
3. Re-run `mono check` to confirm expected cache behavior.
4. Introduce a lint/test failure and confirm non-zero exit plus clear failure output.

## Performance Considerations

- Reuse existing executor worker-pool and cache behavior to avoid new scheduling complexity.
- Compute impact once per `check` invocation and reuse results across phases.
- Keep per-phase target sets deterministic and minimal to avoid unnecessary execution.

## Migration Notes

- No data migration required.
- Backward compatibility:
  - direct task verbs continue to work unchanged,
  - legacy custom command-map fallback remains for non-task, non-check verbs.

## References

- Goals checklist: `docs/goals.md:60`, `docs/goals.md:63`, `docs/goals.md:109`
- Status command and pending preview: `internal/cli/status.go:29`, `internal/cli/status.go:76`
- Pending task derivation: `internal/cli/impact.go:235`
- Task dispatch and executor: `internal/cli/run.go:35`, `internal/cli/task_executor.go:41`
- Legacy fallback behavior: `internal/cli/run.go:43`, `internal/cli/registry.go:57`
- Dispatch regression tests: `internal/cli/registry_test.go:9`
- Prior research: `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
- Related plans:
  - `.thoughts/shared/plans/2026-02-25-impacted-change-detection-implementation.md`
  - `.thoughts/shared/plans/2026-02-25-goals-heading-c-task-orchestration-engine.md`
