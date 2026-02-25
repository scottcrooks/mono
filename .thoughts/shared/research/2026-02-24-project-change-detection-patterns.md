---
date: 2026-02-24T20:25:37-08:00
researcher: Scott Crooks
git_commit: fd13aa21bea328b3574814e0431585e4babbe3fb
branch: feature/change-detection
repository: mono
topic: "Existing patterns and practices relevant to project change detection (with goals.md:35 context)"
tags: [research, codebase, change-detection, project-graph, cli]
status: complete
last_updated: 2026-02-24
last_updated_by: Scott Crooks
last_updated_note: "Added follow-up research with populated services.example.yml registry evidence"
---

# Research: Existing patterns and practices relevant to project change detection

**Date**: 2026-02-24T20:25:37-08:00
**Researcher**: Scott Crooks
**Git Commit**: fd13aa21bea328b3574814e0431585e4babbe3fb
**Branch**: feature/change-detection
**Repository**: mono

## Research Question
Look at existing patterns and practices with a mind for implementing change detection for projects (`docs/goals.md:35`).

## Summary
Current repository patterns are centered on a `services.yaml` service model and command dispatch, with several reusable building blocks around dependency fields, deterministic ordering, git merge-base helpers, and strict argument validation. There is not yet a project-level impacted-change command, but existing code already contains:

- A declared dependency field (`Service.Depends`) and repeated validation/selection flows.
- Git merge-base and base-branch detection helpers (currently used in worktree status).
- Deterministic sorting and stable output practices.
- Test patterns that validate git-driven logic and parser behavior.

The `docs/goals.md:35` rule (`libraries cannot depend on services`) is in section A2 graph rules; current runtime config does not yet include explicit project types, but dependency references are already modeled as names and validated at selection time.

## Detailed Findings

### Goals Context Around `docs/goals.md:35`
- `docs/goals.md:35` is a graph type-rule line under A2: `libraries cannot depend on services`.
- Impacted change detection requirements are defined in section B immediately after A2 (`docs/goals.md:45-63`), including merge-base comparisons and impacted closure.
- This places `:35` as project-graph policy context for the later impacted-change calculations.

### Current Project/Dependency Data Model
- Runtime config model is `Config` with `Services []Service` and optional `Local *InfraSpec` loaded from `services.yaml` (`internal/cli/registry.go:37-41`, `internal/cli/registry.go:89-104`).
- `Service` includes `Depends []string` as the declared dependency list (`internal/cli/registry.go:43-50`).
- `findService` is the shared lookup primitive by service name and is reused across multiple commands (`internal/cli/registry.go:106-114`, `internal/cli/dev.go:37`, `internal/cli/migrate.go:43`, `internal/cli/hosts.go:120`).
- `list` renders dependency values as part of command output (`internal/cli/registry.go:194-196`).

### Existing Dependency Resolution/Validation Practices
- `dev.ensureInfraDeps` scans `Depends`, deduplicates with a `seen` map, and validates each reference against known local resources (`internal/cli/dev.go:123-132`, `internal/cli/dev.go:143-152`).
- Unknown dependency references return explicit errors (`internal/cli/dev.go:151`).
- Service selection code follows a repeatable pattern: collect requested names, resolve each name, fail on unknown, otherwise operate on resolved entities (`internal/cli/registry.go:61-71`, `internal/cli/hosts.go:109-127`, `internal/cli/infra.go:154-173`, `internal/cli/migrate.go:43-46`).

### Existing Git-Based Change/Ancestor Patterns
- Worktree status determines a default base branch via `origin/HEAD` then fallback candidates (`main`, `master`, remote-prefixed forms) (`internal/cli/worktree.go:460-484`).
- Ancestry check is implemented using `git merge-base --is-ancestor`, with exit code `1` mapped to a non-error false result (`internal/cli/worktree.go:486-496`).
- Worktree list uses this to report merged-vs-base state (`internal/cli/worktree.go:122-162`).
- Tests validate both unmerged and merged cases for the merge-base helper (`internal/cli/worktree_test.go:299-340`) and fallback branch detection (`internal/cli/worktree_test.go:343-368`).

### Deterministic and Explainable Output Practices
- Command names and env file paths are explicitly sorted before output/consumption (`internal/cli/registry.go:200-205`, `internal/cli/worktree.go:622`).
- Metadata output has fixed field ordering and label strings, with tests asserting exact formatting (`internal/cli/metadata.go:79-93`, `internal/cli/metadata_test.go:77-98`).
- Worktree porcelain parsing is structured and validated; malformed input yields explicit parse errors (`internal/cli/worktree.go:373-404`, `internal/cli/worktree_test.go:146-153`).

### Command Registration and Composition Patterns
- Top-level commands self-register via `init()` and a shared map (`internal/cli/registry.go:20-24`, `internal/cli/dev.go:19-21`, `internal/cli/worktree.go:19-21`).
- Main runner resolves explicit top-level commands first, then falls back to generic service command execution (`internal/cli/run.go:26-40`, `internal/cli/registry.go:52-86`).
- This pattern provides a single dispatch path for adding command families and shared config usage.

### Safety/Validation Practices Used Across Modules
- Regex validation is used before shelling out with externally-derived values: DNS labels (`internal/cli/hosts.go:21`, `internal/cli/hosts.go:141-146`), Kubernetes names (`internal/cli/infra.go:94-103`), migration identifiers (`internal/cli/migrate.go:185-193`).
- Many command paths are shell-free (`exec.Command`) with explicit argument lists and allowlisted binaries (`internal/cli/registry.go:155-177`).

