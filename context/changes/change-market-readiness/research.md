---
date: 2026-07-19T14:30:00+00:00
researcher: opencode
branch: auto-review
git_commit: 1e5251ec57de4a7eb27e10e4f2a0fc7fc0f40600
repository: freedius
topic: "Market Readiness: Product, Positioning, Distribution, and Visibility"
tags: [research, market, launch, positioning, distribution, visibility, adoption]
status: complete
last_updated: 2026-07-19
last_updated_by: opencode
---

# Research: Market Readiness

**Date**: 2026-07-19T14:30:00+00:00
**Researcher**: opencode
**Git Commit**: 1e5251ec57de4a7eb27e10e4f2a0fc7fc0f40600
**Branch**: auto-review
**Repository**: freedius

## Research Question

What is the current state of freedius’s product, positioning, distribution, and visibility, and what gaps must be addressed to achieve a credible public launch that drives developer adoption, trust, and market differentiation?

## Summary

freedius is a **local, single-binary Anthropic-compatible gateway for Claude Code** that enables developers to route model aliases to heterogeneous OpenAI- and Anthropic-compatible providers, including local servers, direct vendors, and explicit fallback chains. The product is **not yet ready for broad public launch** due to critical gaps in release process, security, documentation, and distribution. However, the core implementation is **mature enough for a self-hosted developer beta** if the highest-priority issues are addressed.

The strongest defensible wedge is:

> **A local, single-binary gateway that keeps Claude Code’s Anthropic-facing API stable while routing model aliases to OpenAI- or Anthropic-compatible providers, including local servers, direct vendors, and configured fallbacks.**

This positioning is supported by:
- Local operation with no hosted control plane.
- API keys kept in environment variables.
- Requests and responses not logged by default.
- Model-family aliases (`opus`, `sonnet`, `haiku`) that survive upstream model renames.
- Explicit fallback chains for provider resilience.
- A single static binary with no runtime service dependencies.

**Key gaps** fall into four categories:
1. **Product readiness**: SIGTERM handling, proxy authentication, fallback reliability, SSE subscriber leaks, and missing API endpoints.
2. **Positioning and trust**: unsupported claims, missing compatibility evidence, no security/privacy documentation, and no license or community metadata.
3. **Distribution and installation**: no release pipeline, unversioned binaries/images, narrow CI coverage, and incomplete installation paths.
4. **Visibility and adoption**: no screenshots, demo recordings, comparison matrix, FAQ, or community operations.

A **30/60/90-day roadmap** is provided to address these gaps and enable a credible public launch.

---

## Detailed Findings

### 1. Product Readiness

#### Strengths
- **Local install story**: One Go binary, embedded UI assets, embedded starter configuration, and no runtime service dependencies. ([README.md:5](/home/pawel/code/freedius/README.md:5), [main.go:47](/home/pawel/code/freedius/cmd/freedius/main.go:47))
- **CLI ergonomics**: `--config`/`-c`, environment overrides, validation for bind host/port, `--help`, `--version`, selectable text/JSON logs, and configurable stream timeouts. ([main.go:61](/home/pawel/code/freedius/cmd/freedius/main.go:61), [main.go:107](/home/pawel/code/freedius/cmd/freedius/main.go:107))
- **YAML validation**: Strict validation for unknown fields, invalid provider behavior, bad URLs, unknown providers, unsafe model strings, and duplicate fallback entries. ([config.go:107](/home/pawel/code/freedius/config/config.go:107), [config.go:203](/home/pawel/code/freedius/config/config.go:203))
- **Config mutation**: Concurrency-aware with backup plus temporary-file replacement. ([config.go:363](/home/pawel/code/freedius/config/config.go:363), [handlers.go:459](/home/pawel/code/freedius/proxy/web/handlers.go:459))
- **Defensive behavior**: 10 MiB request limits, request IDs, bounded upstream contexts, panic recovery, structured access logs, no body logging, and sensitive-error redaction. ([proxy.go:28](/home/pawel/code/freedius/proxy/proxy.go:28), [errors.go:81](/home/pawel/code/freedius/proxy/errors.go:81))
- **UI functionality**: Live SSE logs/events, provider/mapping CRUD, fallback editing, model refresh, filtering, and mobile-oriented layout. ([handlers.go:24](/home/pawel/code/freedius/proxy/web/handlers.go:24), [handlers.go:89](/home/pawel/code/freedius/proxy/web/handlers.go:89))
- **Provider routing**: Modular with generated provider metadata, OpenAI/Anthropic adapters, explicit mix protocol selection, URL-based compatibility behavior, model family matching, and local token counting. ([mix.go:15](/home/pawel/code/freedius/proxy/mix.go:15), [proxy.go:116](/home/pawel/code/freedius/proxy/proxy.go:116))
- **Test coverage**: Broad unit coverage including fallback, translation, privacy, middleware, web handlers, config persistence, race-enabled tests, linting, generation checks, builds, and vulnerability scanning. ([mage.go:339](/home/pawel/code/freedius/magefiles/mage.go:339), [ci.yml:16](/home/pawel/code/freedius/.github/workflows/ci.yml:16))

