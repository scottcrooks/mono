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

Every subprocess launched by mono receives `CI=true` when its effective child
environment does not contain `CI`. An inherited or dotenv `CI` entry wins,
including an explicitly empty value or `CI=false`; inherited values retain the
existing highest precedence. This changes only the child environment and does
not change mono's own environment. The value is an environment default for child
tools; mono does not promise that every tool changes its prompting behavior.

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

## Focused integration tests

`mono test --integration` preserves the existing broad test behavior unless the
repository opts into focused intent with this root `services.yaml` setting:

```yaml
integration:
  requireFocus: true
```

With that policy enabled, every integration invocation needs `--package`,
`--run`, or the explicit `--full-suite` intent. Service names and `--all` still
select services; `--full-suite` does not mean all services. Package selectors
repeat and use Go package-pattern syntax. `--run` is a Go regular expression;
slash-separated subtest components are passed through to Go, while list output
shows only approximate top-level test candidates. Listing performs Go package
discovery and test listing, but does not execute test functions, preflight
commands, or test cache operations. Directly selected services must match; an
empty selection there fails before tests run. Dependencies remain in the task
graph and ordering, but an unmatched dependency test task is skipped. A focused
request fails if a runnable selected node is not Go.

`--go-concurrency N` sets Go's package parallelism with `-p N` during discovery
and test execution; it is independent of Mono's task-level `--concurrency`.
When explicitly set, it conflicts with `GOFLAGS` containing `-p` or `-p=...`.
GOFLAGS values are not printed. Focused runs bypass the task cache so a broad
suite cache entry cannot mask selection. Use `--no-cache` when external service
state may have changed.

Services may configure an application-owned integration preflight. It uses the
same shell-free allowlisted command syntax, service directory, and effective
integration environment as the test child:

```yaml
services:
  - name: flock
    integrationPreflight: node scripts/check-integration-env.js
```

Mono runs the preflight after selection succeeds and before cache lookup or the
test, including before a cache hit. Failure or cancellation prevents the test
and cache use. Its command participates in the integration cache identity.
Preflight output is command output; Mono does not log environment values.

Proposed Flock migration example (the checker path is consumer-specific and
this example does not assert that a Flock configuration has been verified):

```yaml
integration:
  requireFocus: true
services:
  - name: flock
    integrationPreflight: node scripts/check-integration-env.js
```

```sh
mono test flock --integration --package './internal/store/...' --run '^TestStore$' --go-concurrency 2
```

A Flock wrapper can delegate package/test selection and `test.env` loading to
Mono. Keep resource checks in the application checker; package selection does
not require a per-package `TestMain` guard.

Only these two `.env` locations are read, including when a service directory is
an absolute path. There is no ancestor search, `.env.local`, or environment-specific
variant. Missing files are optional. Other read errors or malformed files stop
the affected service before launch and identify the service and file path without
printing source values. TUI preparation checks all children before starting any;
streaming services start independently, so another service may already be running.
Files are read during each child's preparation; there is no atomic snapshot across
services. Restart `mono dev` to pick up edits; files are not watched or reloaded.

## Integration test environment files

Run integration tests explicitly with `mono test --integration [service ...]`.
This is a separate task invocation; `mono check` does not add integration tests.
Integration test children read `test.env` from the invocation directory (the
directory where `mono` is run) and the selected service directory. The service
file overrides the invocation file, and the environment inherited by `mono`
overrides both, including explicitly empty values. These files are parsed
independently with godotenv, so references expand only within their own file and
shell command substitutions remain literal. Missing files are optional.

The overlay applies only to runnable integration test children. It does not read
development `.env` files, affect ordinary tests or other tasks, or configure
dependency installs. It never changes mono's own environment. A read or parse
error fails the affected service before cache lookup or launch and identifies
the file without exposing parsed values.

Integration test cache identity includes the integration profile and a digest
of the complete effective child environment. Environment values are not stored
in cache metadata. Existing source hashing can also invalidate a result when a
file changes. Cache identity does not track external services or other external
state; pass `--no-cache` to force the integration test to run.
