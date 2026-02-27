# Mono Pretty Terminal Feedback Implementation Plan

## Overview

Standardize `mono` terminal output so local runs are easy for humans to scan with pretty-printing and color, while CI remains plain, stable, and readable in logs.

## Current State Analysis

`mono` currently prints directly from many command packages with mixed formatting conventions and duplicated writer utilities:

- Task orchestration prints mixed progress/cached/failure lines directly via `fmt.Printf` and `fmt.Fprintf` (`internal/cli/tasks/task_executor.go:97`, `internal/cli/tasks/task_executor.go:247`, `internal/cli/tasks/task_executor.go:311`).
- Service command execution uses a separate direct-output style (`internal/cli/core/service_exec.go:54`, `internal/cli/core/service_exec.go:58`, `internal/cli/core/service_exec.go:82`).
- Human-facing command outputs vary by command (`internal/cli/commands/quality/check.go:46`, `internal/cli/commands/insights/status.go:65`, `internal/cli/commands/workflow/doctor.go:24`).
- Prefixing logic is duplicated in two places with near-identical behavior (`internal/cli/tasks/deps.go:40`, `internal/cli/commands/runtime/dev.go:267`).
- There is no centralized output-mode policy (TTY/non-TTY, `CI`, `NO_COLOR`), so style and color are not consistently controllable.

## Desired End State

After implementation:

1. `mono` output formatting is produced through a shared output package instead of ad-hoc `fmt.Print*` calls in core execution paths.
2. Local interactive runs use consistent visual structure for sections, step start/finish, warnings, errors, and summaries.
3. Interactive terminals use a consistent semantic color palette for state (`info`, `success`, `warn`, `error`) to improve scanability.
4. CI/non-interactive runs automatically downgrade to plain deterministic text without relying on color or decorative symbols.
5. Stream prefixing for subprocess output is implemented once and reused by task execution and `dev`.
6. Existing behavior remains functionally equivalent (same command outcomes/exit codes), with improved readability only.

### Key Discoveries:
- Highest-volume task output lives in `runTaskCommand` and executor skip/cache paths (`internal/cli/tasks/task_executor.go:97`, `internal/cli/tasks/task_executor.go:247`, `internal/cli/tasks/task_executor.go:311`).
- `RunCommand` is the central non-orchestrated command path and a critical migration point (`internal/cli/core/service_exec.go:47`).
- Existing tests already validate summary text patterns and can be extended for new output contracts (`internal/cli/tasks/task_output_test.go:23`, `internal/cli/commands/workflow/doctor_test.go:15`).
- `docs/goals.md` explicitly calls for output UX quality in task orchestration (`docs/goals.md:72`).

## What We're NOT Doing

- Adding machine-readable output modes (`--json`) in this effort.
- Reworking task scheduling, caching algorithms, or command semantics.
- Introducing third-party terminal UI frameworks.
- Changing CI workflows beyond output readability and consistency.
- Adding command-specific custom color themes; colors remain centrally defined and semantic.

## Implementation Approach

Introduce a small internal output abstraction (`internal/cli/output`) with a mode-aware renderer, semantic color styling, and shared line-prefix stream utility. Migrate command surfaces in phases, starting with highest-value execution paths (`tasks`, `core`, `quality`, `insights`, `doctor`), then complete migration across remaining runtime/workflow commands. Preserve deterministic plain output in CI and add regression tests focused on readability contracts rather than brittle full snapshots.

## Phase 1: Build Shared Output Foundation

### Overview

Create mode-aware output primitives and a single reusable prefixed writer so command packages can stop manually assembling status strings.

### Changes Required:

#### 1. Add `output` package, mode detection, and style policy
**File**: `internal/cli/output/output.go` (new)  
**File**: `internal/cli/output/mode.go` (new)  
**File**: `internal/cli/output/style.go` (new)
**Changes**:
- Define output modes: interactive, plain.
- Detect plain mode via non-TTY and environment policy (`CI`, `NO_COLOR`).
- Define semantic style tokens (for example `Info`, `Success`, `Warn`, `Error`, `Muted`) and map to ANSI colors only for interactive mode.
- Keep palette restrained and high-contrast for readability.
- Expose helpers for structured lines:
  - section headers
  - step start
  - success
  - warning
  - error
  - summary