#### Critical Gaps

| ID  | Gap                                                                                     | Evidence                                                                                     | Acceptance Criteria                                                                                     |
|-----|----------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------|
| P0  | SIGTERM shutdown is incorrect                                                          | [main.go:238](/home/pawel/code/freedius/cmd/freedius/main.go:238), [README.md:185](/home/pawel/code/freedius/README.md:185) | Register `syscall.SIGTERM`; add integration test for clean exit and connection draining.               |
| P0  | Proxy listener has no authentication, while Docker exposes it publicly                | [docker-compose.yml:4](/home/pawel/code/freedius/docker-compose.yml:4), [main.go:207](/home/pawel/code/freedius/cmd/freedius/main.go:207) | Bind Docker ports to localhost by default or add proxy auth token. Document unsafe exposure.          |
| P0  | `FREEDIUS_UI_TOKEN` makes normal browser navigation unusable                           | [handlers.go:47](/home/pawel/code/freedius/internal/eventstream/handlers.go:47), [server.go:35](/home/pawel/code/freedius/proxy/web/server.go:35) | Support secure session cookie or provide reverse-proxy/browser-auth recipe.                            |
| P0  | Anthropic transport failures do not participate in fallback                            | [anthropic_compat.go:127](/home/pawel/code/freedius/proxy/anthropic_compat.go:127), [proxy.go:333](/home/pawel/code/freedius/proxy/proxy.go:333) | Return typed transport error before writing; centralize transport-error response generation.          |
| P0  | Fallback retries every upstream 4xx, including invalid requests and authentication failures | [proxy.go:350](/home/pawel/code/freedius/proxy/proxy.go:350), [errors.go:100](/home/pawel/code/freedius/proxy/errors.go:100) | Define explicit retry policy; do not retry 400, 401, 403, 404, or semantic validation errors.         |
| P0  | Fallback status and response contracts need more specification                        | [openai_compat.go:146](/home/pawel/code/freedius/proxy/openai_compat.go:146), [anthropic_compat.go:141](/home/pawel/code/freedius/proxy/anthropic_compat.go:141) | Document and test mid-stream fallback behavior and error events.                                      |
| P0  | SSE subscribers leak permanently                                                       | [handlers.go:117](/home/pawel/code/freedius/internal/eventstream/handlers.go:117), [eventbus.go:111](/home/pawel/code/freedius/proxy/eventbus.go:111) | Defer unsubscribe; test subscriber count returns to baseline after disconnect.                        |
| P1  | SSE replay has a lost-event race                                                       | [handlers.go:104](/home/pawel/code/freedius/internal/eventstream/handlers.go:104), [handlers.go:117](/home/pawel/code/freedius/internal/eventstream/handlers.go:117) | Use atomic handoff or sequence reconciliation; test no missing events during connection setup.        |
| P1  | Docker proxy exposure is broken because `FREEDIUS_HOST` is ignored                    | [docker-compose.yml:10](/home/pawel/code/freedius/docker-compose.yml:10), [main.go:112](/home/pawel/code/freedius/cmd/freedius/main.go:112) | Read `FREEDIUS_HOST`; test published port `8082` accepts traffic.                                      |
| P1  | Network-exposed proxy has no authentication or transport protection                   | [main.go:452](/home/pawel/code/freedius/cmd/freedius/main.go:452), [server.go:35](/home/pawel/code/freedius/proxy/web/server.go:35) | Add TLS configuration or document unsafe exposure.                                                    |
| P1  | The fallback responder feature is not wired in production                             | [proxy.go:55](/home/pawel/code/freedius/proxy/proxy.go:55), [main.go:147](/home/pawel/code/freedius/cmd/freedius/main.go:147) | Construct `LastResponder`; assign to dispatcher and eventstream handlers; add production wiring test. |
| P1  | Required API keys are checked and then ignored                                         | [main.go:129](/home/pawel/code/freedius/cmd/freedius/main.go:129), [main.go:376](/home/pawel/code/freedius/cmd/freedius/main.go:376) | Decide whether missing keys should be fatal or warnings; log and surface consistently.                |
| P1  | There is no proxy `/v1/models` endpoint                                                | [main.go:452](/home/pawel/code/freedius/cmd/freedius/main.go:452), [proxy.go:138](/home/pawel/code/freedius/proxy/proxy.go:138) | Implement authenticated or local `GET/HEAD /v1/models`; test pagination/limit behavior.               |
| P1  | No readiness endpoint exists                                                           | [main.go:452](/home/pawel/code/freedius/cmd/freedius/main.go:452), [README.md:30](/home/pawel/code/freedius/README.md:30) | Add `/ready` or `/health?deep=1` reporting config validity, provider registration, and credential presence. |
| P1  | Configuration changes are not durable in the supplied Docker setup                    | [main.go:431](/home/pawel/code/freedius/cmd/freedius/main.go:431), [docker-compose.yml:1](/home/pawel/code/freedius/docker-compose.yml:1) | Mount named volume at nonroot user’s config directory or require explicit host-mounted config file.   |
| P1  | Config file permissions are unnecessarily broad                                        | [config.go:401](/home/pawel/code/freedius/config/config.go:401), [config.go:408](/home/pawel/code/freedius/config/config.go:408) | Use `0700` for config directory and `0600` for config file unless tooling compatibility requires otherwise. |
| P1  | The model-fetch endpoint creates a server-side request surface                        | [forms.go:166](/home/pawel/code/freedius/proxy/web/forms.go:166), [models.go:110](/home/pawel/code/freedius/proxy/models.go:110) | Restrict private/link-local destinations; cap response reads; redact model-fetch errors.               |
| P1  | Default startup behavior is friendly but ambiguous                                    | [main.go:315](/home/pawel/code/freedius/cmd/freedius/main.go:315), [main.go:431](/home/pawel/code/freedius/cmd/freedius/main.go:431) | Print effective config source/path prominently or materialize starter config with confirmation.       |

