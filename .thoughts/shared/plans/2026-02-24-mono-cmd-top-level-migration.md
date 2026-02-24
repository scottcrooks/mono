# Mono CLI Top-Level to `cmd/mono` Migration Implementation Plan

## Overview

Refactor the repository so all executable CLI functionality is owned by a proper `cmd/mono` entrypoint and supporting internal packages, enabling both standalone binary builds (`make build`) and tool installation (`go install github.com/scottcrooks/mono/cmd/mono@latest`) without relying on top-level `package main` files.

## Current State Analysis

The repository currently has a split architecture where build tooling expects a `cmd/mono` + `internal` layout, but command implementations are still at repo root:

- CLI dispatch, command registry, service command fallback, and config loading are implemented in top-level [main.go](/home/scott/.worktrees/mono/fix/main.go:14), [main.go](/home/scott/.worktrees/mono/fix/main.go:52), and [main.go](/home/scott/.worktrees/mono/fix/main.go:108).
- Command modules are also top-level and self-register via `init()` (for example [doctor.go](/home/scott/.worktrees/mono/fix/doctor.go:12), [worktree.go](/home/scott/.worktrees/mono/fix/worktree.go:17)).
- `Makefile` already builds/installs from `./cmd/mono` and injects linker flags into `github.com/scottcrooks/mono/internal/version` ([Makefile](/home/scott/.worktrees/mono/fix/Makefile:16), [Makefile](/home/scott/.worktrees/mono/fix/Makefile:29), [Makefile](/home/scott/.worktrees/mono/fix/Makefile:45)).
- Release docs assume `go install .../cmd/mono@latest` and `mono --version` behavior ([docs/release-checklist.md](/home/scott/.worktrees/mono/fix/docs/release-checklist.md:12), [docs/release-checklist.md](/home/scott/.worktrees/mono/fix/docs/release-checklist.md:15)).
- Tests currently target root `package main` command code (e.g., [hosts_test.go](/home/scott/.worktrees/mono/fix/hosts_test.go:1), [metadata_test.go](/home/scott/.worktrees/mono/fix/metadata_test.go:1), [worktree_test.go](/home/scott/.worktrees/mono/fix/worktree_test.go:1)).

## Desired End State

A consistent Go CLI layout where:

1. `cmd/mono/main.go` is the only executable entrypoint.
2. Core logic lives in importable internal packages.
3. `make build` and `go install .../cmd/mono@latest` both work from clean checkouts.
4. Linker-injected version metadata has a concrete backing package and is surfaced via CLI flags.
5. Existing command behavior (`list`, `dev`, `doctor`, `hosts`, `infra`, `migrate`, `metadata`, `worktree`, and service command fallback) remains functionally equivalent.

### Key Discoveries:
- Build tooling is already structured for `cmd/mono`, but source code is not ([Makefile](/home/scott/.worktrees/mono/fix/Makefile:11)).
- Runtime paths assume repository-root execution for config and sidecar state files (`services.yaml`, `.infra-state.json`) ([main.go](/home/scott/.worktrees/mono/fix/main.go:111), [infra.go](/home/scott/.worktrees/mono/fix/infra.go:44)).
- Release checklist expectations (`--version`) are not fully represented in current root code path.

## What We're NOT Doing

- Rewriting command semantics or changing service config schema (`services.yaml`).
- Redesigning infra, hosts, migration, or worktree feature behavior.
- Introducing Cobra/urfave/other CLI frameworks.
- Changing module path (`github.com/scottcrooks/mono`) or release versioning policy.

## Implementation Approach

Perform an incremental relocation with a stable API boundary:

- Build an internal `cli` package with explicit `Run(args []string) int` entrypoint.
- Move command implementations to package-scoped files under `internal/cli` while preserving command registration structure.
- Keep path-sensitive behavior anchored at process working directory so existing workflows still rely on repo-root invocation.
- Introduce `internal/version` package used by linker flags and `--version` output.
- Update tests to target the moved package(s), then remove root-level command source.

## Phase 1: Introduce Executable Entrypoint and Version Plumbing

### Overview
Create the new entrypoint and metadata wiring without moving all command logic yet, so tooling can compile against a stable target.

### Changes Required:

#### 1. Add version package consumed by `-ldflags`
**File**: `internal/version/version.go`
**Changes**: Define exported `Version`, `Commit`, `Date` vars with `dev/none/unknown` defaults and helper formatter.

```go
package version

var (
    Version = "dev"
    Commit  = "none"
    Date    = "unknown"
)
```

#### 2. Add `cmd/mono` entrypoint
**File**: `cmd/mono/main.go`
**Changes**: Parse os args, delegate to new internal runner, `os.Exit(code)`.

```go
package main

import (
    "os"

    "github.com/scottcrooks/mono/internal/cli"
)

func main() {
    os.Exit(cli.Run(os.Args))
}
```

#### 3. Add version/help argument handling shim
**File**: `internal/cli/run.go`
**Changes**: Implement pre-dispatch handling for `--help`, `-h`, `--version`, `version` then call existing dispatch function.

### Success Criteria:

#### Automated Verification:
- [ ] Build succeeds from new entrypoint: `make build`
- [ ] Install target succeeds: `make install-local`
- [ ] Test suite still passes (before full move may require temporary wrappers): `make test`
- [ ] Lint passes: `make lint`

#### Manual Verification:
- [ ] `./bin/mono --help` prints command usage text.
- [ ] `./bin/mono --version` prints version metadata (Version/Commit/Date).
- [ ] `go install github.com/scottcrooks/mono/cmd/mono@latest` flow works locally and binary runs.

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before Phase 2.

