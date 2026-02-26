# G1 Doctor Policy/Manifest Validator Implementation Plan

## Overview

Implement G1 by turning `mono doctor` into a deterministic policy/manifest validator for the current `services.yaml` model, while preserving existing developer-environment checks. The result should provide strict validation diagnostics (including manifest key paths and locations), plus stable exit-code behavior suitable for CI.

## Current State Analysis

`mono doctor` currently checks local tooling and bootstrap setup, installs dependencies, and optionally lists services; it does not validate manifest schema, required project fields, dependency graph integrity, or deploy contract completeness.

The current config loader is permissive (`yaml.Unmarshal`) and has no schema-level unknown-key or required-field enforcement. Some dependency-cycle logic exists in task graph execution, but it is tied to specific task resolution and is not surfaced as manifest validation.

## Desired End State

`mono doctor` validates the manifest contract for G1 and fails with deterministic diagnostics when invalid:
- Manifest schema sanity for `services.yaml` with unknown-key detection
- Required project fields present (including owner/type/runtime contract for G1)
- Service paths exist
- Dependency graph sanity (unknown deps + cycle detection)
- Deploy contract completeness for service projects

Validation output must be deterministic and CI-consumable:
- Stable exit code semantics for validation failures vs runtime errors
- Structured diagnostics with manifest path + line/column and human-readable summary

### Key Discoveries:
- Current config model loads `services.yaml` via permissive unmarshal and returns no validation diagnostics: `internal/cli/core/config.go:55`
- Current `doctor` command is environment/bootstrap focused and skips manifest validation beyond `listServices()`: `internal/cli/commands/workflow/doctor.go:18`, `internal/cli/commands/workflow/doctor.go:145`
- Existing cycle detection logic can be adapted for graph sanity rules: `internal/cli/tasks/task_graph.go:54`
- Gap analysis explicitly prioritizes G1 first and requires deterministic validator outputs with parseable failure behavior: `/home/scott/src/mono/docs/gap_analysis.md:9`

## What We're NOT Doing

- Migrating from `services.yaml` to `mono.yaml` (A-phase work)
- Implementing deploy policy checks from G2 (`latest` tag/probes/resources/owner label/ingress validity policy)
- Implementing `mono plan --format json` (E-phase work)
- Replacing existing `doctor` environment setup flows in this phase

## Implementation Approach

Add a dedicated validation layer that can read `services.yaml` with location-aware YAML AST parsing, emit structured diagnostics, and be called from `doctor`. Keep current environment checks intact, then run policy validation in the same command. Introduce typed CLI errors so `Run()` can map deterministic exit codes.

## Phase 1: Validation Engine Scaffolding

### Overview
Create a reusable validation package and diagnostic model that supports deterministic reporting and exit behavior.

### Changes Required:

#### 1. Validation package and diagnostics types
**File**: `internal/cli/validation/validator.go` (new)
**Changes**: Add validator entrypoint (e.g., `ValidateServicesManifest(path string) Report`) returning stable, sorted diagnostics.

#### 2. Diagnostic schema and exit-code mapping
**File**: `internal/cli/core/errors.go` (new)
**Changes**: Add typed error for CLI exit codes (for example: validation failure vs command/runtime failure) and update command path usage.

#### 3. CLI runner exit-code integration
**File**: `internal/cli/run.go`
**Changes**: Detect typed errors and return deterministic non-zero code for validation failures.

```go
// sketch
if err := cmd.Run(args); err != nil {
    if codeErr, ok := core.AsExitCodeError(err); ok {
        fmt.Fprintln(os.Stderr, codeErr.Error())
        return codeErr.ExitCode()
    }
    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
    return 1
}
```

### Success Criteria:

#### Automated Verification:
- [ ] `make test` passes with new core/runner tests
- [ ] `go test ./internal/cli/...` passes
- [ ] `make lint` passes
- [ ] `make build` passes

#### Manual Verification:
- [ ] Running `mono doctor` on a valid repo returns success and expected summary
- [ ] Running `mono doctor` on invalid manifest returns deterministic non-zero validation code
- [ ] Validation errors are readable and clearly tied to manifest keys

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before proceeding.

---

## Phase 2: Manifest Schema + Required Field Validation

### Overview
Implement strict-ish schema validation for current `services.yaml` contract and required fields needed for G1.

### Changes Required:

#### 1. Location-aware YAML parse
**File**: `internal/cli/validation/yaml_ast.go` (new)
**Changes**: Parse `services.yaml` using `yaml.Node` so diagnostics can include path + line/column.

#### 2. Schema and required-field checks
**File**: `internal/cli/validation/schema_rules.go` (new)
**Changes**:
- Validate top-level keys allowed for current manifest
- Validate service object allowed keys
- Enforce required fields (`name`, `path`, `type/kind`, `runtime/archetype`, `owner`, deploy contract presence for services)
- Preserve deterministic rule ordering

#### 3. Config model compatibility updates
**File**: `internal/cli/core/config.go`
**Changes**: Add fields required for G1 checks where needed (owner/runtime/deploy contract placeholders) without breaking existing command execution behavior.

