# Worktree Status Tagging Implementation Plan

## Overview

Add explicit workflow status tags to `mono worktree` so developers can mark each worktree as `IN_PROGRESS`, `DONE`, or `NEEDS_INPUT`, and surface that tag in `mono worktree list`.

## Current State Analysis

`mono worktree list` currently derives only Git state: branch/detached, dirty/clean, and merged-vs-base. There is no user-managed status metadata.

- Subcommand routing is in `worktreeCommand.Run` and currently supports `create|list|path|remove|prune` only (`internal/cli/commands/workflow/worktree.go:23`).
- List output currently prints `status: <dirty|clean|unknown>` and merged state (`internal/cli/commands/workflow/worktree.go:112`).
- Worktree identity resolution by branch or basename already exists and should be reused for tagging operations (`internal/cli/commands/workflow/worktree.go:407`).
- Existing tests cover arg parsing and worktree porcelain parsing but do not cover any custom metadata behavior (`internal/cli/commands/workflow/worktree_test.go:11`).

## Desired End State

After implementation:

1. `mono worktree tag <branch-or-id> <IN_PROGRESS|DONE|NEEDS_INPUT>` writes status metadata for the resolved worktree.
2. `mono worktree list` prints both:
   - Git cleanliness status (current behavior)
   - Workflow tag status (new behavior)
3. Tag metadata is local-only and stored under `.git/` so it is not committed or shared accidentally.
4. Removing a worktree also removes its stored tag metadata.
5. Tag records for deleted worktrees can be cleaned automatically during `prune`.

### Key Discoveries:
- Current list output has a `status:` field already used for dirty/clean; new workflow tag needs a distinct label to avoid ambiguity (`internal/cli/commands/workflow/worktree.go:160`).
- Path-based identity is stable across list and remove flows, which makes path-keyed metadata straightforward (`internal/cli/commands/workflow/worktree.go:194`).
- Goals doc already expects `worktree list` status reporting, so extending output is aligned with existing direction (`docs/goals.md:129`).

## What We're NOT Doing

- Adding remote/shared/team-wide worktree status synchronization.
- Adding arbitrary/free-form tags beyond `IN_PROGRESS|DONE|NEEDS_INPUT`.
- Changing existing dirty/clean or merged-state computations.
- Modifying the default worktree location strategy (`~/.worktrees/<repo>/<id>`).

## Implementation Approach

Introduce a local metadata store at `<repo>/.git/mono-worktree-statuses.yaml` keyed by absolute worktree path. Extend worktree command surface with a `tag` subcommand and merge metadata into list rendering.

## Phase 1: Metadata Model and Storage Helpers

### Overview

Add typed status constants and deterministic read/write helpers for local status metadata.

### Changes Required:

#### 1. Add workflow status types and validation
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- Add constants for `IN_PROGRESS`, `DONE`, `NEEDS_INPUT`.
- Add parser/validator that normalizes input and rejects invalid status values.

```go
type worktreeWorkflowStatus string

const (
    statusWorking    worktreeWorkflowStatus = "IN_PROGRESS"
    statusDone       worktreeWorkflowStatus = "DONE"
    statusNeedsInput worktreeWorkflowStatus = "NEEDS_INPUT"
)
```

#### 2. Add metadata file model and IO helpers
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- Add helpers to compute repo metadata path (`.git/mono-worktree-statuses.yaml`).
- Add load/save helpers with empty-file-safe behavior.
- Use deterministic serialization order when writing to keep diffs and tests stable.

```go
type worktreeStatusStore struct {
    Worktrees map[string]worktreeWorkflowStatus `yaml:"worktrees"`
}
```

### Success Criteria:

#### Automated Verification:
- [ ] Formatting passes: `make fmt-check`
- [ ] Static checks pass: `make lint`
- [ ] Existing tests still pass: `make test`
- [ ] New metadata helper tests pass: `go test ./internal/cli/commands/workflow -run 'Test(Load|Save)WorktreeStatus'`

#### Manual Verification:
- [ ] No metadata file is created during unrelated commands like `mono worktree path`.
- [ ] Metadata file is created only when first tag is set.

**Implementation Note**: After this phase and automated checks pass, pause for human manual confirmation before Phase 2.

---

## Phase 2: Add Tag Command and Integrate List Output

### Overview

Introduce a dedicated `tag` subcommand and display workflow tag state in `mono worktree list`.

### Changes Required:

#### 1. Add `tag` subcommand routing and usage text
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- Extend subcommand switch to include `tag`.
- Add `runWorktreeTag(args []string)` and parser for `<branch-or-id> <status>`.
- Update usage output to include tag command.

