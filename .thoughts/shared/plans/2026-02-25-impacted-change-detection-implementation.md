# Impacted Change Detection (Goals B) Implementation Plan

## Overview

Implement `docs/goals.md` section **B. Impacted Change Detection** on top of the current `services.yaml` model, without introducing the broader task engine from section C yet.

## Current State Analysis

`mono` currently dispatches explicit subcommands through a command registry and falls back to "service command fanout" for unknown verbs. There is no notion of changed files, impacted closure, or explainable dependency chains.

- Command dispatch and fallback are in `internal/cli/run.go:11-40`.
- Config is loaded from `services.yaml` with service `name/path/depends/commands` in `internal/cli/registry.go:37-104`.
- Help text has no `affected` or `status` command in `internal/cli/registry.go:233-268`.
- Existing git helpers for default base branch detection exist in `internal/cli/worktree.go:460-484`.
- Existing tests include git-backed temp repo patterns in `internal/cli/worktree_test.go`.

## Desired End State

After implementation:

1. `mono affected` prints impacted services (changed + dependents) from git diff against merge-base.
2. `mono affected --explain` prints chain(s) showing why each impacted service is included.
3. `mono status` prints:
   - changed services,
   - impacted services,
   - planned `check` tasks summary (`lint`, `typecheck`, `test`) for impacted services.
4. Base ref is configurable (flag), with deterministic output ordering.

### Key Discoveries
- Reusing existing `Service.Path` and `Service.Depends` avoids waiting on manifest/schema work (`internal/cli/registry.go:44-49`).
- Current CLI command pattern is "register command + parse subcommand/flags in `Run(args)`" (`internal/cli/hosts.go`, `internal/cli/migrate.go`, `internal/cli/infra.go`).
- Existing merge-base default branch logic is already present and can be reused/adapted (`internal/cli/worktree.go:460-484`).

## What We're NOT Doing

- Implementing section C task DAG scheduling/caching.
- Introducing `mono.yaml` or changing manifest schema.
- Adding deploy/policy checks from sections F/G beyond what B requires.
- Implementing JSON plan output (`mono plan --format json`) from section E.

## Implementation Approach

Add a dedicated impact-analysis module that:

1. resolves git base (explicit `--base` or default merge base branch),
2. collects changed files via `git diff --name-only`,
3. maps files to owning services by path prefix,
4. computes impacted closure using reverse dependency traversal,
5. optionally builds explain chains,
6. formats deterministic CLI output for `affected` and `status`.

Keep logic isolated from command rendering so it can be reused later by section C task planning.

## Phase 1: Core Impact Analysis Engine

### Overview

Create reusable primitives for changed-service detection and impacted closure.

### Changes Required

#### 1. Add impact analysis domain code
**File**: `internal/cli/impact.go` (new)
**Changes**:
- Define internal types for:
  - changed set,
  - impacted set,
  - explain paths (`map[service][]chain` or equivalent),
  - check task preview rows.
- Implement:
  - `resolveBaseRef(baseFlag string) (string, error)`
  - `changedFiles(baseRef string) ([]string, error)` using `git diff --name-only <mergeBase>...HEAD`
  - `mapFilesToChangedServices(cfg *Config, files []string) []Service`
  - `buildReverseDeps(cfg *Config) map[string][]string`
  - `computeImpactedClosure(changed []string, reverse map[string][]string) []string`
  - `computeExplainChains(changed []string, reverse map[string][]string) map[string][]string`
- Enforce stable ordering with `sort.Strings`.

```go
// Pseudocode shape
type ImpactReport struct {
    BaseRef   string
    Changed   []string
    Impacted  []string
    Explain   map[string][]string
}
```

#### 2. Add git command helpers for impact analysis
**File**: `internal/cli/impact_git.go` (new)
**Changes**:
- Encapsulate git command execution behind small helper funcs for testability.
- Reuse logic from `defaultMergeBaseBranch` (`internal/cli/worktree.go:460-484`) via extraction or direct shared helper.

#### 3. Add unit tests for impact engine
**File**: `internal/cli/impact_test.go` (new)
**Changes**:
- Table tests for:
  - path-prefix ownership resolution,
  - reverse dependency graph and closure,
  - deterministic ordering,
  - explain chain rendering.
- Git integration tests (temp repo) for merge-base + diff behavior.

### Success Criteria

#### Automated Verification
- [ ] New impact unit tests pass: `go test ./internal/cli -run TestImpact`
- [ ] Full test suite passes: `make test`
- [ ] Lint passes: `make lint`
- [ ] Build succeeds: `make build`

