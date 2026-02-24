---
date: 2026-02-24T11:45:49-08:00
researcher: Scott Crooks
git_commit: 22e7870558664f8ffcc85fcca93c006ca215f226
branch: merge_work
repository: fix
topic: "Current repository state: top-level CLI functionality and scaffolded build tooling"
tags: [research, codebase, cli, build-tooling, go]
status: complete
last_updated: 2026-02-24
last_updated_by: Scott Crooks
---

# Research: Current repository state: top-level CLI functionality and scaffolded build tooling

**Date**: 2026-02-24T11:45:49-08:00  
**Researcher**: Scott Crooks  
**Git Commit**: 22e7870558664f8ffcc85fcca93c006ca215f226  
**Branch**: merge_work  
**Repository**: fix

## Research Question
Review the current repo with emphasis on top-level CLI functionality and scaffolded build tooling.

## Summary
The repository is a Go CLI codebase with command implementations placed at the repository root (`main.go`, `dev.go`, `doctor.go`, `hosts.go`, `infra.go`, `migrate.go`, `metadata.go`, `worktree.go`). Command registration uses `init()` functions per file and a shared `commands` map in `main.go`. Runtime behavior relies on `services.yaml` for service and local infra configuration, but `services.yaml` is not present in this worktree. Build/test tooling exists in `Makefile` and `docs/release-checklist.md`; the `Makefile` references `cmd/mono` and `internal` source paths while the current CLI source files are at repo root.

## Detailed Findings

### CLI command dispatch and config model
- `main()` dispatches by first argument and executes registered commands via the `Command` interface (`main.go:13`, `main.go:52`).
- Unknown subcommands fall back to `runServiceCommand`, which executes named command entries from service config (`main.go:69`, `main.go:74`).
- `loadConfig()` reads `services.yaml` from repo root and unmarshals to `Config` containing `Services` and optional `Local` infra spec (`main.go:37`, `main.go:109`, `main.go:111`).
- `runCommand()` resolves service path, parses command string with `strings.Fields`, and uses an allowlisted command constructor (`main.go:144`, `main.go:154`, `main.go:179`).
- Allowlisted executables are `go`, `pnpm`, `npx`, `node`, `npm` (`main.go:184`).

### Top-level command modules
- `dev` starts multiple service `dev` commands concurrently, manages signal-based shutdown, and auto-starts infra dependencies declared in `depends` (`dev.go:20`, `dev.go:24`, `dev.go:58`, `dev.go:105`).
- `doctor` checks/install dependencies (`go`, `node`, `pnpm`, optional `kubectl`), installs Go tools from `go.mod` `tool (...)` block if present, runs `pnpm install --frozen-lockfile`, and configures git hooks (`doctor.go:15`, `doctor.go:21`, `doctor.go:152`, `doctor.go:186`, `doctor.go:112`, `doctor.go:238`).
- `hosts` manages an `/etc/hosts` block delimited by `# BEGIN ARGUS HOSTS`/`# END ARGUS HOSTS`, with DNS label validation and sync/remove subcommands (`hosts.go:12`, `hosts.go:19`, `hosts.go:26`, `hosts.go:36`, `hosts.go:113`).
- `infra` manages local Kubernetes resources (`up/down/status/logs`), loads `local` config from `services.yaml`, tracks port-forward PIDs in `.infra-state.json`, and shells out to `kubectl` (`infra.go:17`, `infra.go:47`, `infra.go:52`, `infra.go:80`, `infra.go:132`, `infra.go:278`).
- `migrate` supports `<service> up/down/status/create`, resolves DSN from env or service `.env`, and uses `golang-migrate` for SQL migrations (`migrate.go:24`, `migrate.go:29`, `migrate.go:95`, `migrate.go:195`, `migrate.go:264`).
- `metadata` prints current time plus git metadata when available (`metadata.go:14`, `metadata.go:30`, `metadata.go:36`, `metadata.go:69`).
- `worktree` provides `create/list/path/remove/prune`; create flow includes optional bootstrap, copying `.env` files from `apps/` and `packages/`, and running per-service `reqs` command from `services.yaml` (`worktree.go:20`, `worktree.go:31`, `worktree.go:44`, `worktree.go:489`, `worktree.go:536`, `worktree.go:627`).