### Success Criteria:

#### Automated Verification:
- [ ] New validation unit tests pass: `go test ./internal/cli/validation/...`
- [ ] Existing config consumers still pass: `go test ./internal/cli/...`
- [ ] `make test` passes
- [ ] `make lint` passes

#### Manual Verification:
- [ ] Unknown keys in `services.yaml` produce path-based diagnostics
- [ ] Missing required service fields are reported with key path and source location
- [ ] Valid manifests produce no schema errors

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before proceeding.

---

## Phase 3: Path + Graph + Deploy Contract Completeness Checks

### Overview
Complete G1-specific semantic validation (paths, dependency graph sanity, deploy contract completeness).

### Changes Required:

#### 1. Path existence checks
**File**: `internal/cli/validation/path_rules.go` (new)
**Changes**: Validate each service path exists relative to repo root and points to expected project directory.

#### 2. Dependency graph sanity checks
**File**: `internal/cli/validation/graph_rules.go` (new)
**Changes**:
- Unknown dependency detection
- Cycle detection with deterministic chain output (reuse/adapt approach from task cycle logic)

#### 3. Deploy contract completeness checks
**File**: `internal/cli/validation/deploy_rules.go` (new)
**Changes**: Validate required deploy contract fields for service projects (minimum completeness only for G1).

### Success Criteria:

#### Automated Verification:
- [ ] Graph validation tests pass (unknown dep + cycle fixtures)
- [ ] Path/deploy rules tests pass
- [ ] `go test ./internal/cli/...` passes
- [ ] `make test` passes

#### Manual Verification:
- [ ] Invalid service path is reported with service/key context
- [ ] Dependency cycles produce clear deterministic chain text
- [ ] Missing deploy-contract values on services are reported as validation failures

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before proceeding.

---

## Phase 4: Doctor Command Integration + UX/Test Hardening

### Overview
Wire the validator into `mono doctor`, standardize output, and add command-level tests.

### Changes Required:

#### 1. Doctor integration and output modes
**File**: `internal/cli/commands/workflow/doctor.go`
**Changes**:
- Invoke validator during doctor flow
- Print deterministic summary (`N errors`, `N warnings`)
- Return typed validation failure on errors
- Keep current environment checks; validation runs as an explicit section

#### 2. Command-level tests
**File**: `internal/cli/commands/workflow/doctor_test.go` (new)
**Changes**:
- Add fixtures for valid/invalid manifests
- Assert exit-code behavior via CLI runner
- Assert deterministic diagnostic snippets and ordering

#### 3. CLI regression tests
**File**: `internal/cli/run_test.go`
**Changes**: Add tests ensuring validation failures map to intended exit code and non-validation failures remain unchanged.

#### 4. Usage/help updates
**File**: `internal/cli/core/usage.go`
**Changes**: Update doctor description to include policy/manifest validation behavior.

### Success Criteria:

#### Automated Verification:
- [ ] `go test ./internal/cli/commands/workflow/...` passes
- [ ] `go test ./internal/cli/...` passes
- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] `make build` passes

#### Manual Verification:
- [ ] `mono doctor` on a healthy manifest reports validation success
- [ ] `mono doctor` on malformed manifest reports deterministic diagnostics and non-zero validation exit code
- [ ] Existing environment checks still run and report as before
- [ ] Output is understandable in both local interactive use and CI logs

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before marking complete.

---

## Testing Strategy

### Unit Tests:
- Validator diagnostics ordering (stable across runs)
- YAML path extraction (`services[n].field`) and line/column mapping
- Required-field enforcement per service kind
- Unknown-key schema checks
- Graph checks (unknown dependency + cycle)
- Deploy contract completeness rule matrix

### Integration Tests:
- CLI-level `Run()` exit code mapping for validation failures
- `doctor` output and failure behavior with fixture manifests
- Regression coverage ensuring existing commands remain unaffected

### Manual Testing Steps:
1. Run `mono doctor` with valid `services.yaml`; confirm all checks pass.
2. Remove a required service field (for example `owner`) and run `mono doctor`; confirm deterministic failure and clear location/path.
3. Introduce dependency cycle and rerun; confirm cycle chain output.
4. Point one service to missing path; confirm path diagnostic.

## Performance Considerations

Manifest validation should remain linear in service count plus dependency edges (`O(S + E)`), with deterministic sorting of diagnostics as a small additional overhead.

## Migration Notes

No file-format migration in this plan. G1 validator targets existing `services.yaml` and creates enforcement scaffolding for later A-phase migration to `mono.yaml`.

## References

- Goals checklist G1 definition: `docs/goals.md:198`
- Gap analysis G1 priority and required outputs: `/home/scott/src/mono/docs/gap_analysis.md:9`
- Current config loader: `internal/cli/core/config.go:55`
- Current doctor implementation: `internal/cli/commands/workflow/doctor.go:18`
- Existing graph cycle logic reference: `internal/cli/tasks/task_graph.go:54`
- Existing CLI exit-code behavior: `internal/cli/run.go:32`
