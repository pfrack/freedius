---
date: 2026-07-01T20:00:00+02:00
researcher: opencode
git_commit: ae2906d
branch: daemon-mode
repository: freedius
topic: "Integrate Mage into GitHub Actions CI"
tags: [mage, ci, github-actions, build-tooling]
status: complete
last_updated: 2026-07-01
last_updated_by: opencode
---

# Research: Integrate Mage into GitHub Actions CI

**Date**: 2026-07-01T20:00:00+02:00  
**Researcher**: opencode  
**Git Commit**: ae2906d  
**Branch**: daemon-mode  
**Repository**: freedius

## Research Question

How to use Mage in GitHub Actions CI instead of raw `go` commands.

## Summary

Mage is **already fully configured** in this repository. The `magefiles/mage.go` defines a `CI` target that runs `Vet → GenerateCheck → Test → Build` — exactly what `.github/workflows/ci.yml` does with raw commands. The only gap is that GitHub Actions doesn't invoke Mage. Two approaches exist:

1. **Install `mage` binary in CI** — `mage ci` runs the full pipeline
2. **Use zero-install bootstrap** — `go run mage.go ci` (no install needed)

The zero-install approach is recommended since it avoids a setup step and uses the pinned version from `magefiles/go.mod`.

## Detailed Findings

### Current CI Workflow

`.github/workflows/ci.yml` runs these steps sequentially:

```yaml
- run: go vet ./...
- run: go test -race -coverprofile=coverage.out ./...
- run: go build ./...
- run: go generate ./... && git diff --exit-code
- run: govulncheck ./...
```

This duplicates the `CI` Mage target at `magefiles/mage.go:84-87`:

```go
func CI() error {
    mg.SerialDeps(Vet, GenerateCheck, Test, Build)
    return nil
}
```

### Mage Targets Available

| Target | Equivalent CI step |
|---|---|
| `Vet` | `go vet ./...` |
| `GenerateCheck` | `go generate ./... && git diff --exit-code` |
| `Test` | `go test -race -cover ./...` |
| `Build` | `go build -o freedius ./cmd/freedius` |
| `CI` | All four above in sequence |
| `Lint` | Vet + staticcheck + golangci-lint |

### Installation Options for GitHub Actions

**Option A: Install mage binary**
```yaml
- name: Install Mage
  run: go install github.com/magefile/mage@v1.17.2
- name: Run CI
  run: mage ci
```

**Option B: Zero-install bootstrap (recommended)**
```yaml
- name: Run CI
  run: go run mage.go ci
```

The zero-install approach uses `mage.go` (root, `//go:build ignore`) which imports `github.com/magefile/mage/mage` and calls `mage.Main()`. It reads `magefiles/mage.go` and runs targets. No global install needed.

### Key Considerations

1. **Coverage output**: The current CI uses `-coverprofile=coverage.out`. The Mage `Test` target uses `-cover` (terminal output). If coverage artifact upload is needed, the Mage target or a separate step is required.

2. **govulncheck**: Not in any Mage target. Can be added as a separate step after `mage ci`, or added to the Magefile.

3. **Module caching**: `actions/setup-go@v5` with `cache: true` caches the Go module cache. The `magefiles/` module is separate — it will be downloaded on first run but cached thereafter.

4. **Pin consistency**: `go.mod` pins `mage v1.17.2`. The zero-install approach uses the same version via `magefiles/go.mod`.

## Code References

- `.github/workflows/ci.yml` — current CI workflow (35 lines)
- `magefiles/mage.go:84-87` — `CI()` target definition
- `magefiles/mage.go:16-18` — `Test()` target
- `magefiles/mage.go:21-23` — `Vet()` target
- `magefiles/mage.go:26-28` — `Build()` target
- `magefiles/mage.go:31-36` — `GenerateCheck()` target
- `mage.go` — zero-install bootstrap (root)
- `tools.go` — tooling anchor for `go.mod` pinning

## Recommended Approach

Replace the CI workflow with:

```yaml
name: ci

on:
  push:
    branches: ["**"]
  pull_request:
    branches: ["**"]

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.4'
          cache: true
      - name: CI
        run: go run mage.go ci
      - name: govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...
```

This:
- Uses `go run mage.go ci` (zero-install, no setup step)
- Keeps `govulncheck` as a separate step (not in Magefile)
- Fixes Go version to match `go.mod` (`1.26.4` not `1.26.1`)

## Open Questions

1. Should `govulncheck` be added as a Mage target?
2. Should coverage be uploaded as a CI artifact (requires adjusting `Test` target)?
3. Should `Lint` (staticcheck + golangci-lint) run in CI too?