### Build and release tooling present in repo
- `Makefile` defines `build/test/lint/fmt/fmt-check/install-local/check` targets (`Makefile:20`, `Makefile:31`, `Makefile:34`, `Makefile:37`, `Makefile:43`, `Makefile:47`).
- Build inputs in `Makefile` are computed from `cmd/mono` and `internal` directories (`Makefile:11`, `Makefile:27`).
- Build target compiles `./cmd/mono` with version metadata via linker flags (`Makefile:17`, `Makefile:29`).
- `docs/release-checklist.md` documents semantic versioning, `make check`, `go install .../cmd/mono@latest`, and release tagging flow (`docs/release-checklist.md:1`).

### Observable repository contents at research time
- Root contains Go source files and tests, `Makefile`, `docs/`, and a checked-in executable `mono`; no `cmd/`, `internal/`, `services.yaml`, or `thoughts/` directories exist in this worktree snapshot.
- Attempting `./bin/mono metadata` returned `no such file or directory` because `./bin/mono` is not present.
- Attempting `./mono metadata` returned config load failure referencing missing `services.yaml`.

## Code References
- `main.go:13` - `Command` interface used by all subcommands.
- `main.go:52` - CLI argument dispatch entrypoint.
- `main.go:74` - Generic per-service command execution path.
- `main.go:109` - `services.yaml` loading and parsing.
- `main.go:179` - allowlisted command builder.
- `dev.go:24` - `dev` command flow and service selection.
- `dev.go:105` - infra dependency auto-start behavior.
- `doctor.go:21` - environment validation flow.
- `doctor.go:186` - `go.mod` `tool (...)` parser.
- `hosts.go:19` - `/etc/hosts` target path.
- `infra.go:47` - `.infra-state.json` storage.
- `infra.go:132` - deploy flow (`infra up`).
- `migrate.go:29` - migrate subcommand dispatch.
- `migrate.go:95` - DSN resolution order.
- `metadata.go:36` - metadata collection with git fallback.
- `worktree.go:44` - `worktree create` flow.
- `worktree.go:536` - `.env` discovery/copy across `apps/` and `packages/`.
- `worktree.go:627` - service `reqs` execution based on `services.yaml`.
- `Makefile:11` - source discovery rooted at `cmd/mono` + `internal`.
- `Makefile:27` - binary build recipe and dependencies.

## Architecture Documentation
- Command-oriented CLI architecture with one file per major feature area and shared command registration through `init()`.
- Config-driven service orchestration where service actions (`test/lint/build/dev/reqs`) are read from `services.yaml` command strings and executed in service working directories.
- Specialized operational commands:
  - hostfile management for local DNS names (`hosts`),
  - Kubernetes resource lifecycle and local port forwarding (`infra`),
  - SQL migration lifecycle (`migrate`),
  - worktree lifecycle plus bootstrap automation (`worktree`),
  - environment inspection/setup (`doctor`),
  - metadata emission for documentation/handoffs (`metadata`).
- Build metadata embedding pattern in `Makefile` via linker flags (`Version`, `Commit`, `Date`) targeting package path `github.com/scottcrooks/mono/internal/version`.

## Historical Context (from thoughts/)
No `thoughts/` directory exists in this repository snapshot, so no historical notes were discovered in-tree.

## Related Research
No additional research documents were found in this worktree snapshot.

## Open Questions
- `services.yaml` is referenced broadly by runtime commands but is absent in this worktree snapshot.
- `Makefile` build inputs reference `cmd/mono` and `internal`, while current CLI source files are at repository root.
- The checked-in `mono` binary behavior differs from current source behavior for `metadata` execution in this worktree.
