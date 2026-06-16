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

cleanup() {
	rm -f "$CFG" "$LOG"
	if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
		kill -TERM "$SERVER_PID" 2>/dev/null
		wait "$SERVER_PID" 2>/dev/null
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

export NIM_API_KEY=test-dummy-key

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
if [[ "$BODY" == *'"status":"no_match"'* ]]; then pass "4.6 body has status:no_match"; else fail "4.6 body (got $BODY)"; fi

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

LOG_LINES=$(wc -l < "$LOG")
if [[ "$LOG_LINES" == "1" ]]; then pass "4.14 single log line (listening only)"; else fail "4.14 log lines: $LOG_LINES (expected 1)"; fi

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

# 4.10: no config
"$BIN" > "$LOG" 2>&1 & PID=$!; sleep 0.5
if kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
OUTPUT=$(cat "$LOG")
if [[ "$OUTPUT" == *"config file not found"* ]]; then pass "4.10 no config"; else fail "4.10 (got: $OUTPUT)"; fi

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
if [[ $FAIL -eq 0 ]]; then
	echo "All automated checks passed"
	exit 0
else
	echo "$FAIL checks failed"
	exit 1
fi
