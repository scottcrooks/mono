# Release Checklist

## Versioning

- Follow semantic versioning (`vMAJOR.MINOR.PATCH`).
- Tag releases from `main` only after `make check` passes.

## Pre-Release

- Confirm local checks: `make check`
- Confirm installability from module path:
  - `go install github.com/scottcrooks/mono/cmd/mono@latest`
- Verify CLI behavior:
  - `mono --help`
  - `mono --version`
  - `mono metadata`

## Release

- Create and push tag: `git tag vX.Y.Z && git push origin vX.Y.Z`
- Create GitHub release notes summarizing changes and breaking changes (if any).

## Post-Release

- Verify install on a clean environment.
- Verify latest tag resolves via `go install ...@latest`.
