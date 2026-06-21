---
date: 2026-06-21T12:00:00+02:00
researcher: pawel
git_commit: d361805f91845be2e59a9b253f46e5b1b37c634d
branch: main
repository: freedius
topic: "Go package layout — internal/ vs pkg/, Zalando, and other guidelines"
tags: [research, codebase, go, package-layout, internal, pkg, zalando, style-guide, cmd]
status: complete
last_updated: 2026-06-21
last_updated_by: pawel
last_updated_note: "Added follow-up research for cmd/freedius/ reorganization — supersedes the original no-change recommendation."
---

# Research: Go package layout — `internal/` vs `pkg/`, Zalando, and other guidelines

**Date**: 2026-06-21T12:00:00+02:00
**Researcher**: pawel
**Git Commit**: `d361805f91845be2e59a9b253f46e5b1b37c634d`
**Branch**: `main`
**Repository**: `pfrack/freedius`

## Research Question

> golang packages introduce `internal` and `pkg`. Research Go rules and guidelines from Zalando etc.

Translated: what are the rules and conventions for Go package layout — particularly the `internal/` directory (compiler-enforced private packages) and the `pkg/` directory (community-convention public library surface) — and what do Zalando and other major Go style guides say about them? Then: should `freedius` change its current layout in light of those conventions?

## Summary

