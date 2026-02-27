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

## Output Conventions

- Use `internal/cli/output` for human-facing command output.
- Use semantic printer methods for status lines:
  - `StepStart` for action start
  - `StepOK` for success
  - `StepWarn` for warnings/skips
  - `StepErr` for failures
  - `Summary` for neutral informational lines
- Use `output.NewPrefixWriter(...)` for subprocess stdout/stderr prefixing instead of local duplicate implementations.
- Output mode policy:
  - interactive terminals use semantic color and symbols
  - non-interactive/CI/`NO_COLOR=1` runs emit plain deterministic text

## Adding a new command

1. Add command implementation in the appropriate `commands/<purpose>/` package.
2. Register it via the package-local `registerCommand("name", cmd)`.
3. Ensure the package is wired in `internal/cli/run.go` via `Register(registry)`.
4. Add package-local tests for command behavior.
