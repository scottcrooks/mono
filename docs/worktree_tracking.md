# Worktree Tracking With Agents

Use `mono_cmd worktree tag` + `mono_cmd worktree list --state ...` to keep a live workflow view across parallel worktrees.

## Recommended `AGENTS.md` Entry

Add this to your repository `AGENTS.md`:

```bash
mono_cmd() {
  if command -v mono >/dev/null 2>&1; then
    mono "$@"
  elif [ -x bin/mono ]; then
    bin/mono "$@"
  elif go tool mono --help >/dev/null 2>&1; then
    go tool mono "$@"
  else
    echo "mono not found (checked: mono, bin/mono, go tool mono)" >&2
    return 127
  fi
}
```

```md
## Worktree Tracking
- When starting work in a worktree, run: `mono_cmd worktree tag IN_PROGRESS`
- If blocked on user input or external dependency, run: `mono_cmd worktree tag NEEDS_INPUT`
- Before final handoff, run: `mono_cmd worktree tag DONE`
- Use lowercase `--state` filters when reviewing queue health:
  - `mono_cmd worktree list --state active`
  - `mono_cmd worktree list --state needs-input`
  - `mono_cmd worktree list --state done`
```

## State Meanings

- `active`: `IN_PROGRESS` and not merged into default base branch.
- `needs-input`: tagged `NEEDS_INPUT` or merge status cannot be determined.
- `done`: tagged `DONE`, merged, and clean (no uncommitted changes).

## Typical Review Loop

```bash
mono_cmd worktree list --state active
mono_cmd worktree list --state needs-input
mono_cmd worktree list --state done
```