```go
type Printer interface {
    Section(title string)
    StepStart(scope, msg string)
    StepOK(scope, msg string)
    StepWarn(scope, msg string)
    StepErr(scope, msg string)
    Summary(msg string)
}
```

#### 2. Create shared prefixed stream writer
**File**: `internal/cli/output/prefix_writer.go` (new)
**Changes**:
- Move line-buffering prefix behavior into one canonical implementation.
- Keep thread-safe writes and `Flush()` semantics used by task/dev subprocess streams.

#### 3. Add focused tests for output policy
**File**: `internal/cli/output/output_test.go` (new)  
**File**: `internal/cli/output/style_test.go` (new)  
**File**: `internal/cli/output/prefix_writer_test.go` (new)
**Changes**:
- Verify mode selection and plain output fallback behavior.
- Verify color rendering is enabled in interactive mode and suppressed in `CI` or when `NO_COLOR` is set.
- Verify multi-line prefixing and flush behavior.

### Success Criteria:

#### Automated Verification:
- [ ] Output package tests pass: `go test ./internal/cli/output`
- [ ] Existing test suite still passes: `make test`
- [ ] Linting remains clean: `make lint`

#### Manual Verification:
- [ ] Local interactive output shows consistent start/success/error semantics.
- [ ] Local interactive output includes consistent semantic color on status lines.
- [ ] Running with `CI=1` emits plain text with no color or decorative-only symbols.
- [ ] Prefixed subprocess lines remain readable and properly scoped.

**Implementation Note**: After phase completion and automated checks pass, pause for manual confirmation before migrating command paths.

---

## Phase 2: Migrate High-Traffic Execution Paths

### Overview

Apply the new output abstraction to command paths users see most often: orchestrated tasks, service command fanout, and check/status command summaries.

### Changes Required:

#### 1. Migrate orchestrated task executor output
**File**: `internal/cli/tasks/task_executor.go`  
**File**: `internal/cli/tasks/task_output.go`
**Changes**:
- Replace direct `fmt.Print*` status lines with output helpers.
- Standardize cached/skip/failure language and ordering.
- Keep deterministic summary counts and existing exit behavior.

#### 2. Migrate core service command execution
**File**: `internal/cli/core/service_exec.go`
**Changes**:
- Replace start/skip/failure/completed direct prints with shared output helpers.
- Keep subprocess stdout/stderr streaming behavior unchanged.

#### 3. Reuse single prefix writer
**File**: `internal/cli/tasks/deps.go`  
**File**: `internal/cli/commands/runtime/dev.go`
**Changes**:
- Remove duplicate `PrefixWriter` type in runtime `dev`.
- Import and use shared `output` prefix writer in both task and dev flows.

#### 4. Migrate check/status surface
**File**: `internal/cli/commands/quality/check.go`  
**File**: `internal/cli/commands/insights/status.go`
**Changes**:
- Use shared section/list formatting helpers for phase and status output.
- Preserve existing information content while improving consistency.

### Success Criteria:

#### Automated Verification:
- [ ] Task output tests pass: `go test ./internal/cli/tasks`
- [ ] Check/status tests pass: `go test ./internal/cli/commands/quality ./internal/cli/commands/insights`
- [ ] Full quality gate passes: `make check`

#### Manual Verification:
- [ ] `mono check` output is easy to scan phase-by-phase.
- [ ] `mono status` sections are visually consistent and readable.
- [ ] `mono lint`/`mono test` clearly identify per-service progress and failures.
- [ ] Color semantics are consistent (`success` always same color, `error` always same color).

**Implementation Note**: After phase completion and automated checks pass, pause for manual confirmation before full command migration.

---

## Phase 3: Complete Command Migration and Documentation

### Overview

Finish migration of remaining command packages and codify output conventions so new commands follow one style by default.

### Changes Required:

