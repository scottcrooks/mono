---
date: 2026-02-25T21:13:31-08:00
researcher: Scott Crooks
git_commit: 76226f236ec9bed4c3189036988679a6941b5441
branch: main
repository: mono
topic: "Evaluate docs/goals.md and assess what remains as of current codebase"
tags: [research, codebase, goals-checklist, cli, impact, tasks]
status: complete
last_updated: 2026-02-25
last_updated_by: Scott Crooks
---

# Research: Evaluate docs/goals.md and assess what remains

**Date**: 2026-02-25T21:13:31-08:00  
**Researcher**: Scott Crooks  
**Git Commit**: 76226f236ec9bed4c3189036988679a6941b5441  
**Branch**: main  
**Repository**: mono

## Research Question
Evaluate `docs/goals.md` and provide an up-to-date assessment of what remains.

## Summary
Compared to `docs/goals.md`, sections **B** and **C** now have substantial implementation in the current codebase (impacted change detection, `affected`, `status`, `check`, DAG task execution, local cache, concurrency, and task summaries).  
Sections **A**, **E**, **F**, **G**, **H**, and **I** are still largely unimplemented relative to their checklist definitions.  
Section **D** is mixed: `check` and `worktree` are implemented; `dev` exists with clean shutdown behavior; `--all` semantics and explicit watch/recheck behavior in `mono dev` are not implemented in mono itself.

## Detailed Findings

### A. Workspace & Project Model
- Current config source is `services.yaml`, loaded directly in code (`internal/cli/core/config.go:55-70`).
- Current `Service` model includes `name/path/description/kind/archetype/depends/dev/commands` (`internal/cli/core/config.go:16-26`).
- `mono.yaml`-based repo manifest, owner fields, strict schema failure on unknown keys, and explicit project-model enforcement from `goals.md` are not present (`docs/goals.md:20-41`).
- Project graph/type enforcement in A2 is not implemented as a dedicated validator in the current config load path (`internal/cli/core/config.go:55-70`).

### B. Impacted Change Detection
- Implemented:
- Git-based change set and impacted closure are implemented in `BuildImpactReport` (`internal/cli/impact/impact.go:35-60`), including committed changes, working tree diff, and untracked files (`internal/cli/impact/impact.go:76-104`).
- File-to-service mapping uses path prefix ownership (`internal/cli/impact/impact.go:145-163`).
- Merge-base default branch resolution exists (`internal/cli/impact/impact_git.go:20-35`).
- `mono affected` and `mono affected --explain` are implemented (`internal/cli/commands/insights/affected.go:14-52`).
- `mono status` changed/impacted/planned check preview is implemented (`internal/cli/commands/insights/status.go:14-36`, `internal/cli/commands/insights/status.go:75-91`).
- Remaining vs checklist:
- `goals.md` still describes project terminology (`project`), while current implementation is service-oriented naming/output (`docs/goals.md:49-63`, `internal/cli/commands/insights/status.go:31-35`).

### C. Task Orchestration Engine
- Implemented:
- Unit of execution is `TaskNode{service, task}` (`internal/cli/tasks/tasks.go:42-50`).
- Task vocabulary includes `build|lint|typecheck|test|audit|package|deploy` identifiers (`internal/cli/tasks/tasks.go:12-30`).
- Per-archetype task templates for Go and React are implemented (`internal/cli/tasks/task_templates.go:17-51`).
- Task resolution maps selected services to command/skip reason (`internal/cli/tasks/task_resolution.go:27-56`).
- Dependency-aware DAG build + cycle detection are implemented (`internal/cli/tasks/task_graph.go:16-63`, `internal/cli/tasks/task_graph.go:65-125`).
- Parallel batch execution with configurable concurrency is implemented (`internal/cli/tasks/task_executor.go:204-229`).
- Local cache keying/storage is implemented (`internal/cli/tasks/task_cache.go:75-99`), with environment whitelist (`internal/cli/tasks/task_cache.go:16`, `internal/cli/tasks/task_cache.go:79-81`), `--no-cache` support (`internal/cli/tasks/task_flags.go:22-24`), and cache miss diagnostics (`internal/cli/tasks/task_cache.go:65-73`, `internal/cli/tasks/task_executor.go:250-252`).
- End summary and failure exit behavior are implemented (`internal/cli/tasks/task_output.go:42-46`, `internal/cli/tasks/task_executor.go:197-201`).
- Remaining vs checklist:
- `deploy` task id exists, but no default deploy template command exists; tests assert deploy is currently skipped (`internal/cli/tasks/task_templates_test.go:32-34`).
- Checklist language uses project-level terms; implementation is service/task oriented.

### D. Developer Experience Workflows
- Implemented:
- `mono check` runs impacted phases lint/typecheck/test (`internal/cli/commands/quality/check.go:38-57`).
- `mono test <project>` style targeted run behavior exists for orchestrated tasks by passing service names (`internal/cli/tasks/task_resolution.go:65-103`).
- `mono dev <service>` exists, runs configured dev commands, and handles graceful shutdown (`internal/cli/commands/runtime/dev.go:23-113`, `internal/cli/commands/runtime/dev.go:223-240`).
- `mono worktree create/list/path/remove/prune` is implemented (`internal/cli/commands/workflow/worktree.go:23-40`, `internal/cli/commands/workflow/worktree.go:46-229`, `internal/cli/commands/workflow/worktree.go:671-683`).
- Remaining vs checklist:
- `mono test --all` flag semantics are not implemented; unknown flags error in task argument parser (`internal/cli/tasks/task_flags.go:42-44`).
- `mono dev` does not implement mono-managed rebuild/recheck-on-change logic; it delegates to each service’s configured `dev` command (`internal/cli/commands/runtime/dev.go:170-179`, `internal/cli/commands/runtime/dev.go:187-194`).