1. **`internal/` is a Go-toolchain-enforced mechanism, not a convention.** A package whose import path contains a segment named `internal` may only be imported by code rooted at the parent of that `internal` directory. Introduced in Go 1.4. The rule is enforced by `cmd/go`; violations fail to compile. ([Go 1.4 release notes](https://go.dev/doc/go1.4); [pkg.go.dev/cmd/go#hdr-Internal_Directories](https://pkg.go.dev/cmd/go#hdr-Internal_Directories))

2. **`pkg/` is a community convention that predates `internal/` and survives mainly in legacy monorepos.** It originated in Brad Fitzpatrick's Camlistore (2010-ish) and spread to Kubernetes, Moby/Docker, etcd, Terraform, and CockroachDB. The Go standard library used `pkg/` (`$GOROOT/src/pkg/`) until Go 1.4, when the Go team removed it. Today the official Go docs, the standard library, Dave Cheney, and most modern Go services do not use or recommend `pkg/`. The single repo that does — `golang-standards/project-layout` — explicitly states in its own README: *"This is a common layout pattern, but it's not universally accepted and some in the Go community don't recommend it."* The consensus is: **use `pkg/` only when the repo genuinely intends to expose a public Go API that other modules will import; in application repos, prefer `cmd/` + `internal/`.**

3. **Zalando does not publish a Go language style guide.** The widely-cited "Zalando guidelines" repo (`zalando/restful-api-guidelines`) is a REST API / HTTP / JSON guide, not a Go guide. Zalando's only Go-language guidance is one line in `zalando/skipper/CONTRIBUTING.md` deferring to `gofmt` + the `golang/go/wiki/CodeReviewComments` page (which is Google's). Zalando's flagship Go repo (`zalando/skipper`) uses `cmd/` at the root and **no `internal/`, no `pkg/`** — a flat-by-feature layout.

4. **The authoritative layout reference is the Go team's own doc at `go.dev/doc/modules/layout`.** It endorses `cmd/` and `internal/`, and never mentions `pkg/`. For server projects it explicitly says: *"It's recommended to keep packages in `internal` as much as possible."*

5. **`freedius`'s current layout is already aligned with the Go team's recommendation in spirit, except for `cmd/`.** No `pkg/`, no `cmd/` (single binary at root, deliberate — see `context/archive/error-hardening/research.md:261` for the explicit rejection of a *multi-binary* layout). `internal/` is used for exactly the two things the Go team suggests: private cross-cutting code (`internal/envinject`, [snippet.go](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/envinject/snippet.go)) and a private codegen tool (`internal/genproviders`). The top-level public packages (`config/`, `proxy/`) are public because the codegen pipeline writes into them — moving them under `internal/` would break the generator. **The `cmd/` half of the convention is missing**, however — see the follow-up section at the end of this document for the revised recommendation to introduce `cmd/freedius/` (single-binary reorganization, no new binaries).

## Detailed Findings

### 1. freedius current package layout (verified against `d361805`)

Top-level packages (from [main.go](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main.go) and `go list ./...`):

| Path | Package | Public/internal | Purpose |
|---|---|---|---|
| `./` | `main` | public (binary) | Entry point: HTTP server, Bubble Tea TUI, flag parsing, env-var gating |
| [`config/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/config) | `config` | public | YAML config load + validate + thread-safe access (`Config`, `Provider`, `Mapping`) |
| [`proxy/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy) | `proxy` | public | Reverse-proxy core: `Dispatcher`, `Registry`, `Provider` interface, middleware |
| [`proxy/translate/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/translate) | `translate` | public (nested under public `proxy/`) | Anthropic ↔ OpenAI wire-format conversion + BPE token counter |
| [`proxy/tui/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/tui) | `tui` | public (nested under public `proxy/`) | Bubble Tea v2 dashboard (3 tabbed views) |
| [`internal/envinject/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/envinject) | `envinject` | **internal** | `~/.claude/settings.json` merge + shell-rc marker + eval snippet |
| [`internal/genproviders/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/genproviders) | `main` | **internal** (unusual — a `main` package under `internal/`) | Codegen tool: `go run ./internal/genproviders` writes `config/providers_gen.go` and `proxy/adapters_gen.go` from `providers.yaml` |

**No `pkg/`, no `cmd/`.** Confirmed by `git log --all -- 'pkg/' 'cmd/'` → empty: there has never been a `pkg/` or `cmd/` directory in this repo's history.

**Import graph (no cycles):**
```
main → config, proxy, proxy/tui, internal/envinject
proxy → config, proxy/translate
proxy/tui → config, proxy, internal/envinject
proxy/translate → (stdlib only)
internal/envinject → (stdlib only)
internal/genproviders → (stdio only; emits import strings for config and proxy/translate into generated files)
config → (stdlib only)
```

The `internal/genproviders` case is unusual: a `package main` under `internal/`. Go permits this (the `internal` rule applies to import paths, not to package kinds), and it's used here to keep the codegen binary out of `go build .` — only `go run ./internal/genproviders` is invoked from `go generate ./...`. The codegen's output imports `config/` and `proxy/translate/` (lines 341–342 of [`internal/genproviders/main.go`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/genproviders/main.go#L341-L342)), which is why those top-level packages must remain public.

**Documented rationale in this repo:**

- `internal/envinject/` exists because of "cross-cutting, operator-only side effect" — `context/archive/error-hardening/research.md:311`: *"env-injection's `WriteSettingsJSON` should live in a new `internal/envinject/` package so `init` can import it. The only cross-cutting concern; no other module touches `~/.claude/`."*
- `internal/genproviders/` follows the `envinject` precedent — `context/archive/provider-codegen/research.md:192`: *"`internal/` — Contains `envinject/` package — clean pattern to mimic"*.
- Multi-binary layout was **explicitly rejected** — `context/archive/error-hardening/research.md:261`: *"D: Separate binaries via `//go:build ignore` — `cmd/freedius-init/main.go` — Cleanest mental model — Forces `internal/` move; Go tooling doesn't ship multi-binary repos well; one binary name already in user scripts — 100+ restructure — ❌ Reject."*

`AGENTS.md` documents only four entries (`main.go`, `proxy/`, `config/`, `context/**`) and is silent on `internal/`, `proxy/translate/`, `proxy/tui/`, `magefiles/`, `templates/`, and the codegen pipeline. The on-disk layout has outgrown the contributor-facing map.

### 2. Go's `internal` mechanism — compiler-enforced, not convention

**Authoritative source (Go 1.4 release notes, where the feature was introduced):**

> "as of Go 1.4 the `go` command introduces a mechanism to define 'internal' packages that may not be imported by packages outside the source subtree in which they reside. To create such a package, place it in a directory named `internal` or in a subdirectory of a directory named internal. When the `go` command sees an import of a package with `internal` in its path, it verifies that the package doing the import is within the tree rooted at the parent of the `internal` directory."

Source: [go.dev/doc/go1.4#internalpackages](https://go.dev/doc/go1.4#internalpackages)

**Current `go help` text:**

> "Code in or below a directory named 'internal' is importable only by code in the directory tree rooted at the parent of 'internal'."

Source: [pkg.go.dev/cmd/go#hdr-Internal_Directories](https://pkg.go.dev/cmd/go#hdr-Internal_Directories)

**Path semantics:**

```
.../a/b/internal/c/   →  importable only by code in .../a/b/
```

The `internal` segment becomes part of the import path verbatim. Imports from outside the eligible tree fail to compile with an error like `use of internal package ... not allowed`.

**Nested internal** — works recursively with **tighter** protection at each level:

- `.../a/internal/b/` — importable only by code in `.../a/`.
- `.../a/internal/b/internal/c/` — importable only by code in `.../a/internal/b/` (a *strict subset* of what can import `b`).

The Go standard library demonstrates deep nesting:
- [`src/internal/`](https://github.com/golang/go/tree/master/src/internal) — top-level stdlib internals
- [`src/crypto/internal/boring/`](https://github.com/golang/go/tree/master/src/crypto/internal/boring) — internal to `crypto/`
- [`src/net/http/internal/`](https://github.com/golang/go/tree/master/src/net/http/internal) — internal to `net/http/`

**`internal` and tests** — `package foo_test` (external test) declared in `foo/foo_test.go` can still import `foo/internal/bar`. The location of the test file (which directory) determines visibility, not the package name. There is no special carve-out for tests; the standard `internal` rule applies.

### 3. `pkg/` — history, current consensus, and when it's appropriate

**Origin:** The `pkg/` directory name originated in pre-Go-1.0 Go source trees where the standard library lived at `$GOROOT/src/pkg/...`. Brad Fitzpatrick picked it up for his Camlistore project, and from there it spread to Kubernetes, Moby/Docker, etcd, Terraform, CockroachDB, and many other large Go projects. Moby's own [`pkg/README.md`](https://github.com/moby/moby/blob/master/pkg/README.md) acknowledges this:

> "The directory `pkg` is named after the same directory in the camlistore project. Since Brad is a core Go maintainer, we thought it made sense to copy his methods for organizing Go code :) Thanks Brad!"

**Crucial fact — the Go team itself dropped `pkg/` from the stdlib in Go 1.4** ([go.dev/doc/go1.4](https://go.dev/doc/go1.4)):

> "In the main Go source repository, the source code for the packages was kept in the directory `src/pkg`, which made sense but differed from other repositories, including the Go subrepositories. In Go 1.4, the `pkg` level of the source tree is now gone, so for example the `fmt` package's source, once kept in directory `src/pkg/fmt`, now lives one level higher in `src/fmt`."

The current stdlib has no `pkg/` directory: every public package is a sibling of `cmd/`, `internal/`, and other public packages under `src/`.

**Criticisms:**

- **JBD (rakyll), Go team member** — [rakyll.org/style-packages/](https://rakyll.org/style-packages/): *"Avoid exposing your custom repository structure to your users. Align well with the GOPATH conventions. Avoid having `src/`, `pkg/` sections in your import paths."*
- **The Go team's own `go.dev/doc/modules/layout` page omits `pkg/` entirely.** For server projects it explicitly says: *"Server projects typically won't have packages for export, since a server is usually a self-contained binary (or a group of binaries). Therefore, it's recommended to keep the Go packages implementing the server's logic in the `internal` directory."*
- **Travis Jeffery's reversal essay** [travisjeffery.com/ill-take-pkg-over-internal/](https://travisjeffery.com/ill-take-pkg-over-internal/) — an experienced Go author who switched *toward* `pkg/` but documents the arguments on both sides; summarizes the anti-`pkg/` position: *"They don't want pkg in their import paths, which is the main issue with pkg. They call pkg empty: directories containing directories with Go code are empty, directories containing Go files are not empty. Less useful than the other empty directories—cmd and internal. We use cmd directories because compiled commands take their names from their source directories and we use internal directories for the compiler's rules for importing internal packages and these are compiler idiosyncrasies."*
- **golang-standards/project-layout** (the popular community layout reference, 56k stars, not endorsed by the Go team) itself acknowledges: *"Note that the `internal` directory is a better way to ensure your private packages are not importable because it's enforced by Go. The `/pkg` directory is still a good way to explicitly communicate that the code in that directory is safe for use by others."* And: *"This is a common layout pattern, but it's not universally accepted and some in the Go community don't recommend it."* And: *"This is `NOT an official standard defined by the core Go dev team`."*

**Consensus on when `pkg/` is appropriate:**

`pkg/` is appropriate **only when the repo genuinely intends to expose a public Go API** that other modules are expected to import, and the author wants to make that intent visually explicit.

It is **redundant or actively misleading** for the vast majority of Go repos — application repos, CLIs, services, microservices, single-binary tools — because:

1. It conflicts with `internal/`. Anything *not* under `internal/` is, by definition, importable by anyone. `pkg/` muddles "public-by-default" with "intended-for-public-consumption."
2. It is not enforced. Unlike `internal/`, `pkg/` is purely a social convention; nothing prevents external imports.
3. The Go team removed it from their own tree before the modern `cmd/` + `internal/` conventions solidified.
4. The official Go docs don't recommend it.

**Repos that use `pkg/` today (representative):**

| Repo | What it is | `pkg/` rationale / status |
|---|---|---|
| [Kubernetes](https://github.com/kubernetes/kubernetes/tree/master/pkg) | Massive monorepo, ~123k stars | Historical; predates `internal/`. README says *"Use of the `k8s.io/kubernetes` module or `k8s.io/kubernetes/...` packages as libraries is not supported."* — i.e., `pkg/` is used for code other *parts of the monorepo* use, treated as effectively private via documentation. |
| [Moby/Docker](https://github.com/moby/moby/tree/master/pkg) | ~71.7k stars | Legacy; project is actively migrating away. Public API now lives in `api/` and `client/`; root module is *"not intended to be imported as a Go library and has no API stability guarantees."* |
| [etcd](https://github.com/etcd-io/etcd) | Public Go client API | The `pkg/` directory holds the published client interface. |
| [Terraform](https://github.com/hashicorp/terraform/tree/main/internal) | Public infra-as-code tool | Migrated **away** from `pkg/` — only `internal/` remains. |

**Repos that explicitly do NOT use `pkg/`:**

- The Go standard library (no `pkg/` in `src/`)
- Most modern Go services (e.g., `controller-runtime`, `kubebuilder`, `traefik/traefik`, `caddyserver/caddy`)
- Zalando's [`skipper`](https://github.com/zalando/skipper)

### 4. Zalando Go guidance — what actually exists

**Zalando does NOT publish a Go language style guide.** This is the single most important fact about Zalando for this question.

Evidence:

- `https://github.com/zalando` org search for `style guide` → 1 result: [`zalando/dress-code`](https://github.com/zalando/dress-code) — a **CSS brand design system**, last touched 2018-02-05, public archive.
- `org:zalando go guidelines` → 0 results.
- DuckDuckGo `zalando "go style guide" OR "go guidelines"` → "No results found".
- The widely-cited [`zalando/restful-api-guidelines`](https://github.com/zalando/restful-api-guidelines) repo is a **REST API / HTTP / JSON** guide. 19 chapters covering `api-operation`, `http-status-codes-and-errors`, `json-guidelines`, `pagination`, `security`, etc. — zero Go-specific content. License: CC-BY-4.0.

**The only Go-language style guidance Zalando publishes** is one paragraph in [`zalando/skipper/CONTRIBUTING.md`](https://github.com/zalando/skipper/blob/master/CONTRIBUTING.md):

> "Skipper is formatted with gofmt. Please run it on your code before making a pull request. The coding style suggested by the Golang community is the preferred one for the cases that are not covered by gofmt, see the [style doc](https://github.com/golang/go/wiki/CodeReviewComments) for details."

The linked "style doc" is Google's `CodeReviewComments` — i.e., Zalando defers to Google.

**Zalando skipper's `AGENTS.md` adds soft rules** (skipper-specific, not Zalando-corporate-wide):

```
- Use idiomatic Go
- go doc on exposed things
- package docs in doc.go
- no comments in code if they are not critical
- no kubernetes client-go dependencies
- run linter `make lint` and fix all findings
```

**The layout Zalando actually uses in its flagship Go repo ([`zalando/skipper`](https://github.com/zalando/skipper), 3.3k stars):**

- `cmd/skipper/` — entry point. Yes.
- `internal/` — **NO**. Not present.
- `pkg/` — **NO**. Not present.
- Top-level packages: 35+ flat-by-feature (`circuit`, `config`, `dataclients`, `eskip`, `eskipfile`, `etcd`, `fastcgiserver`, `filters`, `io`, `jwt`, `loadbalancer`, `logging`, `metrics`, `net`, `otel`, `pathmux`, `predicates`, `proxy`, `proxylistener`, `queuelistener`, `ratelimit`, `routesrv`, `routing`, `scheduler`, `script`, `secrets`, `skptesting`, `swarm`, `tracing`, `validation`, …).

So Zalando's *de facto* recommendation, demonstrated by skipper, is:

- `cmd/<binary>/` at the repo root for entry points.
- No `pkg/` and no `internal/`; packages live directly at the module root or under domain-named subdirs.
- The module path itself (`github.com/zalando/skipper`) is the import path.

This is closer to Peter Bourgon's "Best Practices for Industrial Programming" / flat package layout than to `golang-standards/project-layout`.

### 5. Other major Go guidelines — cross-source comparison

| Topic | Uber | Google | Effective Go | Go team `modules/layout` | golang-standards | Dave Cheney | Stdlib | Kubernetes | Moby |
|---|---|---|---|---|---|---|---|---|---|
| **`internal/` for private packages** | — (not addressed) | — (not addressed) | — (pre-dates it, Go 1.0) | **YES**, *"keep packages in `internal` as much as possible"* | **YES** (compiler-enforced) | **YES** (§5.1.3) | **YES** (pervasive) | **NO** (uses `staging/`) | **YES** |
| **`pkg/` for public library code** | — (not addressed) | — (not addressed) | — (not addressed) | — (not mentioned) | **YES**, with disclaimer *"not universally accepted"* | **Implicitly NO** | **NO** | **YES** (legacy) | **YES** (legacy, migrating away) |
| **`cmd/` for binaries** | — (not addressed) | — (not addressed) | — (not addressed) | **YES**, *"common convention … very useful in a mixed repository"* | **YES**, `/cmd/myapp` per binary | **YES** (refs `cmd/contour`) | **YES** (`src/cmd/go`, `src/cmd/gofmt`) | **YES** (`cmd/kubelet` etc.) | **YES** (`cmd/dockerd`) |
| **Package names: short, lowercase, no underscores/mixedCaps** | YES | YES (Decisions) | YES (original source) | — (defers to Effective Go) | — (out of scope) | YES (§4.1) | YES (exemplar) | — | — |
| **Avoid `util`/`common`/`helpers`** | — | **YES** (Decisions) | implicit | — | — | **YES** (§4.2) | YES | — | — |

(— = not addressed by that source; YES/NO = explicit position.)

**Consensus (where all/most authoritative sources agree):**

1. **`internal/` is the correct way to express "package private to my project."** Recommended by the Go team's own doc, the community layout repo, Dave Cheney, and demonstrated by the standard library, Moby, and most modern Go projects. The Go team's wording — *"It's recommended to keep packages in `internal` as much as possible"* — is the strongest signal.
2. **`cmd/` is the conventional home for `package main` binaries** in repositories that also contain importable packages. The Go team, the community layout, Dave Cheney, the stdlib, Kubernetes, and Moby all agree.
3. **Package names are short, lowercase, one word, descriptive, no underscores, no stutter with the directory name.** Unanimous.
4. **One directory = one package.** Universal.
5. **Public package == sibling of `cmd/` and `internal/`.** No authority except `golang-standards/project-layout` recommends a `pkg/` shell layer above public packages; and that repo is explicitly *not endorsed by the Go team* (it says so in its own README).

**Disagreements:**

- **`pkg/`.** Pro: `golang-standards/project-layout`; Kubernetes (historical); Moby (historical, migrating away). Con / silent: Go team's `modules/layout` doc never mentions it; Dave Cheney implicitly opposes it; the stdlib does not have one; Uber, Google, Effective Go, Sameer Ajmani's "Package names" blog post are all silent on it.
- **Package granularity.** Dave Cheney: "prefer fewer, larger packages." Google, Effective Go, Sameer Ajmani: "make each package focused, avoid `util`/`common`." Compatible but different emphasis.
- **Top-level layout philosophy.** go-kit: domain-driven (`auth/`, `log/`, `metrics/`, `transport/`). Kubernetes: hybrid (`cmd/`, `pkg/`, `staging/`, `plugin/`). golang-standards: layer-driven with many auxiliary dirs. No official Go-team position prescribes a *top-level* layout beyond `cmd/` and `internal/`.

## Architecture Insights

For `freedius` specifically, the current layout has the following load-bearing properties that should not change:

1. **The codegen pipeline constrains `config/` and `proxy/translate/` to be public.** [`internal/genproviders/main.go:341-342`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/genproviders/main.go#L341-L342) writes Go source files that emit `import "github.com/pfrack/freedius/config"` and `import "github.com/pfrack/freedius/proxy/translate"` strings. A package under `internal/` cannot be imported by anything outside its own subtree. If we ever wanted to move `config/` or `proxy/translate/` under `internal/`, we'd need to either (a) move `genproviders` *outside* `internal/` so it could still emit imports of an `internal/` target — which would require relocating the generator itself, or (b) keep the generated packages public, defeating the move. So these two packages are stuck at the module root.

2. **`proxy/translate/` and `proxy/tui/` could live under `internal/` but currently don't.** The only callers of `proxy/translate/` are in `proxy/` itself ([openai_compat.go:14](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/openai_compat.go#L14), [mix.go:12](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/mix.go#L12), [count_tokens_local.go:8](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/count_tokens_local.go#L8)). The only caller of `proxy/tui/` is `main.go:26`. Both could be moved under `internal/proxy/` without breaking anything. The current choice (public, nested under `proxy/`) treats the whole `proxy/` subtree as an importable "proxy library" surface — defensible if and only if `freedius` someday wants to expose this as a public Go API. **Today, given the project is a single-binary application, the technically-correct choice would be to move both under `internal/proxy/`**, matching the Go team's *"keep packages in `internal` as much as possible"* recommendation.

3. **No `pkg/` is needed and would be wrong.** Adding `pkg/` would conflate "public-by-default" (anything not under `internal/` is importable) with "intended-for-public-consumption" — a category that `freedius` does not have. The Go team's `modules/layout` doc doesn't recommend `pkg/` even for libraries; for a single-binary application it's actively misleading.

4. **The `internal/` use cases are textbook.** Both `internal/envinject/` (operator-only side effect on the user's shell rc and `~/.claude/`) and `internal/genproviders/` (codegen tool) match exactly the Go team's guidance for what belongs under `internal/`: things that should not be importable by external code.

5. **Privacy as a layout constraint.** Three files ([proxy/proxy.go:1-6](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/proxy.go#L1-L6), [`proxy/translate/anthropic_openai.go:1-5`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/translate/anthropic_openai.go#L1-L5)) begin with a privacy-policy comment ("DO NOT log request or response bodies in this file"). Package boundaries also serve as privacy boundaries — this is another reason to keep these packages cohesive.

## Recommendation for `freedius`

**The current layout is correct and aligned with the Go team's own recommendation.** No structural change to the package tree is required. Concretely:

- ✅ **Keep `internal/envinject/` and `internal/genproviders/` as they are.** Both are textbook uses of `internal/`.
- ✅ **Keep `config/` and `proxy/` at the module root, public.** The codegen pipeline constrains them to be public; nothing in `freedius` is harmed by this.
- ❌ **Do not add a `pkg/` directory.** `freedius` is a single-binary application with no public Go API. `pkg/` would be redundant with `internal/` (compiler-enforced privacy) and misleading (no public API to point at).
- 🟡 **Optional cleanup: move `proxy/translate/` and `proxy/tui/` under `internal/proxy/`.** This would tighten the package surface to match the Go team's *"keep packages in `internal` as much as possible"* guidance. Trade-off: it costs nothing functionally, but loses the "the whole `proxy/` subtree is an importable library" framing — which today is purely hypothetical. **Defer until/unless there's a concrete external consumer.**
- 🟡 **Update `AGENTS.md` to document the directories it omits:** `internal/`, `proxy/translate/`, `proxy/tui/`, `magefiles/`, `templates/`, `providers.yaml`, and the codegen pipeline (`go generate ./...`). The contributor-facing map has outgrown the actual tree. This is a documentation issue, not a layout issue.

If the project ever does expose a public Go API (e.g., `freedius-go-sdk` or similar), the right pattern is to extract that API into its own repository with its own module path, not to add a `pkg/` directory to the existing repo.

## Code References

freedius-specific references (GitHub permalinks, commit `d361805`):

- [main.go:23-26](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main.go#L23-L26) — root `main` imports `config`, `internal/envinject`, `proxy`, `proxy/tui`.
- [main.go:1-3](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main.go#L1-L3) — package doc comment.
- [internal/envinject/snippet.go:5-6](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/envinject/snippet.go#L5-L6) — operator-only side-effect package.
- [internal/genproviders/main.go:1-18](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/internal/genproviders/main.go#L1-L18) — codegen tool that emits import strings for public packages at lines 341-342.
- [config/config.go:1-3](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/config/config.go#L1-L3) — package godoc.
- [proxy/proxy.go:1-6](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/proxy.go#L1-L6) — privacy-policy comment header.
- [proxy/translate/anthropic_openai.go:7-9](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/translate/anthropic_openai.go#L7-L9) — translate-package godoc.
- [proxy/tui/model.go:1-4](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/proxy/tui/model.go#L1-L4) — tui-package godoc.
- [AGENTS.md:11-19](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/AGENTS.md#L11-L19) — contributor-facing project-structure list (incomplete; missing `internal/`, `proxy/translate/`, `proxy/tui/`).

## Historical Context (from prior changes)

- [`context/archive/proxy-skeleton/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/proxy-skeleton) — `main.go` and `config/config.go` were split from day one (commit `aba3247`), establishing the public `config/` + private `proxy/` initial layout.
- [`context/archive/error-hardening/research.md:261`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/error-hardening/research.md#L261) — multi-binary `cmd/` layout was **explicitly rejected**: *"Forces `internal/` move; Go tooling doesn't ship multi-binary repos well; one binary name already in user scripts."*
- [`context/archive/error-hardening/research.md:311`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/error-hardening/research.md#L311) — rationale for `internal/envinject/`: *"env-injection's `WriteSettingsJSON` should live in a new `internal/envinject/` package so `init` can import it."*
- [`context/archive/provider-codegen/research.md:192`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/provider-codegen/research.md#L192) — rationale for `internal/genproviders/` mimicking the `envinject` precedent: *"Contains `envinject/` package — clean pattern to mimic."*
- [`context/archive/providers-section-refactor/research.md:217`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/providers-section-refactor/research.md#L217) — note on `internal/envinject/`'s dependency direction: *"reads env vars directly, no config dependency."*

## Related Research

No prior `context/changes/go-package-layout/research.md` exists; this is the first artifact on the topic. Related archived research (different topics, but cited above for layout-rationale context):

- [`context/archive/proxy-skeleton/`](https://github.com/pfrack/freedius/tree/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/proxy-skeleton)
- [`context/archive/error-hardening/research.md`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/error-hardening/research.md)
- [`context/archive/provider-codegen/research.md`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/provider-codegen/research.md)

## Open Questions

1. **`internal/genproviders` package name.** The file is `package main` under `internal/`. This is unusual but legal. An alternative would be `package genproviders` with no `func main()`, invoked as a library by a separate `cmd/genproviders/main.go`. Worth considering only if the generator ever needs to be consumed programmatically; today `go run ./internal/genproviders` is the only entry point.
2. **Future SDK extraction.** If `freedius` ever exposes a public Go API, the right pattern is to extract it into its own module (e.g., `github.com/pfrack/freedius-sdk`) rather than to add a `pkg/` directory to the existing repo. The current layout supports this without changes.
3. **AGENTS.md documentation drift.** As noted in the Recommendation section, the contributor-facing structure doc has not kept up with the on-disk layout. This is independent of the layout itself but worth fixing in a small follow-up.

## Authoritative sources cited (full URL list)

- Go 1.4 release notes (introduces `internal`, removes `pkg/` from stdlib): https://go.dev/doc/go1.4
- `cmd/go` documentation, "Internal Directories" section: https://pkg.go.dev/cmd/go#hdr-Internal_Directories
- Russ Cox's internal-packages design doc: https://go.dev/s/go14internal
- Go team "Organizing a Go module" (authoritative layout reference): https://go.dev/doc/modules/layout
- Effective Go: https://go.dev/doc/effective_go
- Sameer Ajmani, "Package names" (Go blog): https://go.dev/blog/package-names
- Uber Go Style Guide: https://github.com/uber-go/guide/blob/master/style.md
- Google Go Style Guide (Guide + Decisions): https://google.github.io/styleguide/go/
- golang-standards/project-layout: https://github.com/golang-standards/project-layout
- Dave Cheney, "Practical Go" / "Package oriented design": https://dave.cheney.net/practical-go/presentations/qcon-china.html
- JBD / rakyll, "Style guideline for Go packages": https://rakyll.org/style-packages/
- Travis Jeffery, "I'll take pkg over internal": https://travisjeffery.com/ill-take-pkg-over-internal/
- Standard Go library `src/` (no `pkg/`; has `cmd/` + `internal/`): https://github.com/golang/go/tree/master/src
- Standard library `cmd/`: https://github.com/golang/go/tree/master/src/cmd
- Kubernetes: https://github.com/kubernetes/kubernetes
- Moby / Docker: https://github.com/moby/moby/blob/master/pkg/README.md (acknowledgment of `pkg/` origin)
- Terraform (migrated away from `pkg/`): https://github.com/hashicorp/terraform/tree/main/internal
- Zalando skipper `CONTRIBUTING.md` (Zalando's only Go-language style guidance): https://github.com/zalando/skipper/blob/master/CONTRIBUTING.md
- Zalando skipper `AGENTS.md` (skipper-specific soft rules): https://github.com/zalando/skipper/blob/master/AGENTS.md
- Zalando restful-api-guidelines (REST, not Go): https://github.com/zalando/restful-api-guidelines

---

## Follow-up Research — 2026-06-21 (cmd/freedius/ reorganization)

**Trigger:** User pushback — "we should have `cmd/`". Direction: single-binary reorganization to `cmd/freedius/main.go`, aligned with the convention used by the Go stdlib, Kubernetes, Moby, and Zalando skipper. Does **not** add a second binary.

### Revised recommendation

**Move the application entry point into `cmd/freedius/`.** This brings `freedius` into line with the de-facto Go convention (Kubernetes `cmd/kubelet`, Moby `cmd/dockerd`, skipper `cmd/skipper`, stdlib `cmd/go`).

This **supersedes** the prior "no layout change required" recommendation in the Summary and Recommendation sections above. The `internal/`, `config/`, `proxy/`, `proxy/translate/`, and `proxy/tui/` package structure is unchanged — only the location of the single `package main` at the root moves.

The prior rejection documented at `context/archive/error-hardening/research.md:261` was specifically about **adding a second binary** (`cmd/freedius-init/main.go`), not about the `cmd/` convention per se. The user's current direction is the narrower option: keep one binary, but follow the convention.

### Authoritative backing for this layout

The Go team's own [`go.dev/doc/modules/layout`](https://go.dev/doc/modules/layout) explicitly endorses `cmd/` for "mixed repositories that have both commands and importable packages," and uses it in its own "Server" example. The stdlib uses it (`src/cmd/go`, `src/cmd/gofmt`, …). Major Go projects uniformly follow this convention:

| Project | Layout |
|---|---|
| Go stdlib | `src/cmd/<toolname>/main.go` |
| Kubernetes | `cmd/<binary>/main.go` (e.g. `cmd/kubelet`, `cmd/kubectl`) |
| Moby/Docker | `cmd/dockerd/main.go`, `cmd/docker-proxy/main.go` |
| Zalando skipper | `cmd/skipper/main.go` |
| Terraform | `cmd/<binary>/main.go` |

### Concrete migration — files that change

**Move:**
| From (root) | To |
|---|---|
| `main.go` | `cmd/freedius/main.go` |
| `main_test.go` | `cmd/freedius/main_test.go` |
| `templates/starter.yaml` | `cmd/freedius/templates/starter.yaml` |

**`//go:embed` path adjustment:** `main.go:46-47` does `//go:embed templates/starter.yaml`. Since the embed path is relative to the source file, moving both `main.go` and `templates/` together keeps the embed path identical (`./templates/starter.yaml`). No other code change is required inside `main.go`.

**Update build / run / test commands:**
| File | Current | After |
|---|---|---|
| [`magefiles/mage.go:27`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/magefiles/mage.go#L27) (`Build`) | `go build -o freedius .` | `go build -o freedius ./cmd/freedius` |
| [`magefiles/mage.go:45`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/magefiles/mage.go#L45) (`Run`) | `go run .` | `go run ./cmd/freedius` |
| [`magefiles/mage.go:54`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/magefiles/mage.go#L54) (`Verbose`) | `go run . --verbose-errors` | `go run ./cmd/freedius --verbose-errors` |
| [`test-manual.sh:42`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/test-manual.sh#L42) | `go build -o "$BIN" .` | `go build -o "$BIN" ./cmd/freedius` |
| [`README.md:11`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/README.md#L11) | `go build -o freedius .` | `go build -o freedius ./cmd/freedius` |
| [`AGENTS.md:7-8`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/AGENTS.md#L7-L8) | `go run .` / `go build -o freedius .` | `go run ./cmd/freedius` / `go build -o freedius ./cmd/freedius` |

**Files that do NOT change:**

- **`.github/workflows/ci.yml`** — uses `go test ./...`, `go vet ./...`, `go build ./...`, `go generate ./...`. All four glob patterns work unchanged because `./...` matches every package in the module, including the relocated `cmd/freedius`. No edit required.
- **`mage.go`** (root, `//go:build ignore`) and **`tools.go`** (root, `//go:build tools`) — these are **tooling artifacts**, not the application binary. The standard Go practice is to leave build-tool shims at the module root; they are not user-facing binaries. They stay put.
- **`magefiles/`** — already a separate Go module (`magefiles/go.mod`); unaffected.
- **`providers.yaml`**, **`config.example.yaml`**, **`.golangci.yaml`**, **`.gitignore`** — unaffected.
- **`scripts/pre-commit`** — calls `mage lint`, unaffected.

### What `freedius` will look like after this change

```
freedius/
├── cmd/
│   └── freedius/
│       ├── main.go             (was ./main.go)
│       ├── main_test.go        (was ./main_test.go)
│       └── templates/
│           └── starter.yaml    (was ./templates/starter.yaml)
├── config/                     (unchanged, public)
├── proxy/                      (unchanged, public)
│   ├── translate/              (unchanged, public nested)
│   └── tui/                    (unchanged, public nested)
├── internal/
│   ├── envinject/              (unchanged)
│   └── genproviders/           (unchanged; main package under internal/)
├── magefiles/                  (unchanged; separate Go module)
├── mage.go                     (unchanged; //go:build ignore bootstrap)
├── tools.go                    (unchanged; //go:build tools dep pin)
├── providers.yaml              (unchanged)
├── config.example.yaml         (unchanged)
├── go.mod, go.sum              (unchanged — no module-path change)
└── ...                         (everything else unchanged)
```

### Risk / verification checklist

Because this is a pure relocation (no logic changes), verification is mechanical:

1. `go build -o /tmp/freedius-test ./cmd/freedius` produces a working binary.
2. `./cmd/freedius/main.go` `//go:embed` still finds `templates/starter.yaml` (path is relative to the source file, so this is automatic once both move together).
3. `go vet ./...`, `go test -race -cover ./...`, `go generate ./...`, `go build ./...` all pass unchanged.
4. `mage Build`, `mage Run`, `mage Verbose`, `mage CI`, `mage ManualTest` all succeed.
5. `test-manual.sh` passes end-to-end.
6. No external consumer of `github.com/pfrack/freedius` exists; module path is unchanged, so import paths are unaffected.

### Open question raised by this follow-up

**Should `internal/genproviders/` also be moved to `cmd/genproviders/`?** It is currently `package main` under `internal/` — legal but unusual. Moving it to `cmd/genproviders/` would:
- Make it discoverable via `go run ./cmd/genproviders` (matches the codegen convention used by Kubernetes `cmd/`, protoc-gen-*, etc.)
- Eliminate the "package main under internal/" anomaly
- Cost: the generator's templates emit `import "github.com/pfrack/freedius/config"` and `import "github.com/pfrack/freedius/proxy/translate"` into generated files; nothing about its location under `internal/` is load-bearing for that (those public packages are not under `internal/`). It can move freely.

**Recommendation:** include the `internal/genproviders/` → `cmd/genproviders/` move in the same change, so the project ends up with a clean `cmd/freedius/` + `cmd/genproviders/` two-binary layout that matches Kubernetes/Moby/stdlib conventions end-to-end. Both are tiny, mechanical changes that can ship in one commit.
