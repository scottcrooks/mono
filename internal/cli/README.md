# internal/cli structure

- `run.go`: top-level entrypoint and dispatch.
- `core/`: shared command registry, config loading, usage, and service-command execution.
- `commands/`: top-level user commands organized by purpose.
  - `insights/`: `affected`, `status`
  - `quality/`: `check`
  - `runtime/`: `dev`, `hosts`, `infra`, `migrate`, `telemetry`
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

## Dev TUI services

Services with the `ts-ink` or `go-tui` archetype use their existing `dev` command
as an interactive terminal application. `go-tui` inherits Go's default Air
command and Go task and dependency-install behavior. Existing `commands.dev` or
`dev` values override that default.

Select at most one TUI service with `mono dev`. Its process takes over the
current terminal while other selected services run in the background. All
three standard streams must be terminals. Background output and session
lifecycle messages are retained in a private `.cache/dev/session-*.log` file;
mono prints the log path before takeover and after cleanup. On exit or
interruption, mono stops the background process groups and waits up to five
seconds before forcing remaining processes to stop. Infrastructure started for
the session continues to persist after `mono dev` exits.

## Adding a new command

Shared local observability is managed by `mono telemetry`; see
[the telemetry workflow](../../docs/telemetry.md). It has its own global configuration
and fixed namespace, independent of project manifests and `mono infra` lifecycle.
`make test-telemetry-integration` runs the explicitly gated disposable kind test;
the default test suite never contacts a live cluster.

1. Add command implementation in the appropriate `commands/<purpose>/` package.
2. Register it via the package-local `registerCommand("name", cmd)`.
3. Ensure the package is wired in `internal/cli/run.go` via `Register(registry)`.
4. Add package-local tests for command behavior.
