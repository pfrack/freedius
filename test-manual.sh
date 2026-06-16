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
	cp config.example.yaml "$CFG"
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

echo ""
echo "=== Phase 4: end-to-end ==="

if ! start_server; then
	echo "  server failed to start"
	exit 1
fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
HEADER=$(printf '%s\n' "$RESP" | grep -i '^X-Freedius-Matched-Provider:' | tr -d '\r' | awk '{print $2}')
if [[ "$STATUS" == "501" ]]; then pass "4.5 known model status=501"; else fail "4.5 status (got $STATUS)"; fi
if [[ "$HEADER" == "nim" ]]; then pass "4.5 X-Freedius-Matched-Provider: nim"; else fail "4.5 header (got $HEADER)"; fi

BODY=$(curl -sS -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$STATUS" == "404" ]]; then pass "4.6 unknown model status=404"; else fail "4.6 status (got $STATUS)"; fi
if [[ "$BODY" == *'"status":"no_match"'* ]]; then pass "4.6 body has status:no_match"; else fail "4.6 body (got $BODY)"; fi

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

OUTPUT=$("$BIN" --port 99999 2>&1 || true)
if [[ "$OUTPUT" == *"invalid --port value: 99999"* ]]; then pass "4.8 --port 99999"; else fail "4.8 (got: $OUTPUT)"; fi

OUTPUT=$("$BIN" --host 10.0.0.1 2>&1 || true)
if [[ "$OUTPUT" == *"invalid --host value: 10.0.0.1"* ]]; then pass "4.9 --host 10.0.0.1"; else fail "4.9 (got: $OUTPUT)"; fi

cat > "$CFG" <<'YAML'
models:
  claude-opus-4:
    provider: nim
   model: foo
YAML
OUTPUT=$("$BIN" 2>&1 || true)
rm -f "$CFG"
if [[ "$OUTPUT" == *"["*"]"* ]]; then
	pass "4.11 malformed YAML produces line:col error"
else
	fail "4.11 (got: $OUTPUT)"
fi

OUTPUT=$("$BIN" 2>&1 || true)
if [[ "$OUTPUT" == *"config file not found"* ]]; then pass "4.10 no config"; else fail "4.10 (got: $OUTPUT)"; fi

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
echo "=== Manual eyeball checks (not automatable) ==="
echo "  1.7  Inspect .github/workflows/ci.yml for YAML correctness"
echo "  1.8  Inspect config.example.yaml for schema"

echo ""
if [[ $FAIL -eq 0 ]]; then
	echo "All automated checks passed"
	exit 0
else
	echo "$FAIL checks failed"
	exit 1
fi