#### 2. Persist selected tag by resolved worktree path
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- Reuse `resolveWorktree(...)` for target selection.
- Write tag into store keyed by `entry.Path`.
- Print confirmation like `[ok] Tagged worktree <path> as DONE`.

#### 3. Render tag in `worktree list`
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- Load metadata once before iterating entries.
- Keep existing git cleanliness output but rename label from `status:` to `git-status:` for clarity.
- Add `workflow-status: <IN_PROGRESS|DONE|NEEDS_INPUT|unset>` line per worktree.

#### 4. Add tests for parsing, set/list behavior, and invalid statuses
**File**: `internal/cli/commands/workflow/worktree_test.go`
**Changes**:
- Add arg parser tests for `tag`.
- Add metadata integration tests for list rendering of set/unset tags.
- Add invalid-status rejection tests.

### Success Criteria:

#### Automated Verification:
- [ ] Worktree package tests pass: `go test ./internal/cli/commands/workflow`
- [ ] Existing CLI tests still pass: `make test`
- [ ] Build succeeds: `make build`

#### Manual Verification:
- [ ] `mono worktree tag <id> IN_PROGRESS` sets and persists tag.
- [ ] `mono worktree list` shows both `git-status` and `workflow-status`.
- [ ] Invalid status values return clear error and non-zero exit.

**Implementation Note**: After this phase and automated checks pass, pause for human manual confirmation before Phase 3.

---

## Phase 3: Lifecycle Cleanup and Regression Hardening

### Overview

Keep metadata accurate when worktrees are removed/pruned and lock behavior with focused tests.

### Changes Required:

#### 1. Remove tag entry on `worktree remove`
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- After successful `git worktree remove`, delete metadata map entry for removed path and persist store.

#### 2. Garbage-collect stale entries on `worktree prune`
**File**: `internal/cli/commands/workflow/worktree.go`
**Changes**:
- After prune, compare stored paths against live `git worktree list --porcelain` entries.
- Remove non-existent/stale keys and persist if changed.

#### 3. Add regression tests for lifecycle cleanup
**File**: `internal/cli/commands/workflow/worktree_test.go`
**Changes**:
- Unit tests for stale cleanup helper logic.
- Ensure remove path cleanup does not affect other worktree tags.

### Success Criteria:

#### Automated Verification:
- [ ] New lifecycle tests pass: `go test ./internal/cli/commands/workflow -run 'TestWorktree(StatusCleanup|RemoveCleanup)'`
- [ ] Full suite passes: `make test`
- [ ] Full validation passes: `make check`

#### Manual Verification:
- [ ] Removing a tagged worktree removes that row’s workflow status from future `list` output.
- [ ] Pruning cleans stale metadata rows without touching active worktrees.

**Implementation Note**: After this phase and automated checks pass, pause for human manual confirmation before merge.

---

## Testing Strategy

### Unit Tests:
- Status validation/parser (accept exact allowed constants, reject others).
- Metadata load/save behavior including empty/missing file scenarios.
- Cleanup helpers for stale paths and remove path behavior.

### Integration Tests:
- `runWorktreeTag` + metadata + list output interaction.
- Remove/prune flows preserving existing command behavior while updating metadata.

### Manual Testing Steps:
1. Create a worktree: `mono worktree create feature/tag-demo --id tag-demo --no-bootstrap`
2. Tag it: `mono worktree tag tag-demo IN_PROGRESS`
3. Confirm list output includes `workflow-status: IN_PROGRESS`.
4. Change tag to `NEEDS_INPUT`, re-run list, and verify updated output.
5. Remove worktree and confirm workflow status no longer appears.

## Performance Considerations

- Metadata IO is a single small YAML file read/write per relevant command; overhead should be negligible relative to existing git subprocess calls.
- `list` should load metadata once (not per worktree) to avoid unnecessary filesystem churn.

## Migration Notes

- No schema/data migration required.
- Existing users can continue using `worktree list` without tagging; output should show `workflow-status: unset` until explicitly tagged.

## References

- Worktree command implementation: `internal/cli/commands/workflow/worktree.go:23`
- List rendering and current status labels: `internal/cli/commands/workflow/worktree.go:112`
- Worktree resolution helper: `internal/cli/commands/workflow/worktree.go:407`
- Existing worktree tests baseline: `internal/cli/commands/workflow/worktree_test.go:11`
- Goals checklist for worktree list behavior: `docs/goals.md:129`
