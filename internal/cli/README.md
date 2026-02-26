# internal/cli structure

- `run.go`: top-level entrypoint and dispatch.
- `core/`: shared command registry, config loading, usage, and service-command execution.
- `commands/`: top-level user commands organized by purpose.
  - `insights/`: `affected`, `status`
  - `quality/`: `check`
  - `runtime/`: `dev`, `hosts`, `infra`, `migrate`
  - `workflow/`: `doctor`, `worktree`
  - `meta/`: `metadata`, `list`
- `tasks/`: orchestrated task engine (`build|lint|typecheck|test|audit|package|deploy`).
- `impact/`: change detection and check planning.

## Adding a new command

1. Add command implementation in the appropriate `commands/<purpose>/` package.
2. Register it via the package-local `registerCommand("name", cmd)`.
3. Ensure the package is wired in `internal/cli/run.go` via `Register(registry)`.
4. Add package-local tests for command behavior.
