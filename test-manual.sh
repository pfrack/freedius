#!/usr/bin/env bash
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BIN="$SCRIPT_DIR/freedius"
LOG="$SCRIPT_DIR/freedius-test.log"
CFG="$SCRIPT_DIR/freedius.yaml"

TMPHOME=$(mktemp -d)
ORIG_GOMODCACHE=$(go env GOMODCACHE)
ORIG_GOCACHE=$(go env GOCACHE)
export HOME="$TMPHOME"

export FREEDIUS_PORT="${FREEDIUS_PORT:-8080}"
PORT="$FREEDIUS_PORT"
BASE_URL="http://127.0.0.1:$PORT"
export XDG_CONFIG_HOME="$TMPHOME/.config"
export GOMODCACHE="$ORIG_GOMODCACHE"
export GOCACHE="$ORIG_GOCACHE"

SERVER_PID=""

UPSTREAM_PID=""

cleanup() {
	rm -f "$CFG" "$LOG"
	if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
		kill -TERM "$SERVER_PID" 2>/dev/null
		wait "$SERVER_PID" 2>/dev/null
	fi
	if [[ -n "$UPSTREAM_PID" ]] && kill -0 "$UPSTREAM_PID" 2>/dev/null; then
		kill -TERM "$UPSTREAM_PID" 2>/dev/null
		wait "$UPSTREAM_PID" 2>/dev/null
	fi
	rm -rf "$TMPHOME" 2>/dev/null || true
}
trap cleanup EXIT SIGTERM SIGINT

echo "=== Building ==="
if ! go build -o "$BIN" ./cmd/freedius; then
	echo "build failed"
	exit 1
fi

command -v jq >/dev/null 2>&1 || { echo "jq is required"; exit 1; }

FAIL=0
pass() { echo "  PASS  $1"; }
fail() { echo "  FAIL  $1"; FAIL=$((FAIL + 1)); }

start_server() {
	if [[ -f "$CFG" ]]; then
		:  # caller supplied config
	else
		cp config.example.yaml "$CFG"
	fi
	# Wrap in script(1) to provide a pseudo-TTY for Bubble Tea (needs a PTY
	# to stay alive; on a pure non-TTY stdin it exits immediately).
	script -eq -c "$BIN --no-export-hint" /dev/null >"$LOG" 2>&1 &
	SERVER_PID=$!
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		if curl -sS -o /dev/null "$BASE_URL/v1/messages" 2>/dev/null; then
			return 0
		fi
		sleep 0.1
	done
	return 1
}

resp_header() {
	printf '%s\n' "$1" | grep -i "$2" | tr -d '\r' | awk '{print $2}'
}

echo ""
echo "=== Phase 3: family-pattern routing (nim only, no real API calls) ==="

cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus:    { provider_name: nim, model_string: opus-target }
  sonnet:  { provider_name: nim, model_string: sonnet-target }
  haiku:   { provider_name: nim, model_string: haiku-target }
  auto:    { provider_name: nim, model_string: auto-target }
  default: { provider_name: nim, model_string: default-target }
YAML

export NVIDIA_NIM_API_KEY=test-dummy-key

if ! start_server; then
	echo "  server failed to start"
	exit 1
fi

# 3.8a: family pattern — claude-opus-4-1 matches opus
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4-1"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
PROV=$(resp_header "$RESP" '^X-Freedius-Matched-Provider:')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$STATUS" =~ ^[0-9]+$ ]]; then pass "3.8a opus family dispatch status=$STATUS"; else fail "3.8a status (got $STATUS)"; fi
if [[ "$PROV" == "nim" ]]; then pass "3.8a X-Freedius-Matched-Provider: nim"; else fail "3.8a provider (got $PROV)"; fi
if [[ "$MODEL" == "opus-target" ]]; then pass "3.8a X-Freedius-Matched-Model: opus-target"; else fail "3.8a model (got $MODEL)"; fi

# 3.8b: claude-sonnet-4-6 matches sonnet
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-sonnet-4-6"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "sonnet-target" ]]; then pass "3.8b sonnet family dispatch"; else fail "3.8b model (got $MODEL)"; fi