## Code References
- `docs/goals.md:35` - A2 project type-rule line.
- `docs/goals.md:45` - Start of impacted change detection section.
- `internal/cli/registry.go:43` - `Service` structure containing `Depends`.
- `internal/cli/registry.go:89` - `services.yaml` config loading.
- `internal/cli/registry.go:106` - shared service lookup (`findService`).
- `internal/cli/dev.go:115` - dependency-driven infra startup logic.
- `internal/cli/dev.go:151` - unknown dependency error behavior.
- `internal/cli/worktree.go:460` - default base branch resolution.
- `internal/cli/worktree.go:486` - merge-base ancestry helper.
- `internal/cli/worktree_test.go:299` - merge-base behavior test coverage.
- `internal/cli/metadata.go:79` - stable metadata output formatting.
- `internal/cli/metadata_test.go:77` - exact output ordering assertions.
- `internal/cli/hosts.go:109` - requested-name selection and dedupe flow.
- `internal/cli/infra.go:154` - resource selection by explicit names.

## Architecture Documentation
- The codebase is currently service-centric (`services.yaml`) rather than a typed project graph manifest.
- Dependency information exists as name references (`Depends`) and is used by command-specific workflows (not a central dependency graph engine).
- Git ancestry logic exists in worktree lifecycle code and is already backed by tests.
- Output determinism and parser strictness patterns are present in multiple modules and tests.
- Command architecture is centralized dispatch + command self-registration with shared helpers.

## Historical Context (from thoughts/)
- `thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md` records that impacted-change commands (`affected`, `status`) are not present in this snapshot and cites `docs/goals.md` section B.
- `thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md` documents the same service-driven runtime model and command registration structure.
- `thoughts/shared/plans/2026-02-24-mono-cmd-top-level-migration.md` captures the migration that established `cmd/mono` + `internal/cli` packaging used by the current command surface.

## Related Research
- `thoughts/shared/research/2026-02-24-goals-checklist-implementation-status.md`
- `thoughts/shared/research/2026-02-24-cli-and-build-tooling-current-state.md`

## GitHub Permalinks
- https://github.com/scottcrooks/mono/blob/fd13aa21bea328b3574814e0431585e4babbe3fb/internal/cli/registry.go#L43
- https://github.com/scottcrooks/mono/blob/fd13aa21bea328b3574814e0431585e4babbe3fb/internal/cli/dev.go#L115
- https://github.com/scottcrooks/mono/blob/fd13aa21bea328b3574814e0431585e4babbe3fb/internal/cli/worktree.go#L460
- https://github.com/scottcrooks/mono/blob/fd13aa21bea328b3574814e0431585e4babbe3fb/internal/cli/worktree_test.go#L299

## Open Questions
- `services.yaml` is not present in this worktree snapshot, so concrete in-repo examples of project/service entries and dependency chains are not inspectable here.
- `./bin/mono metadata` is unavailable in this tree at research time; metadata was collected from git/date plus source-level command behavior.

## Follow-up Research 2026-02-24T20:30:58-08:00
User update: `services.example.yml` was added from a real registry source to provide concrete examples.

### Observed File State
- `services.example.yml` exists in repository root but is currently empty (`0` bytes, `0` lines) at research time.
- Because the file has no YAML content yet, there are no service/project records, dependency edges, or path-prefix mappings to extract.

### Impact on Current Documentation
- Existing findings in this document remain based on live code in `internal/cli/*` and prior thoughts documents.
- Registry-driven concrete examples remain pending until `services.example.yml` (or `services.yaml`) contains populated entries.

## Follow-up Research 2026-02-24T20:31:22-08:00
User update: `services.example.yml` now contains saved registry content.

### Registry Evidence Extracted
- Four services are declared with explicit `name` + `path` pairs:
  - `polaris` at `packages/polaris` (`services.example.yml:2-3`)
  - `pythia` at `apps/pythia` (`services.example.yml:12-13`)
  - `mallos` at `apps/mallos` (`services.example.yml:26-27`)
  - `daedalus` at `apps/daedalus` (`services.example.yml:39-40`)
- The only declared `depends` edge in the file is `pythia -> postgres` (`services.example.yml:15`).
- Local infrastructure declares `postgres` as a named resource under `local.resources` (`services.example.yml:52-56`), matching the existing `dev.ensureInfraDeps` model that resolves service dependencies to infra resource names (`internal/cli/dev.go:143-152`).

### Concrete Command Vocabulary in Real Registry Data
- Go-oriented service commands are shown for `polaris` and `pythia` (`go test`, `go build`, `go tool golangci-lint`, `go tool govulncheck`) (`services.example.yml:6-10`, `services.example.yml:17-24`).
- Node/React services (`mallos`, `daedalus`) use `pnpm` and `npx playwright` command entries (`services.example.yml:30-37`, `services.example.yml:43-50`).
- Command keys used across entries include: `reqs`, `test`, `test-integration`, `lint`, `build`, `audit`, `run`, `dev`.

### Change-Detection-Relevant Shape (As-Is)
- Path prefixes are concrete and project-scoped (`apps/*`, `packages/*`), which aligns with section B’s changed-file to owning-project mapping concept (`docs/goals.md:49`).
- Dependency declaration in this sample currently links a service to an infra resource (`postgres`) rather than service-to-service or library-to-service edges.
- No explicit project-type field exists in this sample (`service` vs `library`), so the A2 type rule at `docs/goals.md:35` is still policy-level context rather than an encoded field in the YAML entries.
