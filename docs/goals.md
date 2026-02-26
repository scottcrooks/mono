# Mono MVP Feature Checklist (≤20 Services, Repo-Level Manifest, Helm-Only Deploy)

Use this as a validation grid against your PoC.

---

# Priority Order (Current)

- [ ] Phase 1 priority: **B. Impacted Change Detection**
- [ ] Phase 2 priority: **C. Task Orchestration Engine**
- [ ] Phase 3 priority: **G. Policy & Validation (“No Sprawl”)**
- [ ] After B/C/G, reassess A/D/E/F/H/I based on implementation progress and remaining gaps
- [ ] Note: A is currently treated as partially covered by existing shorthand command execution via `services.yaml`

---

# A. Workspace & Project Model

## A1. Repo-Level Manifest
- [ ] `mono.yaml` is the single source of truth for:
  - [ ] project inventory (services + libs)
  - [ ] project paths
  - [ ] project types (`service`, `library`)
  - [ ] owners
  - [ ] explicit deps (project → project)
  - [ ] runtime/tooling selection (minimal: node/typescript)
  - [ ] deploy contract (services only)
- [ ] Strict schema validation (unknown keys fail)
- [ ] Deterministic parsing + stable ordering (same input → same plan output)

## A2. Graph Rules / Enforcement
- [ ] All deps must reference a known project
- [ ] No cycles (cycle detection with clear error)
- [ ] Type rules enforced (MVP):
  - [ ] libraries cannot depend on services
  - [ ] services can depend on libraries
  - [ ] (optional) services cannot depend on other services unless explicitly allowed

## A3. Discovery
- [ ] No filesystem scanning required (manifest-driven)
- [ ] Errors point to exact manifest path/key (good diagnostics)

---

# B. Impacted Change Detection

## B1. Git-Based Change Set
- [ ] Uses merge-base vs HEAD (or configurable base ref)
- [ ] Maps changed files → owning project via `path` prefixes
- [ ] Computes impacted closure:
  - [ ] “changed projects”
  - [ ] “impacted projects” (changed + dependents)

## B2. Explainability
- [ ] `mono affected` returns impacted project list
- [ ] `mono affected --explain` prints dependency chains  
  - e.g. `libs/shared-config -> services/billing-api`

## B3. Status Summary
- [ ] `mono status` prints:
  - [ ] changed projects
  - [ ] impacted projects
  - [ ] planned tasks summary (what would run for `mono check`)

---

# C. Task Orchestration Engine

## C1. Task Model
- [ ] Unit of execution is **(project, task)** not file/action
- [ ] Task vocabulary (MVP):
  - [ ] `build`
  - [ ] `lint`
  - [ ] `typecheck`
  - [ ] `test`
  - [ ] `package` (services)
  - [ ] `deploy` (services)

## C2. Default Task Resolution
- [ ] Per-runtime templates map tasks → commands (MVP: Node/TS)
- [ ] Overrides allowed only in controlled ways (manifest fields), not arbitrary scripts everywhere

## C3. DAG Execution
- [ ] Executes tasks respecting dependency order
  - e.g. if service depends on lib, `lib:build` runs before `service:build`
- [ ] Parallel execution of independent tasks (configurable concurrency)

## C4. Local Caching (Coarse)
- [ ] Cache key includes:
  - [ ] task name
  - [ ] project file content hash
  - [ ] relevant lockfiles
  - [ ] selected env vars (whitelist)
- [ ] Cache hit skips task execution
- [ ] Cache miss runs task and stores result
- [ ] `--no-cache` flag exists
- [ ] Basic cache diagnostics (“why miss?”)

## C5. Output UX
- [ ] Clear segmented logs per project/task
- [ ] Summary at end: succeeded / failed / skipped (cached)
- [ ] Non-zero exit code on failure

---

# D. Developer Experience Workflows

## D1. Local PR Gate
- [x] `mono check` runs impacted:
  - [x] lint
  - [x] typecheck
  - [x] unit tests

## D2. Targeted Execution
- [ ] `mono test <project>` runs for that project
- [ ] `mono test --all` runs everything

## D3. Dev Command
- [ ] `mono dev <service>`:
  - [ ] runs service in watch mode
  - [ ] rebuilds/rechecks on change
  - [ ] clean shutdown handling
- [ ] (optional) dependency bring-up deferred from MVP

## D4. Worktree Management
- [ ] `mono worktree create <branch> [--from <ref>] [--id <unique-id>] [--no-bootstrap]`
  - [ ] creates isolated git worktree for unrelated parallel work
  - [ ] supports optional bootstrap/setup after create
- [ ] `mono worktree list` shows:
  - [ ] worktree path
  - [ ] branch or detached state
  - [ ] dirty/clean status
  - [ ] merged status vs default base branch
- [ ] `mono worktree path <branch-or-id>` resolves worktree location
- [ ] `mono worktree remove <branch-or-id> [--force]` supports safe cleanup
- [ ] `mono worktree prune` cleans stale worktree metadata

---

# E. CI Integration

## E1. Plan Generation
- [ ] `mono plan --format json` emits:
  - [ ] list of `(project, task)` nodes
  - [ ] dependency relationships or topological order
  - [ ] deterministic/stable output

## E2. Local == CI Parity
- [ ] Same task resolution locally and in CI
- [ ] Plan parameterizable by:
  - [ ] base ref / commit range
  - [ ] environment (for deploy stages)

---

# F. Kubernetes Deployment (Helm-Only Contract)

## F1. Owned Helm Chart
- [ ] Mono uses first-party chart (`charts/mono-service`)
- [ ] Services cannot supply raw templates
- [ ] Services can only provide allowed values

## F2. Deploy Contract Fields (Minimum)
Per service:
- [ ] container port
- [ ] probes (path + port)
- [ ] resources requests/limits
- [ ] env vars (non-secret references only)
- [ ] ingress on/off + host (or suffix model)

Repo-level:
- [ ] registry/repo naming convention
- [ ] image tag = git SHA
- [ ] base labels/annotations (owner, service, repo)

## F3. Environment Overlays
- [ ] `mono.env.yaml` defines `staging` + `prod`:
  - [ ] namespace
  - [ ] default replicas
  - [ ] shared Helm values
- [ ] Deterministic merge order:
  1. chart defaults
  2. mono defaults
  3. env values
  4. service overrides

## F4. Deployment Commands
- [ ] `mono deploy <service> --env <env>`:
  - [ ] image build + push
  - [ ] `helm upgrade --install`
- [ ] `mono deploy:render`
- [ ] `mono deploy:diff`

---

# G. Policy & Validation (“No Sprawl”)

## G1. `mono doctor`
Validates:
- [ ] manifest schema
- [ ] project paths exist
- [ ] dependency graph sanity
- [ ] required fields present (owner/type/runtime)
- [ ] deploy contract completeness

## G2. Deployment Policy Checks
- [ ] no `latest` image tag
- [ ] required probes present
- [ ] resources required
- [ ] owner label required
- [ ] ingress host validity

---

# H. Minimal Supported Stack (MVP Scope Control)

- [ ] Node + TypeScript first-class
- [ ] Docker-based packaging
- [ ] Kubernetes via Helm only
- [ ] No plugin ecosystem

---

# I. Explicit Non-Goals (MVP)

- [ ] Multiple Helm charts
- [ ] Raw Helm value injection
- [ ] GitOps controller integration
- [ ] Preview environments
- [ ] Remote cache
- [ ] Remote execution
- [ ] Polyglot plugin system
- [ ] Cluster provisioning
