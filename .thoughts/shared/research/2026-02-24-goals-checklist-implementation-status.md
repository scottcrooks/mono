---
date: 2026-02-24T20:08:48-08:00
researcher: Scott Crooks
git_commit: c292ea34608ae0ee37bdd25fbe3e4040095e045b
branch: main
repository: mono
topic: "goals.md implementation status: what is implemented vs what still needs implementation"
tags: [research, codebase, goals-checklist, cli, mono]
status: complete
last_updated: 2026-02-25
last_updated_by: Scott Crooks
last_updated_note: "Added follow-up research for B-C-G prioritization and scheduler-first strategy"
---

# Research: goals.md implementation status

**Date**: 2026-02-24T20:08:48-08:00
**Researcher**: Scott Crooks
**Git Commit**: c292ea34608ae0ee37bdd25fbe3e4040095e045b
**Branch**: main
**Repository**: mono

## Research Question
Read and understand `docs/goals.md`, then research the codebase to determine what still needs to be implemented.

## Summary
The current codebase is a Go CLI focused on service command dispatch from `services.yaml` and operational commands (`dev`, `doctor`, `hosts`, `infra`, `migrate`, `worktree`, `metadata`). Based on repository evidence, most of the `docs/goals.md` MVP checklist is not yet implemented in this repository snapshot. The code implements command dispatch, service-level command execution, local infra helpers, and developer utilities, but it does not contain the repo-level `mono.yaml` project model, impacted-change detection pipeline, plan generation, DAG task engine, task caching, Helm deploy contract, or deployment policy validation described in `goals.md`.

## Detailed Findings

### Current implemented surface
- CLI entrypoint and command dispatch are implemented in `cmd/mono/main.go` and `internal/cli/run.go`.
- Registered top-level commands are `list`, `dev`, `doctor`, `hosts`, `infra`, `migrate`, `metadata`, `worktree` (`internal/cli/*` command `registerCommand(...)` calls).
- Generic service command execution exists: unknown top-level verbs are treated as service commands resolved from `services.yaml` command maps (`internal/cli/run.go`, `internal/cli/registry.go`).
- Config model in code is `services.yaml` with `services[]` and optional `local` infrastructure (`internal/cli/registry.go`, `internal/cli/infra.go`).
- `doctor`, `infra`, `migrate`, and `worktree` provide operational workflows, but these are not the same feature set as the goals checklist requirements.

### goals.md checklist status by section

#### A. Workspace & Project Model (`docs/goals.md:7`)
- Still needed:
- `mono.yaml` single-source manifest and schema enforcement are not present.
- Project types (`service` vs `library`), owners, runtime selection, and deploy contract fields are not represented in current config structs.
- Graph validation (unknown deps, cycle checks, type rules) is not implemented.
- Current state evidence:
- Config parser reads `services.yaml` only (`internal/cli/registry.go:88-104`).
- `Service` has `Name`, `Path`, `Description`, `Depends`, `Commands` only (`internal/cli/registry.go:43-50`).

#### B. Impacted Change Detection (`docs/goals.md:35`)
- Still needed:
- No `affected` command.
- No merge-base based changed-file/project impact computation.
- No impacted closure or explain chain output.
- No status summary command for changed/impacted/planned tasks.
- Current state evidence:
- Help output/usage lists no `affected`, `status`, or `plan` command (`internal/cli/registry.go:233-270`, `./bin/mono --help`).

#### C. Task Orchestration Engine (`docs/goals.md:57`)
- Still needed:
- No `(project, task)` DAG planner.
- No dependency-ordered task graph execution.
- No configurable parallel DAG scheduling for build/lint/typecheck/test/package/deploy tasks.
- No local task cache, cache keys, no-cache flag, or cache diagnostics.
- Current state evidence:
- Service commands run sequentially per selected service (`internal/cli/registry.go:77-84`).
- `dev` command runs service dev processes concurrently, but this is runtime process orchestration, not DAG task planning (`internal/cli/dev.go:76-113`).

#### D. Developer Experience Workflows (`docs/goals.md:96`)
- Partially present:
- Targeted command invocation for a single service exists via positional args for generic commands (`internal/cli/registry.go:61-75`).
- `dev` command exists and supports graceful signal shutdown (`internal/cli/dev.go:62-113`, `internal/cli/dev.go:219-252`).
- Still needed:
- No built-in `mono check` command implementing impacted lint/typecheck/tests.
- No explicit `--all` switch handling for `mono test --all` semantics in CLI code.
- No explicit watch/recheck loop implementation in `dev`; it delegates to each service's configured `dev` command.

#### E. CI Integration (`docs/goals.md:117`)
- Still needed:
- No `mono plan --format json` command.
- No deterministic machine-readable task plan output.
- No base-ref/commit range plan parameterization.
- Current state evidence:
- No plan command is registered (`internal/cli/* registerCommand` usage).

#### F. Kubernetes Deployment (Helm-only) (`docs/goals.md:133`)
- Still needed:
- No Helm chart contract (`charts/mono-service`) in repository.
- No deploy contract schema with service-level ingress/probes/resources/env constraints.
- No `mono.env.yaml` overlays and deterministic merge strategy.
- No `mono deploy`, `mono deploy:render`, or `mono deploy:diff` commands.
- Current state evidence:
- Infra command uses direct `kubectl apply/delete/logs/port-forward` over resource manifests from config (`internal/cli/infra.go:139-255`, `internal/cli/infra.go:307-420`).