#### Highest-Value Launch Sequence

1. Fix `SIGTERM` handling and add end-to-end shutdown test.
2. Decide and enforce the security model for the proxy listener, especially Docker and non-loopback binds.
3. Make dashboard authentication usable from a browser.
4. Correct Anthropic fallback transport handling and define retryable status policy.
5. Wire `LastResponder` and add `/v1/models`.
6. Add readiness reporting, Docker config persistence, and SSRF/response-size protections.
7. Add black-box tests covering the real binary, authenticated browser/API flows, Docker behavior, fallback across both protocols, and provider/model discovery.

After these changes, the local single-user experience would be strong enough for a public beta.

---

### 2. Positioning and Differentiation

#### Strongest Defensible Wedge

> **A local, single-binary Anthropic-compatible gateway for Claude Code that lets one developer switch between heterogeneous OpenAI- and Anthropic-compatible providers, with model-family aliases, protocol translation, and explicit per-request fallback chains, while keeping credentials and payloads on the developer’s machine.**

This wedge is supported by:
- **Claude Code-oriented request compatibility**: The OpenAI adapter translates Anthropic requests and streams them back. ([openai_compat.go:18](/home/pawel/code/freedius/proxy/openai_compat.go:18))
- **Anthropic-to-OpenAI translation and reverse streaming translation**: The mix adapter selects Anthropic or OpenAI behavior based on explicit protocol or URL. ([mix.go:44](/home/pawel/code/freedius/proxy/mix.go:44))
- **Model-family aliases**: Family resolution exists specifically to avoid enumerating every Claude model version. ([proxy.go:116](/home/pawel/code/freedius/proxy/proxy.go:116))
- **Explicit fallback topology**: Fallbacks are configured per mapping, ordered, validated for duplicates, and bounded by a shared chain timeout. ([config.go:56](/home/pawel/code/freedius/config/config.go:56), [proxy.go:275](/home/pawel/code/freedius/proxy/proxy.go:275))
- **Local operation with no hosted control plane**: The binary is built with `CGO_ENABLED=0`, stripped, and runs on a distroless non-root image. ([Dockerfile:3](/home/pawel/code/freedius/Dockerfile:3))
- **Privacy-oriented default behavior**: The proxy explicitly prohibits logging request and response bodies; error snippets are sanitized and common credential patterns are redacted. ([proxy.go:1](/home/pawel/code/freedius/proxy/proxy.go:1), [errors.go:81](/home/pawel/code/freedius/proxy/errors.go:81))