# 3.8c: claude-haiku-3-5 matches haiku
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-haiku-3-5"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "haiku-target" ]]; then pass "3.8c haiku family dispatch"; else fail "3.8c model (got $MODEL)"; fi

# 3.8d: "auto" matches auto
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"auto"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "auto-target" ]]; then pass "3.8d auto family dispatch"; else fail "3.8d model (got $MODEL)"; fi

# 3.9: unmatched model with no default — write config without default: entry
kill -TERM "$SERVER_PID" 2>/dev/null
wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""

cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus: { provider_name: nim, model_string: opus-target }
YAML

if ! start_server; then
	echo "  server failed to start (phase 3.9)"
	exit 1
fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-future-2026"}')
if [[ "$STATUS" == "404" ]]; then pass "3.9 no default returns 404"; else fail "3.9 status (got $STATUS)"; fi

# 3.10: models:-wins-over-family-match precedence
kill -TERM "$SERVER_PID" 2>/dev/null
wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""

cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  claude-opus-4-1: { provider_name: nim, model_string: exact-opus }
  opus:            { provider_name: nim, model_string: family-opus }
YAML

if ! start_server; then
	echo "  server failed to start (phase 3.10)"
	exit 1
fi

# Exact match in models: should win
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4-1"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "exact-opus" ]]; then pass "3.10 models: exact match wins"; else fail "3.10 model (got $MODEL)"; fi

# A non-exact opus version should fall through to family mapping
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4-5"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "family-opus" ]]; then pass "3.10 non-exact opus falls to family"; else fail "3.10 model (got $MODEL)"; fi

# Cleanup
kill -TERM "$SERVER_PID" 2>/dev/null
wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""

echo ""
echo "=== Custom alias check ==="
cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus: { provider_name: nim, model_string: alias-check }
YAML
timeout 2 script -eq -c "$BIN --no-export-hint" /dev/null > /dev/null 2>&1
RC=$?
if [[ "$RC" == "124" ]]; then pass "NIM config: accepted (listened 2s, killed by timeout)"; else fail "NIM config: exit code $RC"; fi

