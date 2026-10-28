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

## Dev environment files

`mono dev` supplies environment defaults from the invocation directory's `.env`
(next to the loaded `services.yaml`) and each selected service directory's `.env`.
This applies to streaming services, TUI background services, and the foreground TUI.
For example, using synthetic values:

```dotenv
# .env
APP_REGION=local
APP_COLOR=gray
```

```dotenv
# apps/sample/.env
APP_COLOR=blue
SAMPLE_FEATURE=enabled
```

```dotenv
# apps/sample2/.env
APP_COLOR=green
```

Both services receive `APP_REGION=local`. Sample receives blue and its feature
flag; sample2 receives green and never receives sample's file values. Precedence,
from lowest to highest, is repository file, service file, inherited child
environment. An inherited value wins even when explicitly empty, such as
`APP_COLOR= mono dev sample`. File values never change mono's own environment.

These are parsed values in the initial child environment, not mounted files or
shell scripts. Syntax uses godotenv v1.5.1: assignments, comments, `export`, single
and double quotes, multiline quoted values, escapes, and same-file variable
references. Later duplicate assignments win. Each file is parsed independently;
references do not expand against the repository file or inherited environment.
Shell command substitutions are never executed. Applications remain responsible
for any configuration loading they perform after launch.

Only these two `.env` locations are read, including when a service directory is
an absolute path. There is no ancestor search, `.env.local`, or environment-specific
variant. Missing files are optional. Other read errors or malformed files stop
the affected service before launch and identify the service and file path without
printing source values. TUI preparation checks all children before starting any;
streaming services start independently, so another service may already be running.
Files are read during each child's preparation; there is no atomic snapshot across
services. Restart `mono dev` to pick up edits; files are not watched or reloaded.