#### Likely Competing Categories

1. **Production LLM gateways and routing platforms** (LiteLLM, Langfuse): multi-user operation, provider abstraction, observability, budgets, retries, deployment support.
2. **Hosted model routers and aggregators** (OpenRouter): unified API, provider breadth.
3. **OpenAI-compatible local proxies and inference servers** (Ollama, LM Studio, vLLM, llama.cpp): “run locally and expose an API.”
4. **Claude Code configuration wrappers and shell tooling**: lightweight environment switching.
5. **Observability and API management tools**: local operational metadata and configuration.

#### Unsupported or Overstated Claims

| Claim                                                                                     | Evidence                                                                                     | Recommendation                                                                                     |
|------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------|
| “Nobody has built” this category                                                          | [prd.md:20](/home/pawel/code/freedius/context/foundation/prd.md:20)                         | Revise to “we are optimized for...” unless externally validated.                                 |
| “Cheaper or free providers” as a durable value proposition                                | [prd.md:74](/home/pawel/code/freedius/context/foundation/prd.md:74)                         | Position cost savings as a use case, not a differentiator.                                      |
| “Claude Code cannot tell the difference”                                                   | [prd.md:38](/home/pawel/code/freedius/context/foundation/prd.md:38)                         | Treat as target acceptance criterion; provide end-to-end evidence.                              |
| “All requests transparently route” across providers                                         | [proxy.go:138](/home/pawel/code/freedius/proxy/proxy.go:138)                                | Qualify with a compatibility matrix; implement `/v1/models`.                                    |
| Fallback reliability is uniform across adapters                                            | [anthropic_compat.go:127](/home/pawel/code/freedius/proxy/anthropic_compat.go:127)          | Fix Anthropic transport-error handling; document mid-stream fallback limits.                  |
| Startup validation covers fallback credentials                                              | [main.go:376](/home/pawel/code/freedius/cmd/freedius/main.go:376)                           | Extend `checkRequiredEnvVars` to walk fallback providers.                                      |
| “Zero external runtime dependencies”                                                       | [README.md:5](/home/pawel/code/freedius/README.md:5)                                        | Revise to “single static binary with no runtime service dependencies.”                         |
| “Imperceptible overhead,” “sub-50MB,” and “negligible CPU”                                | [prd.md:85](/home/pawel/code/freedius/context/foundation/prd.md:85)                         | Add benchmarks and measurements.                                                              |
| The dashboard as a trust differentiator                                                    | [web-ui-friendliness/research.md:30](/home/pawel/code/freedius/context/changes/web-ui-friendliness/research.md:30) | Position as operational metadata, not analytics or observability.                              |