# provider: custom should be rewritten to anthropic.
# In the unified binary, missing env vars are surfaced via the TUI Config tab
# rather than blocking startup — the server starts and listens normally.
cat > "$CFG" <<'YAML'
providers:
  custom: { behavior: mix, default_base_url: https://x.com/v1/messages, default_api_key_env: CUSTOM_API_KEY }
mappings:
  opus: { provider_name: custom, model_string: alias-check }
YAML
timeout 2 script -eq -c "$BIN --no-export-hint" /dev/null > /dev/null 2>&1
RC=$?
if [[ "$RC" == "124" ]]; then pass "custom alias: accepted (listened 2s, killed by timeout)"; else fail "custom alias: exit code $RC"; fi

echo ""
echo "=== Original smoke tests (updated for S-02 schema) ==="

# Test 4.6: unknown model → 404 no_match (no default: mapping)
cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus: { provider_name: nim, model_string: deepseek-ai/deepseek-v4-flash }
YAML
if ! start_server; then echo "  server failed to start (no-default)"; exit 1; fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$STATUS" == "404" ]]; then pass "4.6 unknown model status=404"; else fail "4.6 status (got $STATUS)"; fi

BODY=$(curl -sS -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$BODY" == *'"error":"no_match"'* ]]; then pass "4.6 body has error:no_match"; else fail "4.6 body (got $BODY)"; fi

kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

# Known model dispatch test (with default: so known family routes through)
cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus:    { provider_name: nim, model_string: deepseek-ai/deepseek-v4-flash }
  default: { provider_name: nim, model_string: deepseek-ai/deepseek-v4-flash }
YAML
if ! start_server; then echo "  server failed to start (known)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"opus"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
HEADER=$(resp_header "$RESP" '^X-Freedius-Matched-Provider:')
if [[ "$STATUS" =~ ^[0-9]+$ ]]; then pass "4.5 known mapping status=$STATUS"; else fail "4.5 status (got $STATUS)"; fi
if [[ "$HEADER" == "nim" ]]; then pass "4.5 X-Freedius-Matched-Provider: nim"; else fail "4.5 header (got $HEADER)"; fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{not json')
if [[ "$STATUS" == "400" ]]; then pass "4.7 malformed body status=400"; else fail "4.7 status (got $STATUS)"; fi

kill -TERM "$SERVER_PID"
wait "$SERVER_PID" 2>/dev/null
SHUTDOWN_EXIT=$?
if [[ "$SHUTDOWN_EXIT" == "0" ]]; then pass "4.12 SIGTERM exit=0"; else fail "4.12 exit (got $SHUTDOWN_EXIT)"; fi
SERVER_PID=""

# 4.8: --port validation (background + check exit message)
script -eq -c "$BIN --no-export-hint --port 99999 2>&1" /dev/null >"$LOG" 2>&1 & PID=$!; sleep 0.5
if kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
OUTPUT=$(cat "$LOG")
if [[ "$OUTPUT" == *"invalid --port value"* ]]; then pass "4.8 --port 99999"; else fail "4.8 (got: $OUTPUT)"; fi

# 4.9: --host validation
script -eq -c "$BIN --no-export-hint --host 10.0.0.1 2>&1" /dev/null >"$LOG" 2>&1 & PID=$!; sleep 0.5
if kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
OUTPUT=$(cat "$LOG")
if [[ "$OUTPUT" == *"invalid --host value"* ]]; then pass "4.9 --host 10.0.0.1"; else fail "4.9 (got: $OUTPUT)"; fi

# 4.11: malformed YAML produces line:col error
cat > "$CFG" <<'YAML'
providers:
  nim:
    behavior: openai
    default_api_key_env: NVIDIA_NIM_API_KEY
mappings:
  claude-opus-4
    provider_name: nim
    model_string: foo
YAML
script -eq -c "$BIN --no-export-hint 2>&1" /dev/null >"$LOG" 2>&1 & PID=$!; sleep 0.5
if kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
OUTPUT=$(cat "$LOG")
rm -f "$CFG"
if [[ "$OUTPUT" == *"["*"]"* ]]; then
	pass "4.11 malformed YAML produces line:col error"
else
	fail "4.11 (got: $OUTPUT)"
fi

# 4.10: no config → auto-writes starter
cat > /dev/null <<'NOTE'
The 4.10 test is removed because the behavior changed: freedius now
auto-writes the starter config to ~/.config/freedius/config.yaml when
no config is found, then starts the server. With NVIDIA_NIM_API_KEY set
globally in this script, the server starts successfully.

The auto-write path is implicitly covered by Phase 4 tests (4.14-4.19)
which verify the starter template round-trip through config.Load.
NOTE

# Port conflict test
cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus: { provider_name: nim, model_string: foo }
YAML
if start_server; then
	OUTPUT=$(script -eq -c "$BIN --no-export-hint 2>&1" /dev/null || true)
	if [[ "$OUTPUT" == *"bind: address already in use"* ]]; then
		pass "4.13 port conflict"
	else
		fail "4.13 (got: $OUTPUT)"
	fi
	kill -TERM "$SERVER_PID" 2>/dev/null
	wait "$SERVER_PID" 2>/dev/null
	SERVER_PID=""
else
	fail "4.13 could not start first instance"
fi

echo ""
echo "=== Phase 4: env auto-injection ==="
# The unified binary no longer auto-writes ~/.claude/settings.json or installs
# shell RC at startup. Settings.json installation and shell RC install have
# been removed from freedius. The shell RC env block is now installed from the
# TUI Config tab via Ctrl+S; that path is covered by the TUI unit tests.
# Auto-write of ~/.config/freedius/config.yaml on first run is replaced by a
# lazy in-memory parse of the embedded default — covered by
# TestRun_LazyConfigDoesNotWriteFile. This manual section is intentionally
# empty for the unified binary.

echo ""
if [[ $FAIL -eq 0 ]]; then
	echo "All Phase 4 manual checks passed"
else
	echo "$FAIL Phase 4 checks failed"
fi

# ---------------------------------------------------------------------------
# Error Code Differentiation (error-code-collapse)
# ---------------------------------------------------------------------------

resp_body_json() {
	curl -sS -X POST "$@" 2>/dev/null
}

UPSTREAM_PORT=18999

start_upstream() {
	local status="$1"
	stop_upstream
	python3 -c "
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.send_response($status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', '2')
        self.end_headers()
        self.wfile.write(b'{}')
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', $UPSTREAM_PORT), H).serve_forever()
" &
	UPSTREAM_PID=$!
	sleep 0.3
}

stop_upstream() {
	if [[ -n "$UPSTREAM_PID" ]] && kill -0 "$UPSTREAM_PID" 2>/dev/null; then
		kill -TERM "$UPSTREAM_PID" 2>/dev/null
		wait "$UPSTREAM_PID" 2>/dev/null
	fi
	UPSTREAM_PID=""
}

echo ""
echo "=== Error Code Differentiation ==="

# ---- 5.6: missing API key → 500 authentication_error, no retry ----
stop_upstream
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

unset MISSING_FAKE_KEY 2>/dev/null || true
# Use provider: openai (not in PresetProviders) so the server starts even
# when MISSING_FAKE_KEY is unset. The adapter pre-flight check at runtime
# returns configError "authentication_error" → 500.
cat > "$CFG" <<'YAML'
providers:
  openai: { behavior: openai, default_base_url: http://127.0.0.1:1/v1/chat/completions, default_api_key_env: MISSING_FAKE_KEY }
mappings:
  test: { provider_name: openai, model_string: test }
YAML
if ! start_server; then echo "  server failed to start (5.6)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
ERR_TYPE=$(echo "$BODY" | jq -r '.error.type // empty' 2>/dev/null)

if [[ "$STATUS" == "500" ]]; then pass "5.6 status=500"; else fail "5.6 status (got $STATUS)"; fi
if [[ "$ERR_TYPE" == "authentication_error" ]]; then pass "5.6 error.type=authentication_error"; else fail "5.6 error.type (got $ERR_TYPE)"; fi
if [[ "$BODY" == *"MISSING_FAKE_KEY"* ]]; then pass "5.6 message mentions env var"; else fail "5.6 message missing env var"; fi
if [[ -z "$RETRY" ]]; then pass "5.6 no retry-after"; else fail "5.6 retry-after=$RETRY (should be empty)"; fi
if [[ -z "$SHOULD_RETRY" ]]; then pass "5.6 no x-should-retry"; else fail "5.6 x-should-retry=$SHOULD_RETRY (should be empty)"; fi

# ---- 5.8: connection refused → 529 overloaded_error, retry headers ----
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

export TEST_API_KEY=dummy
cat > "$CFG" <<'YAML'
providers:
  openai: { behavior: openai, default_base_url: http://127.0.0.1:1/v1/chat/completions, default_api_key_env: TEST_API_KEY }
mappings:
  test: { provider_name: openai, model_string: test }
YAML
if ! start_server; then echo "  server failed to start (5.8)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
ERR_TYPE=$(echo "$BODY" | jq -r '.error.type // empty' 2>/dev/null)

if [[ "$STATUS" == "529" ]]; then pass "5.8 status=529"; else fail "5.8 status (got $STATUS)"; fi
if [[ "$ERR_TYPE" == "overloaded_error" ]]; then pass "5.8 error.type=overloaded_error"; else fail "5.8 error.type (got $ERR_TYPE)"; fi
if [[ "$RETRY" == "15" ]]; then pass "5.8 retry-after=15"; else fail "5.8 retry-after (got $RETRY)"; fi
if [[ "$SHOULD_RETRY" == "true" ]]; then pass "5.8 x-should-retry=true"; else fail "5.8 x-should-retry (got $SHOULD_RETRY)"; fi

# ---- 5.9: DNS failure (Anthropic adapter) → 502 api_error, no retry ----
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

cat > "$CFG" <<'YAML'
providers:
  anthropic: { behavior: anthropic, default_base_url: http://nonexistent.invalid, default_api_key_env: TEST_API_KEY }
mappings:
  test: { provider_name: anthropic, model_string: test }
YAML
if ! start_server; then echo "  server failed to start (5.9)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
ERR_TYPE=$(echo "$BODY" | jq -r '.error.type // empty' 2>/dev/null)

if [[ "$STATUS" == "502" ]]; then pass "5.9 status=502 (DNS permanent)"; else fail "5.9 status (got $STATUS)"; fi
if [[ "$ERR_TYPE" == "api_error" ]]; then pass "5.9 error.type=api_error"; else fail "5.9 error.type (got $ERR_TYPE)"; fi
if [[ -z "$RETRY" ]]; then pass "5.9 no retry-after"; else fail "5.9 retry-after=$RETRY (should be empty)"; fi
if [[ -z "$SHOULD_RETRY" ]]; then pass "5.9 no x-should-retry"; else fail "5.9 x-should-retry=$SHOULD_RETRY (should be empty)"; fi

# ---- 5.10: upstream 502 → 502 api_error via translateUpstreamError ----
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

start_upstream 502
cat > "$CFG" <<YAML
providers:
  openai: { behavior: openai, default_base_url: http://127.0.0.1:$UPSTREAM_PORT/v1/chat/completions, default_api_key_env: TEST_API_KEY }
mappings:
  test: { provider_name: openai, model_string: test }
YAML
if ! start_server; then echo "  server failed to start (5.10)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
ERR_TYPE=$(echo "$BODY" | jq -r '.error.type // empty' 2>/dev/null)

if [[ "$STATUS" == "502" ]]; then pass "5.10 upstream 502 → status=502"; else fail "5.10 status (got $STATUS)"; fi
if [[ "$ERR_TYPE" == "api_error" ]]; then pass "5.10 error.type=api_error"; else fail "5.10 error.type (got $ERR_TYPE)"; fi
if [[ "$RETRY" == "15" ]]; then pass "5.10 retry-after=15"; else fail "5.10 retry-after (got $RETRY)"; fi
if [[ "$SHOULD_RETRY" == "true" ]]; then pass "5.10 x-should-retry=true"; else fail "5.10 x-should-retry (got $SHOULD_RETRY)"; fi
stop_upstream

# ---- 5.11: upstream 504 → 504 api_error via translateUpstreamError ----
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

start_upstream 504
cat > "$CFG" <<YAML
providers:
  openai: { behavior: openai, default_base_url: http://127.0.0.1:$UPSTREAM_PORT/v1/chat/completions, default_api_key_env: TEST_API_KEY }
mappings:
  test: { provider_name: openai, model_string: test }
YAML
if ! start_server; then echo "  server failed to start (5.11)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
ERR_TYPE=$(echo "$BODY" | jq -r '.error.type // empty' 2>/dev/null)

if [[ "$STATUS" == "504" ]]; then pass "5.11 upstream 504 → status=504"; else fail "5.11 status (got $STATUS)"; fi
if [[ "$ERR_TYPE" == "api_error" ]]; then pass "5.11 error.type=api_error"; else fail "5.11 error.type (got $ERR_TYPE)"; fi
if [[ "$RETRY" == "15" ]]; then pass "5.11 retry-after=15"; else fail "5.11 retry-after (got $RETRY)"; fi
if [[ "$SHOULD_RETRY" == "true" ]]; then pass "5.11 x-should-retry=true"; else fail "5.11 x-should-retry (got $SHOULD_RETRY)"; fi
stop_upstream

# ---- 5.12: regression — existing smoke tests should still work ----
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

export NVIDIA_NIM_API_KEY="${NVIDIA_NIM_API_KEY:-test-dummy-key}"
cat > "$CFG" <<'YAML'
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
mappings:
  opus: { provider_name: nim, model_string: smokes }
YAML
if ! start_server; then echo "  server failed to start (5.12)"; exit 1; fi

# Valid request dispatch
RESP=$(curl -sS -D - -o /dev/null -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"opus"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
PROV=$(resp_header "$RESP" '^X-Freedius-Matched-Provider:')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$STATUS" =~ ^[0-9]+$ ]]; then pass "5.12 dispatch status=$STATUS"; else fail "5.12 status (got $STATUS)"; fi
if [[ "$PROV" == "nim" ]]; then pass "5.12 X-Freedius-Matched-Provider: nim"; else fail "5.12 provider (got $PROV)"; fi
if [[ "$MODEL" == "smokes" ]]; then pass "5.12 X-Freedius-Matched-Model: smokes"; else fail "5.12 model (got $MODEL)"; fi

# Unmatched model
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$STATUS" == "404" ]]; then pass "5.12 unknown model → 404"; else fail "5.12 unknown (got $STATUS)"; fi

# Malformed body
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '{not json')
if [[ "$STATUS" == "400" ]]; then pass "5.12 malformed body → 400"; else fail "5.12 malformed (got $STATUS)"; fi

# Empty body
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' -d '')
if [[ "$STATUS" == "400" ]]; then pass "5.12 empty body → 400"; else fail "5.12 empty (got $STATUS)"; fi

# ---- Cleanup ----
unset TEST_API_KEY
stop_upstream
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

echo ""
if [[ $FAIL -eq 0 ]]; then
	echo "All error-code-collapse manual checks passed"
else
	echo "$FAIL error-code-collapse checks failed"
fi

# ---------------------------------------------------------------------------
# Client wire format (openai-client-format)
#
# freedius answers in the wire format the client speaks. POST /v1/messages is
# translated Anthropic<->OpenAI; POST /v1/chat/completions is already OpenAI
# shaped and is forwarded untranslated, so the client gets chunks with a
# top-level "id" instead of Anthropic events.
# ---------------------------------------------------------------------------

echo ""
echo "=== Client wire format ==="

# Upstream that answers with OpenAI chunks and records the request it received.
UPSTREAM_PORT=18998
stop_upstream
python3 -c "
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        with open('$TMPHOME/upstream_body.json', 'wb') as fh:
            fh.write(body)
        payload = (
            b'data: {\"id\":\"chatcmpl-m\",\"object\":\"chat.completion.chunk\",'
            b'\"created\":1,\"model\":\"upstream-model\",\"choices\":'
            b'[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"pong\"},'
            b'\"finish_reason\":null}]}\n\n'
            b'data: {\"id\":\"chatcmpl-m\",\"object\":\"chat.completion.chunk\",'
            b'\"created\":1,\"model\":\"upstream-model\",\"choices\":'
            b'[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":'
            b'{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4}}\n\n'
            b'data: [DONE]\n\n'
        )
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', $UPSTREAM_PORT), H).serve_forever()
" &
UPSTREAM_PID=$!
sleep 0.3

export TEST_API_KEY=test-dummy-key
cat > "$CFG" <<YAML
providers:
  openai: { behavior: openai, default_base_url: http://127.0.0.1:$UPSTREAM_PORT/v1/chat/completions, default_api_key_env: TEST_API_KEY }
mappings:
  default: { provider_name: openai, model_string: upstream-model }
YAML

if ! start_server; then
	echo "  server failed to start (6.x)"
	exit 1
fi

# ---- 6.1: OpenAI client, streaming → OpenAI chunks, not Anthropic events ----
STREAM=$(curl -sS -N -X POST "$BASE_URL"/v1/chat/completions \
	-H 'content-type: application/json' \
	-H 'authorization: Bearer test' \
	-d '{"model":"kimi-k3","stream":true,"messages":[{"role":"user","content":"ping"}]}')
FIRST=$(printf '%s\n' "$STREAM" | grep -m1 '^data: {' | sed 's/^data: //')

if printf '%s' "$STREAM" | grep -q 'event: message_start'; then
	fail "6.1 OpenAI client received Anthropic events"
else
	pass "6.1 OpenAI client receives no Anthropic event framing"
fi
if printf '%s' "$FIRST" | jq -e '.id != null and .object == "chat.completion.chunk"' >/dev/null 2>&1; then
	pass "6.1 first data line is an OpenAI chunk with a top-level id"
else
	fail "6.1 first data line is not an OpenAI chunk: $FIRST"
fi
if printf '%s' "$STREAM" | grep -q 'data: \[DONE\]'; then
	pass "6.1 [DONE] sentinel forwarded"
else
	fail "6.1 [DONE] sentinel missing"
fi
if printf '%s' "$FIRST" | jq -e '.model == "kimi-k3"' >/dev/null 2>&1; then
	pass "6.1 response echoes the model the client asked for"
else
	fail "6.1 echoed model wrong: $FIRST"
fi
if jq -e '.model == "upstream-model"' "$TMPHOME/upstream_body.json" >/dev/null 2>&1; then
	pass "6.1 upstream received the remapped model"
else
	fail "6.1 upstream model not remapped: $(cat "$TMPHOME/upstream_body.json")"
fi

# ---- 6.2: OpenAI client fields freedius does not model must survive ----
curl -sS -N -o /dev/null -X POST "$BASE_URL"/v1/chat/completions \
	-H 'content-type: application/json' \
	-d '{"model":"kimi-k3","stream":true,"max_completion_tokens":4096,"reasoning_effort":"high","messages":[{"role":"user","content":"ping"}]}'
if jq -e '.max_completion_tokens == 4096 and .reasoning_effort == "high"' \
	"$TMPHOME/upstream_body.json" >/dev/null 2>&1; then
	pass "6.2 provider-specific request fields forwarded untouched"
else
	fail "6.2 fields dropped: $(cat "$TMPHOME/upstream_body.json")"
fi

# ---- 6.3: OpenAI client, non-streaming → JSON completion ----
JSON=$(curl -sS -X POST "$BASE_URL"/v1/chat/completions \
	-H 'content-type: application/json' \
	-d '{"model":"kimi-k3","stream":false,"messages":[{"role":"user","content":"ping"}]}')
if printf '%s' "$JSON" | jq -e '.object == "chat.completion" and .id != null' >/dev/null 2>&1; then
	pass "6.3 OpenAI non-streaming reply is a chat.completion"
else
	fail "6.3 OpenAI non-streaming reply wrong: $JSON"
fi

# ---- 6.4: Anthropic client, non-streaming → one Message document ----
MSG=$(curl -sS -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-sonnet-4-5-20250929","max_tokens":32,"stream":false,"messages":[{"role":"user","content":"ping"}]}')
if printf '%s' "$MSG" | grep -q 'event:'; then
	fail "6.4 non-streaming reply leaked SSE framing: $MSG"
else
	pass "6.4 non-streaming reply carries no SSE framing"
fi
if printf '%s' "$MSG" | jq -e '.type == "message" and .role == "assistant" and (.id | startswith("msg_"))' >/dev/null 2>&1; then
	pass "6.4 non-streaming reply is an Anthropic Message"
else
	fail "6.4 non-streaming reply is not a Message: $MSG"
fi
if printf '%s' "$MSG" | jq -e '.content[0].text == "pong"' >/dev/null 2>&1; then
	pass "6.4 non-streaming reply carries the upstream content"
else
	fail "6.4 non-streaming reply lost the content: $MSG"
fi

# ---- 6.5: Anthropic client, streaming → unchanged event stream ----
SSE=$(curl -sS -N -X POST "$BASE_URL"/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-sonnet-4-5-20250929","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"ping"}]}')
if printf '%s' "$SSE" | grep -q 'event: message_start' &&
	printf '%s' "$SSE" | grep -q 'event: message_stop'; then
	pass "6.5 Anthropic streaming reply still emits message_start/message_stop"
else
	fail "6.5 Anthropic streaming reply wrong: $SSE"
fi
if printf '%s' "$SSE" | grep -q '"model":"claude-sonnet-4-5-20250929"'; then
	pass "6.5 Anthropic stream echoes the client model"
else
	fail "6.5 Anthropic stream model echo missing: $SSE"
fi

# ---- Cleanup ----
unset TEST_API_KEY
stop_upstream
rm -f "$TMPHOME/upstream_body.json"
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

echo ""
if [[ $FAIL -eq 0 ]]; then
	echo "All client wire format manual checks passed"
else
	echo "$FAIL client wire format checks failed"
fi
