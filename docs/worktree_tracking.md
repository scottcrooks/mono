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

## Pruning Merged Worktrees

The no-argument command only prunes stale Git worktree metadata and stale workflow tags. To remove clean linked worktrees whose branches are merged into the detected default base branch, run:

```bash
mono_cmd worktree prune --merged
```

Preview the selected removals and any dirty or uninspectable worktrees that will be skipped with:

```bash
mono_cmd worktree prune --merged --dry-run
```

To remove all eligible clean linked worktrees regardless of merge detection, including detached worktrees, run:

```bash
mono_cmd worktree prune --all
```

Preview that selection with `mono_cmd worktree prune --all --dry-run`.

To review eligible worktrees one at a time, run `mono_cmd worktree prune -i` or `mono_cmd worktree prune --interactive`. Each prompt uses `Keep worktree <path> (<branch>)? [Y/n]`; press Enter or `y` to keep it, and `n` to remove it. Dirty worktrees are included and marked in the prompt with a warning that removal discards uncommitted changes. Only an explicit `n` force-removes a dirty worktree. Add `--merged` to review only worktrees detected as merged.

`--all` and standalone interactive mode include clean unmerged and detached worktrees. All selection modes preserve the current worktree, the primary checkout, and the default-base checkout. Non-interactive modes skip dirty worktrees; all modes skip worktrees whose status cannot be inspected. They use refs already available locally and do not fetch. Removing a worktree retains its local branch. The no-argument command remains limited to stale Git worktree metadata and stale workflow tags.

Use `--all` or `--merged` as the selector; they cannot be combined. `--dry-run` works with either selector and cannot be combined with interactive mode.