#### Missing Proof Points and Assets

1. **A real Claude Code compatibility demonstration**: reproducible transcript or video showing tool use, multi-turn context, streaming, errors, and model switching.
2. **Provider compatibility matrix**: per provider/protocol support for tool calls, streaming, reasoning content, count tokens, errors, model discovery, authentication, and known limitations.
3. **Golden request/response fixtures**: representative fixtures for Anthropic messages, tool use, tool results, thinking blocks, streaming boundaries, upstream errors, and fallback transitions.
4. **Measured benchmarks**: proxy overhead, streaming time-to-first-byte, memory at idle and during large tool payloads, and fallback latency.
5. **A trustworthy installation path**: release binary plus one-command installation; deprecate `go build` as the primary path.
6. **Security and privacy documentation**: threat model explaining loopback defaults, Docker exposure, UI token scope, provider error redaction limits, config-file permissions, and retained metadata.
7. **A license and project trust surface**: `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, issue templates, and support policy.
8. **A current Claude Code model-discovery story**: implement and document `/v1/models`.
9. **Failure-path evidence for fallback**: manual verification for broken-primary/working-fallback, full exhaustion, and timeout-budget behavior.
10. **A clear scope statement**: lead with the Claude Code local gateway use case; state what is intentionally unsupported (hosted multi-user gateway operation, billing, circuit breakers, mid-stream retry, full API compatibility, Windows support).

#### Recommended Market Message

> **Run Claude Code through the providers you already use.** Freedius is a local, single-binary gateway that keeps Claude Code’s Anthropic-facing API stable while routing model aliases to OpenAI- or Anthropic-compatible providers, including local servers, direct vendors, and configured fallbacks.

Supporting proof points:
- No hosted account or control plane.
- API keys stay in environment variables.
- Requests and responses are not logged by default.
- `opus`/`sonnet`/`haiku` mappings survive upstream model renames.
- A failed provider can fall through to explicitly configured alternatives.
- The same binary runs locally or in a non-root Docker container.

Avoid leading with “free,” “cheaper,” “zero dependencies,” “nobody else,” or “indistinguishable from Anthropic” until those claims have corresponding external research, measurements, and end-to-end evidence.

---

### 3. Distribution and Visibility

#### Strengths
- **Clear product positioning**: local LLM proxy for Claude Code/OpenCode, with a static binary and no runtime dependencies. ([README.md:1](/home/pawel/code/freedius/README.md:1))
- **Good operator baseline**: embedded default configuration, health endpoint, dashboard, configurable ports/hosts, UI token authentication, structured logs, graceful shutdown. ([README.md:22](/home/pawel/code/freedius/README.md:22), [main.go:159](/home/pawel/code/freedius/cmd/freedius/main.go:159))
- **Broad integration surface**: 16 provider definitions, OpenAI/Anthropic/mixed protocols, custom endpoints, fallback chains, and local providers. ([providers.yaml:23](/home/pawel/code/freedius/providers.yaml:23), [README.md:58](/home/pawel/code/freedius/README.md:58))
- **Strong automated quality targets**: race-enabled tests, generated-file checks, formatting, linting, vulnerability scanning, and build validation. ([mage.go:339](/home/pawel/code/freedius/magefiles/mage.go:339))
- **Docker runtime has sensible security defaults**: multi-stage build, static compilation, distroless image, non-root user, exposed service ports. ([Dockerfile:3](/home/pawel/code/freedius/Dockerfile:3), [Dockerfile:15](/home/pawel/code/freedius/Dockerfile:15))
- **Product direction is documented**: target persona, privacy expectations, provider compatibility boundaries. ([prd.md:18](/home/pawel/code/freedius/context/foundation/prd.md:18), [prd.md:85](/home/pawel/code/freedius/context/foundation/prd.md:85))

#### Priority Findings

| ID  | Gap                                                                                     | Evidence                                                                                     | Acceptance Criteria                                                                                     |
|-----|----------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------|
| P0  | No reproducible release process or published artifacts                                 | [ci.yml:1](/home/pawel/code/freedius/.github/workflows/ci.yml:1), [main.go:331](/home/pawel/code/freedius/cmd/freedius/main.go:331) | Add release workflow; publish versioned binaries, checksums, SBOM, and provenance.                    |
| P0  | Licensing and contribution signals are missing                                         | (No `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, or issue templates)  | Add license, contribution guide, security policy, code of conduct, and issue templates.               |
| P1  | Platform support is implied, not demonstrated                                          | [ci.yml:17](/home/pawel/code/freedius/.github/workflows/ci.yml:17)                          | Expand CI to Linux ARM64, macOS Intel/Apple Silicon, Windows; document support matrix.                |
| P1  | Docker distribution is usable locally but not registry-ready                           | [Dockerfile:8](/home/pawel/code/freedius/Dockerfile:8)                                      | Add image publishing workflow, versioned tags, healthcheck, SBOM, vulnerability scan, multi-arch build. |
| P1  | Documentation is strong on features but weak on first-run distribution                | [README.md:7](/home/pawel/code/freedius/README.md:7)                                        | Lead with released binaries; add exact Claude Code/OpenCode setup commands; document upgrade path.     |
| P1  | Integration claims need compatibility qualification and smoke coverage                | [providers.yaml:61](/home/pawel/code/freedius/providers.yaml:61)                            | Create provider/client compatibility matrix; add contract smoke tests.                               |
| P2  | Discoverability and community conversion are underdeveloped                           | [README.md:169](/home/pawel/code/freedius/README.md:169)                                    | Add screenshots, demo recording, architecture diagram, comparison matrix, FAQ, community operations.   |
| P2  | Commercial extension paths are not defined                                             | [tech-stack.md:5](/home/pawel/code/freedius/context/foundation/tech-stack.md:5)             | Document commercial boundary; add extension seams for hosted/team control plane.                     |

