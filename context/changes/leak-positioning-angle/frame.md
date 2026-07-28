# Frame Brief: Claude Code Leak as Freedius Selling Point

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

The Claude Code source leak (March 31, 2026, v2.1.88) revealed mechanisms
that detect proxy usage and deploy anti-distillation countermeasures.
The user wants to leverage this as a selling point for Freedius.

## Initial Framing (preserved)

- **User's stated cause or approach**: Claude Code checks which models it's
  working with; if Freedius emulates "proper models," Claude Code would
  "behave better" — unlocking "full agent mode."
- **User's proposed direction**: Build model emulation into Freedius and
  position "model emulation that bypasses detection" as a super selling point.
- **Pre-dispatch narrowing**: This is purely a selling-point question — user
  has not observed behavioral differences, has not tested side-by-side.

## Dimension Map

The observation could originate at any of these dimensions:

1. **Steganographic proxy detection** — Claude Code detects it's talking
   through a proxy and degrades behavior
2. **Anti-distillation poisoning** — Claude Code/server injects fake tools
   or garbled data when a proxy is detected
3. **Response model field gating** — Claude Code checks the `model` field
   in responses and gates features based on mismatch  ← user's framing
4. **Client-side model selection** — Claude Code's behavior differences
   come from which model WEIGHTS it chose to call, not from checking
   what responded
5. **Privacy/sovereignty angle** — the leak reveals Anthropic tracking
   users; Freedius routes away from Anthropic entirely

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| **#1: Steganographic detection degrades behavior** | Added v2.1.91 (April 2), REMOVED v2.1.198 (July 1). Targeted ONLY Chinese AI labs/resellers via timezone + hostname check. Never applied to localhost proxies. Source: The Register, The Decoder, Anthropic engineer Thariq Shihipar confirmation on X. | NONE — removed, never applied to Freedius |
| **#2: Anti-distillation poisoning affects Freedius users** | `ANTI_DISTILLATION_CC` flag (`tengu_anti_distill_fake_tool_injection`) — CLIENT sends flag in request, SERVER injects fakes. Freedius routes to NIM/OpenCode, not Anthropic. Those servers don't process the flag. Source: zetbit.tech analysis, claude.ts lines 301-313. | NONE — mechanism never fires when routing away from Anthropic |
| **#3: Response model field gating** | Freedius passes upstream model name in response (`proxy/translate/anthropic_openai.go:590`). No evidence in leaked source OR any analysis that Claude Code checks this field and gates features. The GitHub issue #40211 is about sub-agent MODEL SELECTION (client choosing which model to delegate to), not about checking responses. | NONE — no such mechanism exists |
| **#4: Client-side model selection is what matters** | Anthropic's official blog (July 7, 2026): "The model setting decides which set of frozen weights handles your request." Model is chosen BEFORE request. Effort level is sent as API parameter BEFORE request. Claude Code doesn't check what model responds — it already knows because it chose. Behavior differences = model capability differences (weights), not harness gates. | STRONG — this is how it actually works |
| **#5: Privacy/sovereignty as the real angle** | Steganographic tracking existed for 3 months. Anti-distillation poisons responses. Both are server-side Anthropic mechanisms. Freedius routes AWAY from Anthropic servers entirely. Alibaba banned Claude Code over tracking concerns (July 2026). Developer trust is a live issue. | STRONG — factually true, resonates with current news cycle |

## Narrowing Signals

- Anthropic's own blog (official, not leaked) explicitly states: model
  selection chooses which WEIGHTS handle the request. The weights are
  frozen. There is no "unlock" mechanism based on what model string appears
  in the response.
- Claude Code's effort level (how many tool calls, how much verification)
  is a client-side budget decision made BEFORE the API call. It does not
  change based on what comes back.
- The GitHub issue #40211 about model overriding is about Claude Code
  DELEGATING to cheaper sub-agents — a client-side routing decision, not
  a detection/gating mechanism.
- The user has NOT observed any behavioral difference — this is preemptive.

## Cross-System Convention

Proxy tools (mitmproxy, ngrok, Envoy) never claim to "emulate" the
upstream service to "unlock features." They position on: control,
visibility, privacy, cost. The "model emulation bypass" framing has
no precedent in adjacent tooling because the mechanism it assumes
(response-field gating) doesn't exist in how API clients work.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is:** The "model emulation unlocks
> full agent mode" premise is factually incorrect — no such gate exists.
> Claude Code's behavior differences come from model CAPABILITY (frozen
> weights), not from the harness detecting/restricting based on what
> model responded. The selling point the user wants exists, but it's a
> DIFFERENT selling point: privacy and sovereignty.

What IS true and IS a valid selling point:

1. **Anthropic was secretly tracking proxy users** for 3 months via
   steganography. This is verified, was widely reported, and led to
   Alibaba banning Claude Code.
2. **Anthropic's server injects fake tool data** to poison training
   data of anyone intercepting API traffic.
3. **Freedius routes away from Anthropic entirely.** No tracking. No
   fake tool injection. No steganographic fingerprinting. The user
   controls the pipe.

The privacy/sovereignty angle is:
- Factually accurate
- Timely (the story is < 1 month old, still in news cycle)
- Differentiated (no other Claude Code proxy explicitly positions on this)
- Verifiable (users can confirm: traffic goes to their chosen provider,
  not Anthropic)

The "model emulation" angle is:
- Factually incorrect (no such gate exists)
- Easily debunked (Anthropic's own blog explains how model selection works)
- Positioning against a solved problem (steganography was removed July 1)
- Potentially reputation-damaging (claiming to bypass something that
  doesn't exist signals either dishonesty or misunderstanding)

## Confidence

- **HIGH** — multiple independent sources confirm the mechanism. Anthropic's
  own official blog (July 7) explicitly describes how model selection works
  client-side. No evidence of response-field gating in any leaked source
  analysis. The privacy angle is independently verifiable.

## What Changes for /10x-plan

If a plan is needed, it should be about:

1. **Positioning copy that uses the privacy/sovereignty angle** — referencing
   the leak story as context for WHY a user would want to route away from
   Anthropic. "Your requests never touch Anthropic's servers. No tracking.
   No data poisoning. You choose the provider."
2. **NOT about building model emulation** — there is nothing to build.
   Claude Code already thinks it's talking to Claude (because Freedius
   intercepts at the base_url level). There is no additional emulation
   needed and no gate to bypass.

Do NOT plan work around "emulating models to unlock features." The premise
is false and building on it would be wasted effort at best, or embarrassing
positioning at worst.

## References

- Leak confirmation: GitHub mirrors (imattas/claude-code-leak, Exhen/claude-code-2.1.88, davccavalcante/claude-code-leaked)
- Steganographic detection: The Register (July 1, 2026), The Decoder (July 1, 2026)
- Removal confirmation: The Register ("version 2.1.198, released on July 1")
- Anti-distillation: zetbit.tech analysis, apidog.com analysis
- Model selection mechanism: claude.com/blog/claude-model-and-effort-level-in-claude-code (July 7, 2026)
- Alibaba ban: thenextweb.com (July 2026), sitepoint.com
- Freedius response model passthrough: `proxy/translate/anthropic_openai.go:590`
- Freedius env injection: `internal/envinject/settings.go` (sets ANTHROPIC_BASE_URL to localhost)