#### G. Policy & Validation (`docs/goals.md:173`)
- Partially present:
- `mono doctor` command exists but validates local tooling availability and bootstrap tasks, not manifest graph/deploy policy constraints (`internal/cli/doctor.go:18-167`).
- Still needed:
- Manifest schema validation against required owner/type/runtime fields.
- Dependency graph sanity checks tied to project model in goals.
- Deployment policy checks (`latest` tag, probe/resource/owner/ingress policies).

#### H. Minimal Supported Stack (`docs/goals.md:192`)
- Partially present:
- Command allowlist supports Node ecosystem tools (`pnpm`, `node`, `npm`, `npx`) and Go (`internal/cli/registry.go:159-177`).
- Still needed:
- Explicit Docker packaging pipeline for services.
- Kubernetes via Helm-only implementation (current infra is kubectl-manifest driven).
- Formalized no-plugin architecture around the goals checklist model.

#### I. Explicit Non-Goals (`docs/goals.md:201`)
- Not directly codified as explicit enforcement in current codebase.
- No dedicated code paths were found documenting these non-goals as validation rules.

## Code References
- `docs/goals.md:7` - Section A requirements for workspace/project model.
- `docs/goals.md:35` - Section B impacted-change requirements.
- `docs/goals.md:57` - Section C task engine requirements.
- `docs/goals.md:96` - Section D developer workflow requirements.
- `docs/goals.md:117` - Section E CI plan requirements.
- `docs/goals.md:133` - Section F Helm/deploy contract requirements.
- `docs/goals.md:173` - Section G policy/doctor requirements.
- `cmd/mono/main.go:9-11` - CLI process entrypoint.
- `internal/cli/run.go:10-40` - top-level dispatch and fallback to service command execution.
- `internal/cli/registry.go:37-50` - `Config`/`Service` model currently used.
- `internal/cli/registry.go:88-104` - `services.yaml` loader.
- `internal/cli/registry.go:233-270` - usage/command list (absence of affected/plan/deploy/status/check).
- `internal/cli/dev.go:24-113` - `dev` command service process orchestration.
- `internal/cli/doctor.go:18-167` - environment checks and bootstrap actions.
- `internal/cli/infra.go:139-255` - `infra up` kubectl deployment flow.
- `internal/cli/migrate.go:29-93` - migrate command subcommand dispatch.
- `internal/cli/worktree.go:23-44` - worktree command suite registration and dispatch.
- `Makefile:39-63` - repo-level build/lint/test/check targets (Make commands, not mono subcommands).

## Architecture Documentation
- Current architecture is command-centric CLI orchestration.
- Configuration boundary is a service list (`services.yaml`) plus optional local infra resources; it is not a project graph manifest with typed nodes.
- Execution model is direct command invocation in service directories and command-specific imperative logic (infra, migrate, worktree), without a central DAG planner or change-impact engine.
- Operational Kubernetes support is manifest+kubectl based rather than Helm contract based.

## Historical Context (from thoughts/)
- `.thoughts/shared/plans/2026-02-24-mono-cmd-top-level-migration.md` documents a completed migration objective toward `cmd/mono` + `internal/cli` layout.
- `.thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md` captures an earlier snapshot emphasizing CLI/build migration state.
- Current live code confirms the migration layout is present (`cmd/mono`, `internal/cli`, `internal/version`), and this research extends that by comparing implementation against `docs/goals.md`.

## Related Research
- `.thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md`

## Open Questions
- `services.yaml` is referenced by runtime code but is absent in this repository snapshot, so service inventory/dependency examples could not be inspected directly.
- The goals checklist references `mono.yaml` and deployment contracts; no corresponding manifest artifacts are present in this snapshot.
- `gh` CLI is not installed in this environment, so GitHub permalinks were not generated.

## Follow-up Research 2026-02-25T00:00:00-08:00
User clarification: item 1 should treat `services.yaml` as the current expected manifest name, while the rest of the `mono.yaml` goals scope is still missing.

### Clarification Evidence
- Runtime config loading is hardcoded to `services.yaml` (`internal/cli/registry.go:91`).
- Core command flows depend on that config load (`internal/cli/registry.go:54-86`, `internal/cli/dev.go:26-30`, `internal/cli/infra.go:80-90`, `internal/cli/migrate.go:38-46`).
- In the current repository snapshot, `services.yaml` is not present on disk, so runtime command flows that require config cannot load project/service inventory from this repo state.

### Updated interpretation of checklist item A1
- Manifest file naming: currently implemented expectation is `services.yaml` (not `mono.yaml`).
- Remaining A1 fields beyond service list/command map are still not represented in current structs, including project type, owner, runtime selection metadata, and deploy contract fields.

## Follow-up Research 2026-02-25T10:30:00-08:00
User direction: prioritize implementation in order `B -> C -> G`, then reassess remaining sections. Rationale provided: section A is currently sufficient for early B/C execution via existing shorthand command definitions in `services.yaml`, and portions of that config are expected to be replaced as components are standardized.

### Prioritization captured
- Priority 1: **B. Impacted Change Detection**
- Priority 2: **C. Task Orchestration Engine**
- Priority 3: **G. Policy & Validation (“No Sprawl”)**
- Reassess A/D/E/F/H/I after B/C/G implementation progress

### Implementation strategy clarification
- Use current `services.yaml` shape as the working input contract for initial B/C delivery.
- Treat config schema evolution/standardization as a later migration concern.
- Preserve scheduler behavior as the stable core while config adapters evolve.
- In practical terms, impact-set computation and orchestration semantics are intended to be locked first, with manifest standardization following.

### Repository alignment evidence
- Current config loading is `services.yaml`-based (`internal/cli/registry.go:88-104`).
- Current command execution model is service-command mapping from config (`internal/cli/registry.go:52-86`).
- `docs/goals.md` now includes an explicit priority block with `B -> C -> G` and reassessment note.