#### 30/60/90-Day Roadmap

**Days 0-30: Make one trustworthy release possible**

1. Add an explicit open-source license, contribution guide, security policy, code of conduct, support expectations, and issue templates.
2. Define semantic versioning, compatibility promises, release notes format, and a `VERSION`/tag policy.
3. Add a release workflow triggered by tags:
   - build Linux amd64/arm64, macOS amd64/arm64, and Windows amd64;
   - inject version, commit, and build date into the binary;
   - publish archives and SHA256 checksums to GitHub Releases;
   - attach SBOM and provenance/signing metadata.
4. Add a smoke test that downloads the release artifact, runs `--version`, starts the proxy, and verifies `/health`.
5. Fix README installation to lead with released binaries, then Docker, then source builds. Add exact Claude Code/OpenCode setup commands and an upgrade path.
6. Reconcile stale TUI/manual-test documentation and verify all examples against the current web UI behavior.

**Success gate**: A new user can install a pinned release without Go, verify its checksum/version, configure one provider, and complete a documented health check.

**Days 31-60: Expand platform and container confidence**

1. Turn CI into a matrix for supported Go/OS/architecture combinations, at minimum Linux amd64/arm64, macOS arm64/amd64, and Windows amd64.
2. Add Docker `buildx` publishing to GHCR with `latest`, semver, and immutable SHA tags; publish amd64/arm64 images.
3. Add Docker healthcheck, image labels, SBOM/vulnerability scanning, and a Compose example that requires or clearly documents UI authentication when exposed externally.
4. Create a provider/client compatibility matrix covering:
   - Claude Code and OpenCode;
   - Anthropic and OpenAI wire formats;
   - streaming, tool use, fallback, `/health`, and model discovery;
   - tested providers versus “community-reported” providers.
