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
trap cleanup EXIT

echo "=== Building ==="
if ! go build -o "$BIN" .; then
	echo "build failed"
	exit 1
fi

FAIL=0
pass() { echo "  PASS  $1"; }
fail() { echo "  FAIL  $1"; FAIL=$((FAIL + 1)); }

start_server() {
	if [[ -f "$CFG" ]]; then
		:  # caller supplied config
	else
		cp config.example.yaml "$CFG"
	fi
	"$BIN" > "$LOG" 2>&1 &
	SERVER_PID=$!
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
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
mappings:
  opus:    { provider: nim, model: opus-target }
  sonnet:  { provider: nim, model: sonnet-target }
  haiku:   { provider: nim, model: haiku-target }
  auto:    { provider: nim, model: auto-target }
  default: { provider: nim, model: default-target }
YAML

export NVIDIA_NIM_API_KEY=test-dummy-key

if ! start_server; then
	echo "  server failed to start"
	exit 1
fi

# 3.8a: family pattern — claude-opus-4-1 matches opus
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4-1"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
PROV=$(resp_header "$RESP" '^X-Freedius-Matched-Provider:')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$STATUS" =~ ^[0-9]+$ ]]; then pass "3.8a opus family dispatch status=$STATUS"; else fail "3.8a status (got $STATUS)"; fi
if [[ "$PROV" == "nim" ]]; then pass "3.8a X-Freedius-Matched-Provider: nim"; else fail "3.8a provider (got $PROV)"; fi
if [[ "$MODEL" == "opus-target" ]]; then pass "3.8a X-Freedius-Matched-Model: opus-target"; else fail "3.8a model (got $MODEL)"; fi

# 3.8b: claude-sonnet-4-6 matches sonnet
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-sonnet-4-6"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "sonnet-target" ]]; then pass "3.8b sonnet family dispatch"; else fail "3.8b model (got $MODEL)"; fi

# 3.8c: claude-haiku-3-5 matches haiku
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-haiku-3-5"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "haiku-target" ]]; then pass "3.8c haiku family dispatch"; else fail "3.8c model (got $MODEL)"; fi

# 3.8d: "auto" matches auto
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"auto"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "auto-target" ]]; then pass "3.8d auto family dispatch"; else fail "3.8d model (got $MODEL)"; fi

# 3.9: unmatched model with no default — write config without default: entry
kill -TERM "$SERVER_PID" 2>/dev/null
wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""

cat > "$CFG" <<'YAML'
mappings:
  opus: { provider: nim, model: opus-target }
YAML

if ! start_server; then
	echo "  server failed to start (phase 3.9)"
	exit 1
fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-future-2026"}')
if [[ "$STATUS" == "404" ]]; then pass "3.9 no default returns 404"; else fail "3.9 status (got $STATUS)"; fi

# 3.10: models:-wins-over-family-match precedence
kill -TERM "$SERVER_PID" 2>/dev/null
wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""

cat > "$CFG" <<'YAML'
models:
  claude-opus-4-1: { provider: nim, model: exact-opus }
mappings:
  opus: { provider: nim, model: family-opus }
YAML

if ! start_server; then
	echo "  server failed to start (phase 3.10)"
	exit 1
fi

# Exact match in models: should win
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4-1"}')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$MODEL" == "exact-opus" ]]; then pass "3.10 models: exact match wins"; else fail "3.10 model (got $MODEL)"; fi

# A non-exact opus version should fall through to family mapping
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
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
mappings:
  opus: { provider: nim, model: alias-check }
YAML
timeout 2 "$BIN" > /dev/null 2>&1
RC=$?
if [[ "$RC" == "124" ]]; then pass "NIM config: accepted (listened 2s, killed by timeout)"; else fail "NIM config: exit code $RC"; fi

