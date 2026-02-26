# Internal CLI Purpose-Based Command Structure Refactor Implementation Plan

## Overview

Refactor `internal/cli` so command logic and related files are grouped by purpose in subdirectories, reducing the flat file surface while preserving `mono` behavior, output, and test coverage.

## Current State Analysis

`internal/cli` currently mixes all command files, orchestration files, impact analysis files, and tests in one flat directory.

- Dispatch is centralized in `Run(args)` and resolves in this order: registered command, orchestrated task, service command fallback (`internal/cli/run.go:11`, `internal/cli/run.go:27`, `internal/cli/run.go:35`, `internal/cli/run.go:43`).
- Command registration is side-effect based through `init()` in each command file and shared mutable registry map (`internal/cli/registry.go:20`, `internal/cli/registry.go:22`, `internal/cli/registry.go:29`).
- Command/config primitives (`Command`, `Config`, `Service`, `loadConfig`, `runServiceCommand`) are tightly coupled in one file (`internal/cli/registry.go:15`, `internal/cli/registry.go:37`, `internal/cli/registry.go:57`, `internal/cli/registry.go:92`).
- Orchestrated task engine primitives and execution flow are also in the same directory (`internal/cli/tasks.go`, `internal/cli/task_executor.go`, `internal/cli/task_graph.go`, `internal/cli/task_resolution.go`).
- Tests already verify key dispatch boundaries that must remain stable through refactor (`internal/cli/registry_test.go`, `internal/cli/run_test.go`, `internal/cli/check_test.go`).

## Desired End State

A structured `internal/cli` layout where:

1. Entry and high-level dispatch remain in `internal/cli`.
2. Command implementations move to `internal/cli/commands/<purpose>/`.
3. Orchestration/task engine files move to `internal/cli/tasks/`.
4. Impact/change-detection logic moves to `internal/cli/impact/`.
5. Shared command runtime primitives move to `internal/cli/core/`.
6. Existing command names, flags, and output behavior remain backward-compatible.

### Key Discoveries:
- Existing dispatch and fallback behavior is explicit and test-covered, so the refactor must keep ordering intact (`internal/cli/run.go:26-48`, `internal/cli/registry_test.go`).
- Side-effect registration via `init()` currently depends on package-local unexported state, which cannot survive directory/package splits without an explicit registration API (`internal/cli/registry.go:20-31`).
- `check` depends on impact and orchestrated-task internals, so it should migrate after stable package boundaries are introduced (`internal/cli/check.go:38-53`).

## What We're NOT Doing

- Changing command UX, flags, or command names.
- Replacing the CLI architecture with Cobra/urfave or another framework.
- Reworking service schema semantics in `services.yaml`.
- Changing task orchestration behavior or dependency rules.

## Implementation Approach

Use a boundary-first refactor:

1. Introduce explicit exported APIs in `core`, `tasks`, and `impact` packages.
2. Move command implementations incrementally by purpose, replacing `init()` side effects with explicit registration wiring in one place.
3. Move tests alongside their target command packages while preserving top-level integration tests in `internal/cli`.
4. Keep `internal/cli/run.go` as the stable entrypoint throughout.

## Phase 1: Establish Stable Package Boundaries

### Overview
Create package interfaces that allow command files to move out of the flat directory without behavior changes.

### Changes Required:

#### 1. Create `core` package for shared CLI primitives
**Files**:
- `internal/cli/core/registry.go`
- `internal/cli/core/config.go`
- `internal/cli/core/service_exec.go`
- `internal/cli/core/usage.go`

**Changes**:
- Move `Command`, command registry state, config structs, config loader, service-command fallback execution, and usage printer into `core`.
- Export only required APIs (`Register`, `Lookup`, `RunServiceCommand`, `LoadConfig`, etc.).

#### 2. Introduce explicit command bootstrap in `internal/cli`
**Files**:
- `internal/cli/bootstrap.go` (new)
- `internal/cli/run.go` (update)

**Changes**:
- Replace implicit package-wide `init()` registration dependency with explicit `registerCommands()` called from package init/bootstrap path.
- Preserve dispatch order exactly as currently implemented.

#### 3. Carve out `tasks` and `impact` packages
**Files**:
- `internal/cli/tasks/*.go` (new package)
- `internal/cli/impact/*.go` (new package)

**Changes**:
- Move task and impact logic behind exported functions/types used by `check` and task command handling.
- Keep existing semantics and default behaviors unchanged.

### Success Criteria:

#### Automated Verification:
- [ ] Formatting passes: `make fmt-check`
- [ ] Static checks pass: `make lint`
- [ ] Tests pass after boundary extraction: `make test`
- [ ] CLI still builds: `make build`
- [ ] go fix is run to modernize all code

#### Manual Verification:
- [ ] `./bin/mono --help` output remains intact.
- [ ] `./bin/mono check --base HEAD` still routes through check flow.
- [ ] `./bin/mono test --integration <service>` behavior is unchanged.

**Implementation Note**: After completing this phase and automated checks pass, pause for manual confirmation before Phase 2.

