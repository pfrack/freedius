package envinject

import "fmt"

func Snippet(host string, port int) string {
	addr := fmt.Sprintf("%s:%d", host, port)
	return fmt.Sprintf(`# Paste these into your shell to route Claude Code through freedius:
export ANTHROPIC_BASE_URL="http://%s"
export ANTHROPIC_API_KEY="freedius-dummy"
export ENABLE_TOOL_SEARCH="true"
# Optional: disable telemetry and error reporting
export DISABLE_TELEMETRY="1"
export DISABLE_ERROR_REPORTING="1"
# Or the kill-switch for all non-essential traffic:
# export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1"
# Silence this hint with --no-export-hint`, addr)
}