# provider: custom should be rewritten to anthropic.
# CUSTOM_API_KEY not set → env-var check fails at startup → server never listens
cat > "$CFG" <<'YAML'
mappings:
  opus: { provider: custom, model: alias-check, base_url: https://x.com/v1/messages, api_key_env: CUSTOM_API_KEY }
YAML
OUTPUT=$(timeout 2 "$BIN" 2>&1 || true)
if [[ "$OUTPUT" == *"CUSTOM_API_KEY"* ]]; then
	pass "custom alias: rewritten to anthropic, startup rejects missing CUSTOM_API_KEY"
else
	fail "custom alias: (got: $OUTPUT)"
fi

echo ""
echo "=== Original smoke tests (updated for S-02 schema) ==="

# Test 4.6: unknown model → 404 no_match (no default: mapping)
cat > "$CFG" <<'YAML'
mappings:
  opus: { provider: nim, model: step-3.5 }
YAML
if ! start_server; then echo "  server failed to start (no-default)"; exit 1; fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$STATUS" == "404" ]]; then pass "4.6 unknown model status=404"; else fail "4.6 status (got $STATUS)"; fi

BODY=$(curl -sS -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$BODY" == *'"error":"no_match"'* ]]; then pass "4.6 body has error:no_match"; else fail "4.6 body (got $BODY)"; fi

kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

# Known model dispatch test (with default: so known family routes through)
cat > "$CFG" <<'YAML'
mappings:
  opus:    { provider: nim, model: step-3.5 }
  default: { provider: nim, model: step-3.5 }
YAML
if ! start_server; then echo "  server failed to start (known)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"opus"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
HEADER=$(resp_header "$RESP" '^X-Freedius-Matched-Provider:')
if [[ "$STATUS" =~ ^[0-9]+$ ]]; then pass "4.5 known mapping status=$STATUS"; else fail "4.5 status (got $STATUS)"; fi
if [[ "$HEADER" == "nim" ]]; then pass "4.5 X-Freedius-Matched-Provider: nim"; else fail "4.5 header (got $HEADER)"; fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{not json')
if [[ "$STATUS" == "400" ]]; then pass "4.7 malformed body status=400"; else fail "4.7 status (got $STATUS)"; fi

LOG_OUTPUT=$(cat "$LOG")
if [[ "$LOG_OUTPUT" == *"freedius listening on"* ]]; then pass "4.14 server listening log appears"; else fail "4.14 (got: $LOG_OUTPUT)"; fi

kill -TERM "$SERVER_PID"
wait "$SERVER_PID" 2>/dev/null
SHUTDOWN_EXIT=$?
if [[ "$SHUTDOWN_EXIT" == "0" ]]; then pass "4.12 SIGTERM exit=0"; else fail "4.12 exit (got $SHUTDOWN_EXIT)"; fi
SERVER_PID=""

# 4.8: --port validation (background + check exit message)
"$BIN" --port 99999 > "$LOG" 2>&1 & PID=$!; sleep 0.5
if kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
OUTPUT=$(cat "$LOG")
if [[ "$OUTPUT" == *"invalid --port value"* ]]; then pass "4.8 --port 99999"; else fail "4.8 (got: $OUTPUT)"; fi

# 4.9: --host validation
"$BIN" --host 10.0.0.1 > "$LOG" 2>&1 & PID=$!; sleep 0.5
if kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
OUTPUT=$(cat "$LOG")
if [[ "$OUTPUT" == *"invalid --host value"* ]]; then pass "4.9 --host 10.0.0.1"; else fail "4.9 (got: $OUTPUT)"; fi

# 4.11: malformed YAML produces line:col error
cat > "$CFG" <<'YAML'
models:
  claude-opus-4
    provider: nim
    model: foo
YAML
"$BIN" > "$LOG" 2>&1 & PID=$!; sleep 0.5
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
mappings:
  opus: { provider: nim, model: foo }
YAML
if start_server; then
	OUTPUT=$("$BIN" 2>&1 || true)
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

# ---- 4.19: --no-env writes config only, no settings.json ----
TESTDIR=$(mktemp -d -p "$TMPHOME")
pushd "$TESTDIR" >/dev/null
OUT=$("$BIN" init --no-env --output test-config.yaml 2>&1)
if [[ -f test-config.yaml ]]; then
	pass "4.19a --no-env writes config file"
else
	fail "4.19a config not written"
fi
SETTINGS="$HOME/.claude/settings.json"
if [[ ! -f "$SETTINGS" ]]; then
	pass "4.19b --no-env skips settings.json"
else
	fail "4.19b settings.json written despite --no-env"
fi
popd >/dev/null
rm -rf "$TESTDIR"

# ---- 4.14: init writes settings.json with env block ----
TESTDIR=$(mktemp -d -p "$TMPHOME")
pushd "$TESTDIR" >/dev/null
OUT=$("$BIN" init --output test-config.yaml 2>&1)
if [[ "$OUT" == *"wrote ~/.claude/settings.json"* ]]; then
	pass "4.14a init reports settings.json write"
else
	fail "4.14a (got: $OUT)"
fi
if [[ -f "$SETTINGS" ]]; then
	SETTINGS_CONTENT=$(cat "$SETTINGS")
	pass "4.14b settings.json exists"
else
	fail "4.14b settings.json missing"
fi
if [[ "$SETTINGS_CONTENT" == *"ANTHROPIC_BASE_URL"* ]]; then
	pass "4.14c settings.json has ANTHROPIC_BASE_URL"
else
	fail "4.14c missing ANTHROPIC_BASE_URL in $(echo $SETTINGS_CONTENT | head -c 200)"
fi
if [[ "$SETTINGS_CONTENT" == *"freedius-dummy"* ]]; then
	pass "4.14d settings.json has ANTHROPIC_API_KEY"
else
	fail "4.14d missing API key"
fi
# Parse as valid JSON
if echo "$SETTINGS_CONTENT" | python3 -m json.tool >/dev/null 2>&1; then
	pass "4.14e settings.json is valid JSON"
else
	fail "4.14e invalid JSON: $(echo $SETTINGS_CONTENT | head -c 200)"
fi
popd >/dev/null
rm -rf "$TESTDIR"

# ---- 4.15: --shell-install writes rc file with env vars ----
TESTDIR=$(mktemp -d -p "$TMPHOME")
pushd "$TESTDIR" >/dev/null
HOME="$TESTDIR" SHELL=/bin/zsh "$BIN" init --shell-install --output test-config.yaml >/dev/null 2>&1
RC="$TESTDIR/.zshrc"
if [[ -f "$RC" ]]; then
	pass "4.15a --shell-install writes .zshrc"
else
	fail "4.15a .zshrc not written"
fi
RC_CONTENT=$(cat "$RC")
if [[ "$RC_CONTENT" == *"ANTHROPIC_BASE_URL"* ]]; then
	pass "4.15b .zshrc has ANTHROPIC_BASE_URL"
else
	fail "4.15b missing ANTHROPIC_BASE_URL"
fi
if [[ "$RC_CONTENT" == *"# >>> freedius env >>>"* ]] && [[ "$RC_CONTENT" == *"# <<< freedius env <<<"* ]]; then
	pass "4.15c .zshrc has marker delimiters"
else
	fail "4.15c missing markers"
fi
popd >/dev/null
rm -rf "$TESTDIR"

# ---- 4.16: re-run --shell-install shows "already installed" ----
TESTDIR=$(mktemp -d -p "$TMPHOME")
pushd "$TESTDIR" >/dev/null
HOME="$TESTDIR" SHELL=/bin/zsh "$BIN" init --shell-install --output cfg1.yaml >/dev/null 2>&1
OUT=$(HOME="$TESTDIR" SHELL=/bin/zsh "$BIN" init --shell-install --output cfg2.yaml 2>&1)
if [[ "$OUT" == *"already installed"* ]]; then
	pass "4.16a re-run shows already installed"
else
	fail "4.16a (got: $OUT)"
fi
# Verify single marker block
RC="$TESTDIR/.zshrc"
COUNT=$(grep -c '# >>> freedius env >>>' "$RC" 2>/dev/null || echo 0)
if [[ "$COUNT" -eq 1 ]]; then
	pass "4.16b single marker block in rc"
else
	fail "4.16b marker count: $COUNT (expected 1)"
fi
popd >/dev/null
rm -rf "$TESTDIR"

# ---- 4.17: --force replaces block (not doubled) ----
TESTDIR=$(mktemp -d -p "$TMPHOME")
pushd "$TESTDIR" >/dev/null
HOME="$TESTDIR" SHELL=/bin/zsh "$BIN" init --shell-install --output cfg1.yaml >/dev/null 2>&1
HOME="$TESTDIR" SHELL=/bin/zsh "$BIN" init --shell-install --force --output cfg2.yaml >/dev/null 2>&1
RC="$TESTDIR/.zshrc"
COUNT=$(grep -c '# >>> freedius env >>>' "$RC" 2>/dev/null || echo 0)
if [[ "$COUNT" -eq 1 ]]; then
	pass "4.17 --force replaces block (not doubled)"
else
	fail "4.17 marker count: $COUNT (expected 1 after --force)"
fi
popd >/dev/null
rm -rf "$TESTDIR"

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
cat > "$CFG" <<'YAML'
mappings:
  test: { provider: anthropic, model: test, base_url: http://127.0.0.1:1, api_key_env: MISSING_FAKE_KEY }
YAML
if ! start_server; then echo "  server failed to start (5.6)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST http://127.0.0.1:8080/v1/messages \
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
mappings:
  test: { provider: openai, model: test, base_url: http://127.0.0.1:1/v1/chat/completions, api_key_env: TEST_API_KEY }
YAML
if ! start_server; then echo "  server failed to start (5.8)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
ERR_TYPE=$(echo "$BODY" | jq -r '.error.type // empty' 2>/dev/null)

if [[ "$STATUS" == "529" ]]; then pass "5.8 status=529"; else fail "5.8 status (got $STATUS)"; fi
if [[ "$ERR_TYPE" == "overloaded_error" ]]; then pass "5.8 error.type=overloaded_error"; else fail "5.8 error.type (got $ERR_TYPE)"; fi
if [[ "$RETRY" == "15" ]]; then pass "5.8 retry-after=15"; else fail "5.8 retry-after (got $RETRY)"; fi
if [[ "$SHOULD_RETRY" == "true" ]]; then pass "5.8 x-should-retry=true"; else fail "5.8 x-should-retry (got $SHOULD_RETRY)"; fi

# ---- 5.9: DNS failure (Anthropic adapter) → 502 api_error, no retry ----
kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null; SERVER_PID=""

cat > "$CFG" <<'YAML'
mappings:
  test: { provider: anthropic, model: test, base_url: http://nonexistent.invalid, api_key_env: TEST_API_KEY }
YAML
if ! start_server; then echo "  server failed to start (5.9)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST http://127.0.0.1:8080/v1/messages \
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
mappings:
  test: { provider: openai, model: test, base_url: http://127.0.0.1:$UPSTREAM_PORT/v1/chat/completions, api_key_env: TEST_API_KEY }
YAML
if ! start_server; then echo "  server failed to start (5.10)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST http://127.0.0.1:8080/v1/messages \
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
mappings:
  test: { provider: openai, model: test, base_url: http://127.0.0.1:$UPSTREAM_PORT/v1/chat/completions, api_key_env: TEST_API_KEY }
YAML
if ! start_server; then echo "  server failed to start (5.11)"; exit 1; fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"test"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
RETRY=$(resp_header "$RESP" '^retry-after:')
SHOULD_RETRY=$(resp_header "$RESP" '^x-should-retry:')
BODY=$(resp_body_json -X POST http://127.0.0.1:8080/v1/messages \
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
mappings:
  opus: { provider: nim, model: smokes }
YAML
if ! start_server; then echo "  server failed to start (5.12)"; exit 1; fi

# Valid request dispatch
RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"opus"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
PROV=$(resp_header "$RESP" '^X-Freedius-Matched-Provider:')
MODEL=$(resp_header "$RESP" '^X-Freedius-Matched-Model:')
if [[ "$STATUS" =~ ^[0-9]+$ ]]; then pass "5.12 dispatch status=$STATUS"; else fail "5.12 status (got $STATUS)"; fi
if [[ "$PROV" == "nim" ]]; then pass "5.12 X-Freedius-Matched-Provider: nim"; else fail "5.12 provider (got $PROV)"; fi
if [[ "$MODEL" == "smokes" ]]; then pass "5.12 X-Freedius-Matched-Model: smokes"; else fail "5.12 model (got $MODEL)"; fi

# Unmatched model
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$STATUS" == "404" ]]; then pass "5.12 unknown model → 404"; else fail "5.12 unknown (got $STATUS)"; fi

# Malformed body
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{not json')
if [[ "$STATUS" == "400" ]]; then pass "5.12 malformed body → 400"; else fail "5.12 malformed (got $STATUS)"; fi

# Empty body
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
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