#### 1. Migrate remaining runtime/workflow/meta outputs
**File**: `internal/cli/commands/runtime/dev.go`  
**File**: `internal/cli/commands/runtime/infra.go`  
**File**: `internal/cli/commands/runtime/migrate.go`  
**File**: `internal/cli/commands/runtime/hosts.go`  
**File**: `internal/cli/commands/workflow/doctor.go`  
**File**: `internal/cli/commands/workflow/worktree.go`  
**File**: `internal/cli/commands/meta/metadata.go`  
**File**: `internal/cli/core/list.go`  
**File**: `internal/cli/core/usage.go`
**Changes**:
- Convert direct output calls to shared helpers where applicable.
- Keep command-specific content, but normalize line tone and status style.

#### 2. Add regression tests for output contracts
**File**: `internal/cli/run_test.go`  
**File**: `internal/cli/commands/workflow/doctor_test.go`  
**File**: `internal/cli/tasks/task_output_test.go`  
**File**: additional command test files as needed
**Changes**:
- Extend existing tests to assert normalized output markers and summary structures.
- Add CI/plain-mode output assertions for at least one representative command in each command group.

#### 3. Document output conventions
**File**: `internal/cli/README.md`  
**File**: `docs/goals.md`
**Changes**:
- Add short style guide for future command output.
- Mark the output UX checklist item as completed when implementation is verified.

### Success Criteria:

#### Automated Verification:
- [ ] All command package tests pass: `make test`
- [ ] Formatting and lint checks pass: `make fmt-check && make lint`
- [ ] Build and smoke checks pass: `make build && make smoke`
- [ ] Full gate passes: `make check`

#### Manual Verification:
- [ ] Key commands (`status`, `check`, `lint`, `dev`, `doctor`, `infra status`) share a coherent output style.
- [ ] CI/plain mode remains readable in logs and avoids interactive-only decoration.
- [ ] Error paths still surface enough context to diagnose failing service/task quickly.
- [ ] `NO_COLOR=1` forces plain output even in interactive terminals.

**Implementation Note**: After phase completion and all automated checks pass, pause for final human confirmation on readability expectations.

---

## Testing Strategy

### Unit Tests:
- Output mode detection behavior (`interactive` vs `plain`).
- Style token rendering/suppression behavior across mode combinations.
- Prefix writer line buffering, flushing, and concurrency safety.
- Task summary formatting and stable count presentation.

### Integration Tests:
- Representative CLI command output in both default and `CI=1` environments.
- Failure-path output for orchestrated tasks and direct service command execution.
- Regression coverage for check/status consistency after formatting migration.

### Manual Testing Steps:
1. Run `mono status` on a branch with changed services; confirm section clarity and list readability.
2. Run `mono check --base main` (or equivalent) and verify phase transitions and summaries are obvious.
3. Run a command with forced failure and verify failure context is immediately identifiable.
4. Re-run representative commands with `CI=1` to validate plain, deterministic log output.
5. Re-run representative commands with `NO_COLOR=1` to confirm color suppression behavior.

## Performance Considerations

- Output abstraction must avoid heavy allocations in hot loops (task executor paths).
- Prefix writer behavior must remain streaming-friendly for long-running subprocess output.
- Keep logging deterministic without introducing global locks that reduce task concurrency throughput.

## Migration Notes

- No data migration is required.
- Migration should be incremental and low risk: introduce shared output package first, then adopt per command group.
- Backward compatibility target: command semantics and exit codes remain unchanged; only text formatting becomes standardized.

## References

- Task output and orchestration paths: `internal/cli/tasks/task_executor.go:97`, `internal/cli/tasks/task_executor.go:311`, `internal/cli/tasks/task_output.go:42`
- Core service command output path: `internal/cli/core/service_exec.go:47`
- Check/status command surfaces: `internal/cli/commands/quality/check.go:46`, `internal/cli/commands/insights/status.go:65`
- Duplicate prefix writers: `internal/cli/tasks/deps.go:40`, `internal/cli/commands/runtime/dev.go:267`
- Existing output-related tests: `internal/cli/tasks/task_output_test.go:23`, `internal/cli/commands/workflow/doctor_test.go:15`
- Output UX requirement: `docs/goals.md:72`
