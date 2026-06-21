# Repository Guidelines

freedius is a local HTTP proxy built with Go's standard library (`net/http`, `httputil.ReverseProxy`). Compiles to a single static binary; zero external runtime dependencies.

## Build, Test, and Development Commands

- **Run**: `go run ./cmd/freedius` — starts the proxy server locally.
- **Build**: `go build -o freedius ./cmd/freedius` — produces a static binary.
- **Test**: `go test ./...` — runs all tests.
- **Lint**: `go vet ./...` — runs the Go static analysis checks.
- **Audit**: `govulncheck ./...` — checks for known vulnerabilities in the module graph.

## Project Structure

- `cmd/freedius/` — entry point (single binary), HTTP server setup, proxy routing (@go.dev/doc/net/http for `http.Handler` patterns).
- `proxy/` — reverse proxy logic using `httputil.ReverseProxy`.
- `config/` — configuration loading (env, flags, or file-based).
- `context/foundation/` — product requirements, tech-stack decisions, and plans (do not edit manually unless you are sure).
- `context/changes/` — change-by-change implementation plans and verification logs.

## Coding Style

- **Format**: `gofumpt` (stricter than `gofmt`) enforced in CI.
- **Naming**: Go conventions — `camelCase` for unexported, `PascalCase` for exported, `ALL_CAPS` for env-var constants.
- **Error handling**: Return errors; use `fmt.Errorf("context: %w", err)` for wrapping. Panic only at package init or `main()` entry.
- **No external HTTP router**: Use the standard library's `http.ServeMux` (Go 1.22+ pattern matching, e.g. `GET /api/proxy/{target}`).
- **Configuration**: Env vars with `os.Getenv` defaults; no config library.

## Testing Guidelines

- Tests live in `*_test.go` next to the file they test.
- Use `testing` stdlib + `httptest` for HTTP tests (`httptest.NewServer`, `httptest.NewRequest`, `httptest.ResponseRecorder`).
- Table-driven tests preferred for handler logic.
- Run `go test -cover ./...` before committing to check coverage.

## CI & Commits

- CI runs on GitHub Actions (`@.github/workflows/ci.yml`): `go vet`, `go test`, `go build`.
- Commits use conventional-commit prefixes observed in `git log`: `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`.
- Keep the module path `github.com/pfrack/freedius` in imports.