5. Add contract smoke tests using local mock servers for every adapter class, plus optional manually triggered real-provider checks that never expose credentials.
6. Add troubleshooting documentation for port conflicts, missing API keys, Docker networking, config resolution, streaming failures, and dashboard exposure.

**Success gate**: CI proves the documented binaries and images work on every supported platform, and users can determine whether their client/provider combination is supported before installing.

**Days 61-90: Build visibility, adoption, and extension economics**

1. Add GitHub release badges, download links, Docker pull instructions, project topics, a short architecture diagram, screenshots of the dashboard, and a demo workflow to the README.
2. Create `examples/` for Claude Code, OpenCode, Docker, Ollama, LM Studio, hosted OpenAI-compatible endpoints, and fallback routing.
3. Publish a changelog with upgrade notes and maintain a deprecation policy for config/provider changes.
4. Establish community operations: triage labels, issue response targets, “good first issue” guidance, provider contribution instructions, and a provider compatibility-report template.
5. Decide and document the commercial boundary:
   - open-source local proxy;
   - optional paid support;
   - hosted/team control plane;
   - enterprise auth, policy, audit, and fleet management;
   - or explicitly state that no commercial offering is planned.
6. Add extension seams only after the core boundary is chosen: stable config schema/versioning, provider registry contract, health/status API, and documented event/log export interface.

**Success gate**: The project is discoverable without knowing the repository URL, contributors know how to add providers safely, and potential commercial users can understand what is open source versus supported or hosted.

---

## Architecture Insights

### Request Flow

1. `cmd/freedius/main.go:147-175` loads config, creates provider registry, dispatcher, event bus, log sink, and web handlers.
2. `cmd/freedius/main.go:215-226` wraps proxy traffic in `RecoverMiddleware`, `EventBusMiddleware`, `AccessLogMiddleware`, and `RequestIDMiddleware`.
3. `cmd/freedius/main.go:452-479` routes `/health`, `/`, and all other paths to the proxy handler.
4. `proxy/proxy.go:138-218` validates method/content type, reads the body, extracts `model`, and resolves an exact or family mapping.
5. `proxy/proxy.go:275-347` builds the primary plus fallback chain and invokes the registry-selected adapter.
6. `proxy/openai_compat.go:90-155`, `proxy/anthropic_compat.go:101-149`, and `proxy/mix.go:44-79` translate or forward the request and stream the upstream response.
7. `proxy/eventbus.go:61-93` and `proxy/logtee.go:200-250` publish metadata and logs to in-memory rings and subscribers.
8. `internal/eventstream/handlers.go:87-188` replays ring-buffer contents and holds SSE subscriptions open.
9. `proxy/web/handlers.go:447-752` mutates the in-memory configuration and persists it through `config.Config.SaveData`.

### Key Architecture Risks

1. **Anthropic transport failures bypass fallback routing**
   - `proxy/anthropic_compat.go:128-133` catches transport errors, writes an error response directly, and returns `nil`.
   - `proxy/proxy.go:328-347` treats either `err == nil` or `ww.wroteHeader` as a completed attempt and exits the fallback loop.
   - **Fix**: Return a typed transport error before writing; centralize transport-error response generation in the dispatcher.

2. **Mid-stream Anthropic failures are silently converted into successful truncated responses**
   - `proxy/anthropic_compat.go:147-149` calls `io.Copy(w, resp.Body)` and ignores the returned error.
   - The response status has already been written, so fallback cannot safely occur after streaming begins.
   - **Fix**: Log read errors with request ID, provider, model, and byte/event progress; document mid-stream fallback limits.

