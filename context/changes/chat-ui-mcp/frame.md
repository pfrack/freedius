# Frame Brief: Custom Chat UI with MCP

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

Lumien is a basic chat UI without MCP support. The user needs chat + history +
multiple models + MCP (tools, network, system). Lumien must be downloaded from
releases (distribution friction). The user wants their own Go program that
works the way they want.

## Initial Framing (preserved)

- **User's stated cause or approach**: "Lumien is overkill"
- **User's proposed direction**: Write own chat UI in htmx + Go
- **Pre-dispatch narrowing**: Lumien = basic chat without MCP; user needs full
  stack including MCP; user wants a serious project (tests, CI, clean code);
  existing alternatives are "chujowe" (unusable)

## Dimension Map

The observation could originate at any of these dimensions:

1. **Feature scope** — lumien is a general-purpose chat UI, but the user only
   needs basic chat. *Refuted by user: they need full stack + MCP.*
2. **Tool evaluation** — does lumien provide MCP? *User confirmed: it does not.
   This is where the framing breaks: lumien is UNDERKILL, not overkill.*
3. **Build cost** — is writing a custom UI with MCP actually simpler than
   lumien? *MCP client is non-trivial: tool discovery, call UI, permissions,
   streaming results. But user is a Go developer with skills to match.*
4. **Distribution** — release downloads vs. self-built binary. *Minor friction,
   not the driver.*
5. **Control** — user wants control over their tooling stack. *Partially true,
   but the real driver is missing MCP support.*

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Dimension 1: lumien is overkill (too complex) | User says they need only basic chat | REFUTED — user needs MCP, which is advanced, not basic |
| Dimension 2: lumien lacks needed features (underkill) | User confirmed lumien has no MCP | STRONG — this is the real gap |
| Dimension 3: build cost is acceptable | User is Go dev running freedius; wants serious project | STRONG — skills and motivation present |
| Dimension 4: distribution friction is the driver | Release download is minor | WEAK — not the main issue |
| Dimension 5: control is the main driver | User wants "wlasny program" | PARTIAL — real but secondary to MCP gap |

## Narrowing Signals

- **User confirmed lumien has no MCP** — this is the decisive observation.
  It rules out "overkill" and confirms "underkill".
- **User wants "pełny stack"** — chat + history + models + MCP. This is a
  full-featured client, not a minimal replacement.
- **User wants a serious project** — tests, CI, clean code. Not a hack.
- **Existing alternatives are "chujowe"** — user has evaluated and rejected
  other options. This is a strong signal that custom is justified.

## Cross-System Convention

In this project (freedius), the convention is: build focused tools that do
one thing well, with tests and CI. A chat UI with MCP would follow the same
pattern: focused, tested, maintainable.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: I need a chat UI that supports
> MCP. Lumien doesn't provide it. I want to build my own in Go + htmx.

The initial framing ("lumien is overkill") was a misdiagnosis. Lumien isn't too
complex — it's too simple. The user's direction (build custom UI) is correct,
but for a different reason: **MCP support is missing**, not "too many features."
The plan should be about building a chat UI that supports MCP, not about
replacing an "overkill" tool.

## Confidence

- **HIGH** — strong evidence (lumien lacks MCP), matches convention (focused
> tested tool), decisive narrowing signal (user confirmed MCP gap and wants
> serious project).

## What Changes for /10x-plan

The plan should be about: **building a chat UI with MCP support in Go + htmx**.
Key areas: MCP client (tool discovery, tool calls, permissions, streaming),
chat UI (messages, history, multi-model), and integration with freedius.
The plan should NOT be about "simplifying" or "replacing overkill" — it's about
**adding missing capability (MCP)**.

## References

- User interview: lumien = basic chat, no MCP, release distribution
- User wants: chat + history + models + MCP, serious project, Go + htmx
- Stack: Go (freedius exists), htmx (user's choice)
- Convention: focused, tested, maintainable tools (freedius pattern)