---

## Phase 2: Move Top-Level Command Logic into `internal/cli`

### Overview
Relocate all root CLI files into internal package structure while preserving behavior and test coverage.

### Changes Required:

#### 1. Move command registry and config model
**File**: `internal/cli/registry.go` (new, from root `main.go` pieces)
**Changes**: Move `Command`, `commands`, `registerCommand`, `Config`, `Service`, `loadConfig`, `findService`, `runCommand`, `commandFromParts`, `listServices`, and usage printers.

#### 2. Move command modules
**Files**:
- `internal/cli/dev.go`
- `internal/cli/doctor.go`
- `internal/cli/hosts.go`
- `internal/cli/infra.go`
- `internal/cli/migrate.go`
- `internal/cli/metadata.go`
- `internal/cli/worktree.go`

**Changes**: Move code mostly as-is; convert package to `cli`; keep internal calls and `init()` registration intact.

#### 3. Remove root executable sources and keep repository clean
**Files**:
- delete: `main.go`, `dev.go`, `doctor.go`, `hosts.go`, `infra.go`, `migrate.go`, `metadata.go`, `worktree.go`

**Changes**: Remove duplicates once `internal/cli` compiles and tests pass.

#### 4. Migrate tests to new package location
**Files**:
- `internal/cli/hosts_test.go` (from `hosts_test.go`)
- `internal/cli/metadata_test.go` (from `metadata_test.go`)
- `internal/cli/worktree_test.go` (from `worktree_test.go`)

**Changes**: Update package declarations/imports as needed to target moved logic directly.

### Success Criteria:

#### Automated Verification:
- [ ] Full test suite passes after move: `make test`
- [ ] Build output generated from moved code: `make build`
- [ ] Lint passes on moved packages: `make lint`
- [ ] Formatting checks pass: `make fmt-check`

#### Manual Verification:
- [ ] Core commands still work from repo root: `list`, `metadata`, `worktree list`, `doctor` dry-run behavior.
- [ ] Service command fallback still works when `services.yaml` exists (`mono test <service>` pattern).
- [ ] Error messages remain actionable for missing config and unknown services.

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before Phase 3.

---

## Phase 3: Align Build/Release Tooling and Documentation with New Layout

### Overview
Finalize tooling and docs so local builds, release checks, and go-tool install behavior are coherent and reproducible.

### Changes Required:

#### 1. Tighten source dependency tracking in Makefile
**File**: `Makefile`
**Changes**: Ensure `MONO_SRCS` includes `cmd/mono` and `internal/cli` + `internal/version`; keep `build/install-local` targets unchanged in shape.

#### 2. Update release checklist for actual command contract
**File**: `docs/release-checklist.md`
**Changes**:
- Retain `go install .../cmd/mono@latest` validation.
- Replace stale checks (e.g., `mono ping` if unsupported) with real smoke tests (`mono --help`, `mono --version`, `mono metadata`).

#### 3. Add lightweight smoke test target (optional but recommended)
**File**: `Makefile`
**Changes**: Add `smoke` target to exercise `./bin/mono --help` and `./bin/mono --version` post-build.

### Success Criteria:

#### Automated Verification:
- [ ] Aggregate checks pass: `make check`
- [ ] Local install still works: `make install-local`
- [ ] Smoke target passes (if added): `make smoke`

#### Manual Verification:
- [ ] Release checklist commands are all executable without undocumented steps.
- [ ] A clean shell session can build and run `mono` from `./bin/mono`.
- [ ] A clean module install (`go install .../cmd/mono@latest`) produces a working `mono` binary.

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation that release-readiness checks are acceptable.

---

## Testing Strategy

### Unit Tests:
- Preserve and migrate existing command helper tests (`hosts`, `metadata`, `worktree`).
- Add tests for new `Run(args)` dispatch handling:
  - `--help` routes to usage and exits 0.
  - `--version` prints linker metadata and exits 0.
  - unknown command with missing `services.yaml` surfaces clear error.

### Integration Tests:
- Build + run smoke in CI/local:
  - `make build`
  - `./bin/mono --help`
  - `./bin/mono --version`
  - `./bin/mono metadata`

### Manual Testing Steps:
1. Build from clean checkout: `make build`.
2. Verify entrypoint UX: `./bin/mono --help` and `./bin/mono --version`.
3. Verify representative command path: `./bin/mono worktree list` in git repo.
4. With a valid `services.yaml`, run a service command fallback (`./bin/mono test <service>`).

## Performance Considerations

- No meaningful runtime performance regressions expected; this is a packaging/layout refactor.
- Keep command execution path allocation-neutral by preserving existing direct `exec.Command` patterns.
- Avoid additional filesystem scans during startup beyond existing config reads.

## Migration Notes

- Sequence matters: add new entrypoint and internal packages before deleting root files to avoid broken build windows.
- If large move PR is risky, land in two PRs:
  1. add `cmd/mono` + `internal/version` + wrappers,
  2. full source migration to `internal/cli` + root cleanup.
- Keep backward compatibility for developer workflows by preserving CLI args and command names.

## References

- Related research: `.thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md`
- CLI registry and dispatch: [main.go](/home/scott/.worktrees/mono/fix/main.go:14)
- Build/install targets: [Makefile](/home/scott/.worktrees/mono/fix/Makefile:20)
- Release installability assumptions: [docs/release-checklist.md](/home/scott/.worktrees/mono/fix/docs/release-checklist.md:8)
- Representative command module using registration pattern: [worktree.go](/home/scott/.worktrees/mono/fix/worktree.go:19)