#### Manual Verification
- [ ] In a sample branch with service-local edits, changed services match edited paths.
- [ ] Dependent services are included in impacted set even when unchanged directly.
- [ ] Output order is stable across repeated runs.

**Implementation Note**: After completing this phase and automated checks pass, pause for human confirmation of manual results before starting Phase 2.

---

## Phase 2: `mono affected` Command + Explainability

### Overview

Expose impact computation through a first-class CLI command with optional explain mode.

### Changes Required

#### 1. Register and implement the command
**File**: `internal/cli/affected.go` (new)
**Changes**:
- Register `affected` in `init()`.
- Support flags:
  - `--base <ref>`
  - `--explain`
- Run impact engine and print:
  - default: impacted services list (one per line),
  - explain mode: chain lines like `shared-lib -> billing-api`.

```go
func (c *affectedCommand) Run(args []string) error {
    // parse flags, compute impact, print deterministic output
}
```

#### 2. Surface in top-level usage
**File**: `internal/cli/registry.go`
**Changes**:
- Add `affected` to command list and examples in `printUsage()`.

#### 3. Add command-level tests
**File**: `internal/cli/affected_command_test.go` (new)
**Changes**:
- Verify:
  - no explain output by default,
  - explain format with chains,
  - explicit base ref handling,
  - clean behavior when no changes are detected.

### Success Criteria

#### Automated Verification
- [ ] Affected command tests pass: `go test ./internal/cli -run TestAffectedCommand`
- [ ] Full tests pass: `make test`
- [ ] Build passes: `make build`

#### Manual Verification
- [ ] `mono affected` prints impacted services for a feature branch.
- [ ] `mono affected --explain` shows understandable dependency chains.
- [ ] `mono affected --base main` changes baseline as expected.

**Implementation Note**: After phase completion and automated checks pass, pause for manual confirmation before proceeding.

---

## Phase 3: `mono status` Summary for B3

### Overview

Add status reporting that summarizes changed/impacted services and planned `mono check` tasks.

### Changes Required

#### 1. Implement `status` command
**File**: `internal/cli/status.go` (new)
**Changes**:
- Register `status` command.
- Support `--base <ref>`.
- Render three sections:
  - `Changed services`
  - `Impacted services`
  - `Planned check tasks`
- For planned tasks, show per impacted service which of `lint/typecheck/test` exist in `svc.Commands`, marking missing ones as skipped.

#### 2. Update usage output
**File**: `internal/cli/registry.go`
**Changes**:
- Add `status` command description and examples.

#### 3. Add tests
**File**: `internal/cli/status_test.go` (new)
**Changes**:
- Validate section presence and ordering.
- Validate planned task summary from mocked service command maps.
- Validate empty change set behavior.

### Success Criteria

#### Automated Verification
- [ ] Status tests pass: `go test ./internal/cli -run TestStatus`
- [ ] Formatting/lint/tests/build pass: `make check`

#### Manual Verification
- [ ] `mono status` clearly distinguishes changed vs impacted services.
- [ ] Planned task summary matches `services.yaml` command definitions.
- [ ] Command output is readable and actionable for local PR checks.

**Implementation Note**: After this phase, pause for final manual confirmation before moving to section C work.

---

## Testing Strategy

### Unit Tests
- Path-prefix matching edge cases (`apps/a` vs `apps/ab` boundaries).
- Reverse dependency traversal and cycle-safe closure behavior.
- Deterministic output sorting.
- Explain chain generation for single and multi-hop dependencies.

### Integration Tests
- Temp git repo with baseline commit and feature-branch edits.
- Base ref resolution fallback (`origin/HEAD` -> `main/master`) behavior.
- CLI command invocation tests for `affected` and `status`.

### Manual Testing Steps
1. Create branch, modify one service file, run `mono affected`.
2. Run `mono affected --explain` and verify chain(s).
3. Run `mono status` and validate changed/impacted/planned sections.
4. Re-run commands multiple times to confirm deterministic ordering.

## Performance Considerations

- Keep operations linear in number of services and dependency edges.
- Use single-pass prefix matching and in-memory adjacency maps.
- Avoid expensive repeated git invocations by computing impact report once per command execution.

## Migration Notes

- No schema/data migration required.
- Uses existing `services.yaml` contract.
- Future section C can consume `ImpactReport` directly for task planning.

## References

- Goals checklist B: `docs/goals.md:45-63`
- Existing CLI dispatch: `internal/cli/run.go:11-40`
- Existing config/deps model: `internal/cli/registry.go:37-104`
- Existing default base branch logic: `internal/cli/worktree.go:460-484`
- Prior research snapshot: `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
