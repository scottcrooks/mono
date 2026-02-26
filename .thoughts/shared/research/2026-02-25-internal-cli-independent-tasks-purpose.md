---
date: 2026-02-25T20:41:12-08:00
researcher: Scott Crooks
git_commit: da9a070842c5fbfabda2c0ace69617dcb6c3258f
branch: cliRefactor
repository: clirefactor-20260225-203808
topic: "Document each independent task in internal/cli and its purpose"
tags: [research, codebase, cli, task-orchestration, internal-cli]
status: complete
last_updated: 2026-02-25
last_updated_by: Scott Crooks
---

# Research: Document each independent task in internal/cli and its purpose

**Date**: 2026-02-25T20:41:12-08:00  
**Researcher**: Scott Crooks  
**Git Commit**: da9a070842c5fbfabda2c0ace69617dcb6c3258f  
**Branch**: cliRefactor  
**Repository**: clirefactor-20260225-203808

## Research Question
Document each independent task in `internal/cli` and its purpose.

## Summary
`internal/cli` defines two task layers:
1. Orchestrated task units (`build`, `lint`, `typecheck`, `test`, `audit`, `package`, `deploy`) that execute per service as `TaskNode{service, task}`.
2. User-invoked CLI commands/subcommands (for example `check`, `infra up`, `worktree create`) that perform specific operational workflows.

The orchestrated tasks are resolved from service archetype templates (`go`/`react`) and executed with dependency-aware parallel scheduling.

## Detailed Findings

### Orchestrated Independent Tasks (TaskName)
- `build`: Build artifacts for a service/package.
  - Defined as a canonical task id in `internal/cli/tasks.go:13`.
  - Default commands: `go build ./...` for Go service/package, `pnpm build` for React service/package (`internal/cli/task_templates.go:20`, `internal/cli/task_templates.go:28`, `internal/cli/task_templates.go:36`, `internal/cli/task_templates.go:44`).
- `lint`: Static linting task.
  - Task id: `internal/cli/tasks.go:14`.
  - Default commands: `go tool golangci-lint run ./...` (Go), `pnpm lint` (React) (`internal/cli/task_templates.go:21`, `internal/cli/task_templates.go:29`, `internal/cli/task_templates.go:37`, `internal/cli/task_templates.go:45`).
- `typecheck`: Type checking task.
  - Task id: `internal/cli/tasks.go:15`.
  - Default commands: `go test -run=^$ ./...` (Go service), `pnpm typecheck` (React service/package) (`internal/cli/task_templates.go:22`, `internal/cli/task_templates.go:38`, `internal/cli/task_templates.go:46`).
- `test`: Test execution task.
  - Task id: `internal/cli/tasks.go:16`.
  - Default commands: `go test ./...` (Go), `pnpm test` (React) (`internal/cli/task_templates.go:23`, `internal/cli/task_templates.go:30`, `internal/cli/task_templates.go:39`, `internal/cli/task_templates.go:47`).
  - Integration mode (`--integration`) is only supported for `test`; Go runs `go test -v ./...`, React runs `pnpm test:integration` when script exists (`internal/cli/task_templates.go:58`, `internal/cli/task_templates.go:64`, `internal/cli/task_templates.go:66`, `internal/cli/task_executor.go:51`).
- `audit`: Security/vulnerability audit task.
  - Task id: `internal/cli/tasks.go:17`.
  - Default commands: `go tool govulncheck ./...` (Go), `pnpm audit` (React) (`internal/cli/task_templates.go:24`, `internal/cli/task_templates.go:31`, `internal/cli/task_templates.go:40`, `internal/cli/task_templates.go:48`).
  - This is the only orchestrated task configured to continue despite failures and the only one without cache usage (`internal/cli/task_executor.go:272`, `internal/cli/task_executor.go:276`).
- `package`: Packaging/release build task.
  - Task id: `internal/cli/tasks.go:18`.
  - Default commands: `go build ./...` (Go service), `pnpm build` (React service) (`internal/cli/task_templates.go:25`, `internal/cli/task_templates.go:41`).
- `deploy`: Deployment task id reserved in orchestrated ordering.
  - Task id: `internal/cli/tasks.go:19` and order includes it (`internal/cli/tasks.go:29`).
  - No default template command is defined in `task_templates.go`, so services without explicit support resolve to unsupported/skip for this task (`internal/cli/task_templates.go:77`, `internal/cli/task_templates.go:95`).

### Task Execution Unit
- Independent execution unit is `TaskNode` (`service:task`) (`internal/cli/tasks.go:42`).
- `runOrchestratedTask` parses task flags and resolves service selection before execution (`internal/cli/task_executor.go:41`).
- `resolveTaskRequest` binds each selected service to a command or skip reason for the chosen task (`internal/cli/task_resolution.go:27`).
- `buildTaskGraph` builds dependency edges across services for the same task and detects cycles before scheduling (`internal/cli/task_graph.go:16`, `internal/cli/task_graph.go:54`).

