# Change: Custom Chat UI with MCP

- **Created**: 2026-10-17
- **Status**: preparing
- **Frame**: [frame.md](frame.md)

## Summary

Build a custom chat UI in Go + htmx that supports MCP (Model Context Protocol).
Lumien lacks MCP support; the user needs chat + history + multiple models +
MCP tools (network, system, files).

## Motivation

- Lumien is a basic chat UI without MCP
- User wants full stack: chat + history + models + MCP
- User wants a serious project (tests, CI, clean code)
- Existing alternatives are unusable

## Scope

- MCP client: tool discovery, tool calls, permissions, streaming results
- Chat UI: messages, history, multi-model support
- Integration with freedius (existing Go proxy)
- Stack: Go + htmx

## References

- Frame brief: [frame.md](frame.md)