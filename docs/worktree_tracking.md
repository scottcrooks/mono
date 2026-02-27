# Worktree Tracking With Agents

Use `mono worktree tag` + `mono worktree list --state ...` to keep a live workflow view across parallel worktrees.

## Recommended `AGENTS.md` Entry

Add this to your repository `AGENTS.md`:

```md
## Worktree Tracking
- When starting work in a worktree, run: `mono worktree tag IN_PROGRESS`
- If blocked on user input or external dependency, run: `mono worktree tag NEEDS_INPUT`
- Before final handoff, run: `mono worktree tag DONE`
- Use lowercase `--state` filters when reviewing queue health:
  - `mono worktree list --state active`
  - `mono worktree list --state needs-input`
  - `mono worktree list --state done`
```

## State Meanings

- `active`: `IN_PROGRESS` and not merged into default base branch.
- `needs-input`: tagged `NEEDS_INPUT` or merge status cannot be determined.
- `done`: tagged `DONE`, merged, and clean (no uncommitted changes).

## Typical Review Loop

```bash
mono worktree list --state active
mono worktree list --state needs-input
mono worktree list --state done
```