### E. CI Integration
- `mono plan --format json` is not present in command surface (`internal/cli/core/usage.go:11-25`).
- Deterministic plan output and plan parameterization for CI are not implemented as a dedicated command (`docs/goals.md:143-152`).

### F. Kubernetes Deployment (Helm-Only Contract)
- No Helm chart contract files or Helm deploy commands were found in the repo (`docs/goals.md:159-192`).
- Current infrastructure command path is local `kubectl` workflow via `local` config (`services.example.yml:31-42` and `internal/cli/core/config.go:28-53`).
- `mono deploy:render` and `mono deploy:diff` commands are not present in usage (`internal/cli/core/usage.go:11-25`).

### G. Policy & Validation (“No Sprawl”)
- `mono doctor` exists but validates local tooling/bootstrap concerns (`internal/cli/commands/workflow/doctor.go:18-167`).
- Manifest schema/graph/deploy policy checks listed in goals are not implemented in doctor (`docs/goals.md:198-211`).

### H. Minimal Supported Stack (MVP Scope Control)
- Go and Node tooling are first-class in command execution allowlist (`internal/cli/core/service_exec.go:92-103`) and task templates (`internal/cli/tasks/task_templates.go:17-51`).
- Docker-based packaging and Helm-only deployment requirements are not implemented (`docs/goals.md:217-220`).

### I. Explicit Non-Goals
- The non-goals list is documented in `goals.md` (`docs/goals.md:224-233`), but no dedicated enforcement/guardrail command paths were found for these items in current CLI command surface (`internal/cli/core/usage.go:11-25`).

## Code References
- `docs/goals.md:7-13` - Priority ordering B -> C -> G and reassessment note.
- `docs/goals.md:17-233` - A-I checklist baseline.
- `internal/cli/core/config.go:10-70` - current manifest model and `services.yaml` loading.
- `services.example.yml:1-42` - example manifest shape (`services` + `local`).
- `internal/cli/impact/impact.go:35-60` - impact report construction.
- `internal/cli/impact/impact.go:76-104` - changed file set assembly.
- `internal/cli/impact/impact.go:145-208` - path ownership + impacted closure.
- `internal/cli/impact/impact.go:210-245` - explain chain derivation.
- `internal/cli/impact/impact_git.go:20-35` - default merge-base branch resolution.
- `internal/cli/commands/insights/affected.go:14-52` - `affected` command behavior.
- `internal/cli/commands/insights/status.go:14-36` - `status` command behavior.
- `internal/cli/commands/quality/check.go:17-65` - `check` command phased execution.
- `internal/cli/tasks/tasks.go:12-50` - task ids and execution unit.
- `internal/cli/tasks/task_resolution.go:27-56` - task resolution.
- `internal/cli/tasks/task_graph.go:16-63` - graph build and dependency edges.
- `internal/cli/tasks/task_graph.go:65-125` - cycle detection.
- `internal/cli/tasks/task_executor.go:30-39` - default concurrency.
- `internal/cli/tasks/task_executor.go:204-229` - parallel batch execution.
- `internal/cli/tasks/task_executor.go:241-269` - cache read/miss/store path.
- `internal/cli/tasks/task_cache.go:16-99` - cache key and entry model.
- `internal/cli/tasks/task_flags.go:15-50` - task flags (`--no-cache`, `--concurrency`, `--integration`).
- `internal/cli/tasks/task_output.go:42-46` - task summary output.
- `internal/cli/tasks/task_templates.go:17-51` - current template coverage.
- `internal/cli/tasks/task_templates_test.go:32-34` - deploy currently expected unsupported.
- `internal/cli/commands/runtime/dev.go:23-113` - `dev` runtime orchestration.
- `internal/cli/commands/workflow/worktree.go:23-40` - worktree subcommand surface.
- `internal/cli/core/usage.go:11-25` - user-facing command/task list.
- `internal/cli/commands/workflow/doctor.go:18-167` - doctor current scope.

## Architecture Documentation
- The current CLI is command-registry driven (`internal/cli/run.go:20-30`, `internal/cli/run.go:50-72`).
- It now combines:
- insights layer (`affected`, `status`) driven by git + service graph in `impact/`,
- quality gate layer (`check`) driven by pending impacted tasks,
- orchestration layer (`tasks`) with per-service/per-task DAG execution and local cache.
- Configuration source remains `services.yaml` with optional `local` infra spec, rather than the `mono.yaml` project manifest model described in goals.

## Historical Context (from thoughts/)
- `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md` captured a pre-B/C state where most checklist items were still missing.
- `.thoughts/shared/research/2026-02-25-internal-cli-independent-tasks-purpose.md` documents the newer independent task model and command surface now present in this branch.

## Related Research
- `.thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
- `.thoughts/shared/research/2026-02-24-project-change-detection-patterns.md`
- `.thoughts/shared/research/2026-02-25-internal-cli-independent-tasks-purpose.md`

## Open Questions
- GitHub permalinks were not generated because `gh` CLI is not installed in this environment (`gh repo view --json owner,name` returned command-not-found).
