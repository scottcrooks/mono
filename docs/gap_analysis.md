# Gap Analysis: What To Implement Next

## Summary
Prioritize work that unlocks the most checklist closure with the least rework.
Given current state, G1 is now substantially implemented in `mono doctor`; the highest-leverage order is now: finish residual G1 verification ergonomics, then A formal manifest model, then E plan output, then F deploy contract, then polish D/H/I.

## Current Status Update (2026-02-26)
- `G1` status: mostly implemented in current `services.yaml` model.
- Landed in `mono doctor`:
  - Manifest/schema validation with unknown-key detection (top-level + nested sections)
  - Required field checks (`owner`, `kind/type`, `archetype/runtime`, etc.)
  - Path checks (existence, directory, repo-relative constraints)
  - Dependency sanity checks (unknown deps, deterministic cycle detection)
  - Deploy contract completeness checks for services
  - Deterministic diagnostics with severity and stable ordering
  - Deterministic validation exit code semantics (validation failures distinguished from generic runtime errors)
- Not yet fully closed from original G1 plan in this environment:
  - Full `make lint`/`make build` verification is blocked by sandbox network/module-download restrictions.
  - Full end-to-end fixture coverage for complete `mono doctor` runtime flow can still be expanded.

## Ordered Backlog

1. A. Formalize project model in `mono.yaml` (schema + graph semantics)
- Why first now: G1 validator scaffolding exists; formal schema/model is the next foundational contract for roadmap completion.
- Scope: rename + schema hardening + typed project inventory (`service`/`library`), owners, runtime/tooling, explicit deps, deploy contract placeholders.
- Depends on: existing validator behavior from current G1 implementation.
- Outputs: strict schema validation (unknown keys fail), stable parse ordering, graph rule enforcement.
- Checklist impact: `A1/A2/A3`, enables `E/F/G2`.

2. E1/E2. Add `mono plan --format json` for CI parity
- Why second: turns existing B/C behavior into deterministic CI interface.
- Scope: emit task nodes, dependency edges/topological order, base-ref parameterization, deterministic output.
- Depends on: stable manifest model (step 1), existing impact/task engines.
- Outputs: versioned JSON contract for CI and local parity checks.
- Checklist impact: `E1`, most of `E2`.

3. F. Introduce Helm-only deploy contract and commands
- Why third: highest complexity and operational coupling; best done once manifest and plan contracts are stable.
- Scope: first-party chart contract, allowed service values, env overlays, deterministic merge order, `deploy/render/diff`.
- Depends on: steps 1-2.
- Outputs: deploy schema + command surface + render/diff determinism.
- Checklist impact: `F1/F2/F3/F4`, supports `G2`.

4. G2. Add deploy policy checks
- Why fourth: enforce contract once deploy model exists.
- Scope: prohibit `latest`, require probes/resources/owner label/ingress validity.
- Depends on: step 3.
- Outputs: policy validation integrated into `doctor` and deploy preflight.
- Checklist impact: `G2`.

5. D gap closure pass (targeted)
- Why fifth: mostly done already; close residual items after core model stabilizes.
- Scope: explicit `mono test --all`, confirm/clarify `mono test <project>` semantics, decide whether mono-managed watch/recheck is required vs delegated.
- Depends on: none critical, but safer after step 1.
- Outputs: explicit UX semantics and tests.
- Checklist impact: remaining `D2/D3`.

6. H/I documentation + guardrails pass
- Why sixth: finalize scope boundaries once architecture is stable.
- Scope: codify minimal supported stack and explicit non-goal guardrails in docs/validation where needed.
- Depends on: steps 1-4.
- Outputs: enforceable scope statements and checks.
- Checklist impact: `H`, `I`.

## Recommended "Implement Next" Start Packet
1. Continue from current G1 implementation by closing remaining verification/doc/test hardening gaps.
2. Immediately follow with Step 1 (A model hardening), including `services.yaml -> mono.yaml` as part of schema migration.
3. Treat Step 2 (plan JSON) as the next external interface milestone for CI consumers.

## Acceptance-Oriented Milestones
1. Milestone A: `doctor` fails accurately on malformed/invalid manifest and graph errors with deterministic summaries and exit codes.
2. Milestone B: `mono.yaml` is authoritative with strict schema + graph enforcement.
3. Milestone C: `mono plan --format json` is deterministic and CI-consumable.
4. Milestone D: Helm deploy contract and deploy commands are functional and policy-gated.

## Assumptions
- Current B/C implementation is retained as core behavior, not replaced.
- Naming migration (`services.yaml` to `mono.yaml`) is necessary but not sufficient for section A completion.
- CI and deploy consumers can wait until schema and validator contracts are stable.