### CLI Command Tasks in `internal/cli`
Top-level command registrations and purposes:
- `affected`: List impacted services from git change detection; optional dependency-chain explanation (`internal/cli/affected.go:11`, `internal/cli/affected.go:25`, `internal/cli/affected.go:34`).
- `check`: Run pending impacted-service checks in phases (`lint`, `typecheck`, `test`) (`internal/cli/check.go:14`, `internal/cli/check.go:38`, `internal/cli/impact.go:273`).
- `list`: Print services and available commands/tasks (`internal/cli/registry.go:30`, `internal/cli/registry.go:188`).
- `status`: Show changed/impacted services and planned check tasks (`internal/cli/status.go:11`, `internal/cli/status.go:25`, `internal/cli/status.go:35`).
- `dev`: Start service dev commands concurrently and auto-start infra dependencies (`internal/cli/dev.go:20`, `internal/cli/dev.go:24`, `internal/cli/dev.go:58`).
- `doctor`: Validate/install local development prerequisites and bootstrap repo tooling (`internal/cli/doctor.go:15`, `internal/cli/doctor.go:19`).
- `hosts`: Manage `/etc/hosts` managed block.
  - `hosts sync`: upsert service host entries (`internal/cli/hosts.go:37`, `internal/cli/hosts.go:47`).
  - `hosts remove`: remove managed block (`internal/cli/hosts.go:39`, `internal/cli/hosts.go:86`).
- `infra`: Manage local Kubernetes infrastructure.
  - `infra up`: apply manifests, wait ready, start port-forwards (`internal/cli/infra.go:66`, `internal/cli/infra.go:139`).
  - `infra down`: remove manifests and stop port-forwards (`internal/cli/infra.go:68`, `internal/cli/infra.go:258`).
  - `infra status`: show pod/port-forward status (`internal/cli/infra.go:70`, `internal/cli/infra.go:335`).
  - `infra logs`: tail resource logs (`internal/cli/infra.go:72`, `internal/cli/infra.go:382`).
- `migrate`: Manage service DB migrations.
  - `migrate up`: apply pending migrations (`internal/cli/migrate.go:73`, `internal/cli/migrate.go:195`).
  - `migrate down [N]`: rollback migration steps (`internal/cli/migrate.go:78`, `internal/cli/migrate.go:216`).
  - `migrate status`: print DB migration version/dirty state (`internal/cli/migrate.go:88`, `internal/cli/migrate.go:241`).
  - `migrate create <name>`: create next numbered up/down SQL files (`internal/cli/migrate.go:60`, `internal/cli/migrate.go:264`).
- `metadata`: print date/time + git metadata (`internal/cli/metadata.go:14`, `internal/cli/metadata.go:31`, `internal/cli/metadata.go:37`).
- `worktree`: Manage git worktree lifecycle.
  - `create`: create worktree + optional bootstrap/reqs (`internal/cli/worktree.go:30`, `internal/cli/worktree.go:46`).
  - `list`: show worktrees with dirty/merged status (`internal/cli/worktree.go:32`, `internal/cli/worktree.go:112`).
  - `path`: resolve path by branch/id (`internal/cli/worktree.go:34`, `internal/cli/worktree.go:168`).
  - `remove`: remove target worktree, optional force (`internal/cli/worktree.go:36`, `internal/cli/worktree.go:182`).
  - `prune`: prune stale worktree metadata (`internal/cli/worktree.go:38`, `internal/cli/worktree.go:220`).

### Dispatch Behavior Connecting Command and Task Layers
- `Run(args)` first checks registered commands, then checks if the token is an orchestrated task name, then falls back to service-defined command passthrough (`internal/cli/run.go:26`, `internal/cli/run.go:35`, `internal/cli/run.go:43`).
- This means `mono lint`, `mono test`, etc. are interpreted as orchestrated tasks when matching `TaskName` values (`internal/cli/tasks.go:52`, `internal/cli/run.go:35`).

## Code References
- `internal/cli/tasks.go:13` - canonical orchestrated task ids.
- `internal/cli/task_templates.go:17` - archetype-based task command templates.
- `internal/cli/task_resolution.go:27` - map requested task to per-service execution nodes.
- `internal/cli/task_graph.go:16` - dependency graph for task scheduling.
- `internal/cli/task_executor.go:41` - orchestrated task entrypoint and execution.
- `internal/cli/impact.go:273` - pending-check phase plan (`lint/typecheck/test`).
- `internal/cli/run.go:11` - top-level CLI dispatch order.
- `internal/cli/registry.go:247` - user-facing command/task usage surface.

## Architecture Documentation
- Task model: `TaskName` + `TaskNode` identify normalized orchestration units.
- Resolution model: requested task + selected services -> `ResolvedTaskNode` list with command/skip reason.
- Scheduling model: service dependency graph controls ordering for each task phase; independent ready nodes run in parallel with configurable concurrency.
- Command model: feature commands register via `init()` and are dispatched by name from a shared command registry.

## Historical Context (from thoughts/)
Related existing documents found under `.thoughts/shared/research/`:
- `.thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md`
- `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
- `.thoughts/shared/research/2026-02-24-project-change-detection-patterns.md`

No additional `thoughts/` directory exists in this worktree; repository research notes are stored under `.thoughts/`.

## Related Research
- `.thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md`
- `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
- `.thoughts/shared/research/2026-02-24-project-change-detection-patterns.md`

## Open Questions
- Whether the user intends “independent task” to mean only orchestrated `TaskName` units, or all command/subcommand workflows in `internal/cli`.