---

## Phase 2: Migrate Commands into Purpose-Based Directories

### Overview
Move command implementations and their tests into purpose-focused folders, with each folder containing command code plus related helpers/tests.

### Changes Required:

#### 1. Define purpose directory map
**Directory Structure**:
- `internal/cli/commands/insights/` (`affected`, `status`, shared rendering helpers)
- `internal/cli/commands/quality/` (`check`)
- `internal/cli/commands/runtime/` (`dev`, `hosts`, `infra`, `migrate`)
- `internal/cli/commands/workflow/` (`worktree`, `doctor`)
- `internal/cli/commands/meta/` (`metadata`, `list`)

**Changes**:
- Move command implementation and tests into these folders.
- Keep naming stable (`mono affected`, `mono check`, etc.).

#### 2. Replace `init()` registration with explicit registration functions
**Files**:
- Each command package exposes `Register(reg *core.Registry)`
- `internal/cli/bootstrap.go` calls all package `Register` functions

**Changes**:
- Remove side-effect registration reliance from moved command packages.
- Ensure deterministic registration order and easy discoverability.

#### 3. Keep backward-compatible command behavior
**Files**:
- `internal/cli/run.go`
- migrated command packages

**Changes**:
- No behavior drift in args parsing, output text, and error formatting.
- Preserve integration points with `tasks` and `impact` packages.

### Success Criteria:

#### Automated Verification:
- [ ] All tests still pass after command moves: `make test`
- [ ] Lint/vet passes in new package layout: `make lint`
- [ ] Build output remains valid: `make build`

#### Manual Verification:
- [ ] `./bin/mono affected --base HEAD` output remains valid.
- [ ] `./bin/mono status --base HEAD` output remains valid.
- [ ] `./bin/mono infra status` and `./bin/mono worktree list` still execute correctly.
- [ ] `./bin/mono metadata` output remains unchanged in format.

**Implementation Note**: After completing this phase and automated checks pass, pause for manual confirmation before Phase 3.

---

## Phase 3: Final Cleanup, Documentation, and Guardrails

### Overview
Remove old flat-file remnants, document the new structure, and lock in behavior with tests.

### Changes Required:

#### 1. Delete superseded flat command files
**Files**:
- Remove migrated files from `internal/cli/*.go` and `internal/cli/*_test.go` once package replacements are green.

**Changes**:
- Keep only entrypoint/bootstrap files in `internal/cli` root plus dedicated subdirectories.

#### 2. Add structure documentation
**Files**:
- `docs/` update (or `internal/cli/README.md`)

**Changes**:
- Document package responsibilities and where to add new commands.
- Include registration contract and test expectations.

#### 3. Add regression tests around dispatch and registration
**Files**:
- `internal/cli/run_test.go`
- `internal/cli/*` integration-style tests

**Changes**:
- Assert command lookup precedence remains command -> task -> service fallback.
- Assert all expected top-level commands are registered.

### Success Criteria:

#### Automated Verification:
- [ ] Full validation passes: `make check`
- [ ] Smoke commands pass: `make smoke`

#### Manual Verification:
- [ ] New command placement is obvious for contributors.
- [ ] Adding a new command in the new structure requires no hidden side-effects.
- [ ] No user-visible regressions in existing command workflows.

**Implementation Note**: After completing this phase and automated checks pass, pause for manual confirmation that structure and behavior are acceptable.

---

## Testing Strategy

### Unit Tests:
- Keep package-local tests near each command package.
- Add coverage for explicit registration wiring.
- Add focused tests for shared `core` loaders/execution helpers.

### Integration Tests:
- Preserve top-level dispatch tests in `internal/cli`.
- Keep behavior tests for command/task precedence and fallback semantics.

### Manual Testing Steps:
1. Build binary: `make build`
2. Verify help/version/metadata: `./bin/mono --help`, `./bin/mono --version`, `./bin/mono metadata`
3. Verify dispatch precedence: run known command, task (`mono test`), and custom service command.
4. Verify representative runtime commands (`infra`, `hosts`, `migrate`, `worktree`).

## Performance Considerations

- Startup cost should remain effectively unchanged; this is package/layout refactoring.
- Keep command registration O(n) and avoid filesystem discovery at runtime.
- Preserve existing task execution concurrency and caching behavior.

## Migration Notes

- Migrate command groups one directory at a time to keep reviewable diffs and reduce rollback surface.
- Keep `internal/cli/run.go` stable until all packages are moved.
- If needed, ship in multiple PRs: boundaries first, then command moves, then cleanup/docs.

## References

- Research input: `.thoughts/shared/research/2026-02-25-internal-cli-independent-tasks-purpose.md`
- Dispatch ordering: `internal/cli/run.go:11-48`
- Registry and shared command/config surface: `internal/cli/registry.go:15-244`
- Check command coupling to impact/tasks: `internal/cli/check.go:17-66`
- Existing dispatch behavior tests: `internal/cli/registry_test.go`, `internal/cli/run_test.go`