3. **SSE subscribers leak permanently**
   - `internal/eventstream/handlers.go:117-132` subscribes to the event bus but never calls `Unsubscribe`.
   - **Fix**: Defer unsubscribe immediately after subscribing; test subscriber count returns to baseline after disconnect.

4. **SSE replay has a lost-event race**
   - Events are replayed using a snapshot at `internal/eventstream/handlers.go:104-115`.
   - The live subscription is created only afterward at `internal/eventstream/handlers.go:117-119`.
   - **Fix**: Use atomic handoff or sequence reconciliation; test no missing events during connection setup.

5. **Docker proxy exposure is broken because `FREEDIUS_HOST` is ignored**
   - `docker-compose.yml:10-11` sets `FREEDIUS_HOST=0.0.0.0` and `FREEDIUS_UI_HOST=0.0.0.0`.
   - `cmd/freedius/main.go:112-118` reads `--host` only and otherwise always uses `defaultHost`, `127.0.0.1`.
   - **Fix**: Read `FREEDIUS_HOST`; test published port `8082` accepts traffic.

6. **Network-exposed proxy has no authentication or transport protection**
   - `cmd/freedius/main.go:452-479` exposes the proxy handler without authentication.
   - **Fix**: Add TLS configuration or document unsafe exposure.

---

## Code References

### Product Readiness
- `cmd/freedius/main.go:238` — SIGTERM shutdown registration
- `docker-compose.yml:4` — Docker port binding
- `internal/eventstream/handlers.go:47` — UI token authentication
- `proxy/anthropic_compat.go:127` — Anthropic transport error handling
- `proxy/proxy.go:350` — Fallback retry policy
- `internal/eventstream/handlers.go:117` — SSE subscriber leak
- `proxy.go:55` — Fallback responder wiring
- `main.go:376` — Required API key validation
- `proxy.go:138` — Missing `/v1/models` endpoint
- `main.go:452` — Missing readiness endpoint
- `config.go:401` — Config file permissions
- `forms.go:166` — Model-fetch SSRF surface

### Positioning and Differentiation
- `README.md:3` — Product value proposition
- `proxy/openai_compat.go:18` — OpenAI adapter translation
- `proxy/mix.go:44` — Mix adapter protocol selection
- `proxy/proxy.go:116` — Model family resolution
- `config/config.go:56` — Fallback chain configuration
- `Dockerfile:3` — Distroless non-root image
- `proxy/errors.go:81` — Sensitive error redaction
- `prd.md:20` — “Nobody has built” claim
- `anthropic-models-api/research.md:24` — Missing `/v1/models` research
- `provider-fallback-routing/plan.md:44` — Fallback implementation

### Distribution and Visibility
- `ci.yml:1` — CI workflow
- `main.go:331` — Hard-coded version
- `Dockerfile:8` — Docker image metadata
- `README.md:7` — Installation instructions
- `providers.yaml:61` — Provider definitions
- `mage.go:339` — Automated quality targets
- `docker-compose.yml:1` — Docker Compose example

---

## Open Questions

1. Should the commercial boundary include a hosted/team control plane, or should the project remain a local open-source tool?
2. What is the minimum viable set of benchmarks needed to support “imperceptible overhead” and “sub-50MB” claims?
3. Should the project adopt a formal deprecation policy for config/provider changes, or maintain strict backward compatibility?
4. What is the best way to balance provider breadth with compatibility confidence (e.g., “tested” vs “community-reported” providers)?
5. Should the project pursue package-manager distribution (Homebrew, Chocolatey, AUR, etc.) or focus on direct download and Docker?

---

## Related Research

- `context/changes/anthropic-models-api/research.md` — Missing `/v1/models` endpoint
- `context/changes/provider-fallback-routing/research.md` — Fallback routing implementation
- `context/changes/web-ui-friendliness/research.md` — Web UI redesign and UX gaps
- `context/foundation/prd.md` — Original product requirements and scope
- `context/foundation/roadmap.md` — Product evolution and north star
- `context/foundation/test-plan.md` — High-impact test risks and coverage gaps