#!/usr/bin/env bash
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BIN="$SCRIPT_DIR/freedius"
LOG="$SCRIPT_DIR/freedius-test.log"
CFG="$SCRIPT_DIR/freedius.yaml"
MOCK_PIDS=()

TMPHOME=$(mktemp -d)
ORIG_GOMODCACHE=$(go env GOMODCACHE)
ORIG_GOCACHE=$(go env GOCACHE)
export HOME="$TMPHOME"
export XDG_CONFIG_HOME="$TMPHOME/.config"
export GOMODCACHE="$ORIG_GOMODCACHE"
export GOCACHE="$ORIG_GOCACHE"

cleanup() {
	rm -f "$CFG" "$LOG"
	for pid in "${MOCK_PIDS[@]}"; do
		if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
			kill -TERM "$pid" 2>/dev/null
			wait "$pid" 2>/dev/null
		fi
	done
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

stop_server() {
	if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
		kill -TERM "$SERVER_PID" 2>/dev/null
		wait "$SERVER_PID" 2>/dev/null
	fi
	SERVER_PID=""
}

wait_for_port() {
	local port=$1
	for _ in $(seq 1 30); do
		if curl -sS -o /dev/null "http://127.0.0.1:${port}/" 2>/dev/null; then
			return 0
		fi
		sleep 0.1
	done
	return 1
}

start_mock_custom() {
	local port=$1
	cat >"$TMPHOME/mock_custom.py" <<'PYEOF'
import http.server, json, sys
PORT = int(sys.argv[1])
class H(http.server.BaseHTTPRequestHandler):
	def do_POST(self):
		n = int(self.headers.get('Content-Length', '0'))
		body = self.rfile.read(n)
		sys.stderr.write(f"mock_custom received: {body[:200]}\n")
		sys.stderr.flush()
		auth = self.headers.get('Authorization', '')
		if 'sk-test-custom' not in auth:
			self.send_response(401)
			self.send_header('Content-Type', 'application/json')
			self.end_headers()
			self.wfile.write(b'{"error":"unauthorized"}')
			return
		ct = self.headers.get('Content-Type', '')
		if 'text/event-stream' in self.headers.get('Accept', ''):
			resp = (
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_custom\",\"model\":\"mock\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello from mock\"}}\n\n"
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n"
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			).encode()
			self.send_response(200)
			self.send_header('Content-Type', 'text/event-stream')
			self.send_header('Cache-Control', 'no-cache')
			self.send_header('Connection', 'keep-alive')
			self.send_header('X-Mock-Custom', 'served')
			self.send_header('Content-Length', str(len(resp)))
			self.end_headers()
			self.wfile.write(resp)
		else:
			body_json = json.loads(body)
			resp = json.dumps({
				"id": "msg_custom",
				"type": "message",
				"role": "assistant",
				"model": body_json.get("model", "mock"),
				"content": [{"type": "text", "text": "hello from mock"}],
				"stop_reason": "end_turn",
				"usage": {"input_tokens": 1, "output_tokens": 2},
			}).encode()
			self.send_response(200)
			self.send_header('Content-Type', 'application/json')
			self.send_header('X-Mock-Custom', 'served')
			self.send_header('Content-Length', str(len(resp)))
			self.end_headers()
			self.wfile.write(resp)
	def log_message(self, *a, **k): pass
http.server.HTTPServer(('127.0.0.1', PORT), H).serve_forever()
PYEOF
	python3 "$TMPHOME/mock_custom.py" "$port" >"$TMPHOME/mock_custom.log" 2>&1 &
	MOCK_PIDS+=($!)
	wait_for_port "$port"
}

start_mock_nim() {
	local port=$1
	cat >"$TMPHOME/mock_nim.py" <<'PYEOF'
import http.server, json, sys
PORT = int(sys.argv[1])
class H(http.server.BaseHTTPRequestHandler):
	def do_POST(self):
		n = int(self.headers.get('Content-Length', '0'))
		body = self.rfile.read(n)
		sys.stderr.write(f"mock_nim received: {body[:200]}\n")
		sys.stderr.flush()
		auth = self.headers.get('Authorization', '')
		if 'sk-test-nim' not in auth:
			self.send_response(401)
			self.send_header('Content-Type', 'application/json')
			self.end_headers()
			self.wfile.write(b'{"error":{"message":"invalid api key"}}')
			return
		req = json.loads(body)
		has_tools = bool(req.get('tools'))
		if 'text/event-stream' in self.headers.get('Accept', ''):
			chunks = [
				{"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":None}]},
				{"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{"content":"hello from nim mock"},"finish_reason":None}]},
			]
			if has_tools:
				chunks.append({"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_mock","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":None}]})
				chunks.append({"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\""}}]},"finish_reason":None}]})
				chunks.append({"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"Paris\"}"}}]},"finish_reason":None}]})
				chunks.append({"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]})
			else:
				chunks.append({"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]})
			chunks.append({"id":"chatcmpl-mock","object":"chat.completion.chunk","created":1,"model":req.get("model","mock"),"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}})
			body_bytes = b""
			for c in chunks:
				body_bytes += b"data: " + json.dumps(c).encode() + b"\n\n"
			body_bytes += b"data: [DONE]\n\n"
			self.send_response(200)
			self.send_header('Content-Type', 'text/event-stream')
			self.send_header('Content-Length', str(len(body_bytes)))
			self.end_headers()
			self.wfile.write(body_bytes)
		else:
			resp = {
				"id":"chatcmpl-mock","object":"chat.completion","created":1,"model":req.get("model","mock"),
				"choices":[{"index":0,"message":{"role":"assistant","content":"hello from nim mock"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}
			}
			body_bytes = json.dumps(resp).encode()
			self.send_response(200)
			self.send_header('Content-Type', 'application/json')
			self.send_header('Content-Length', str(len(body_bytes)))
			self.end_headers()
			self.wfile.write(body_bytes)
	def log_message(self, *a, **k): pass
http.server.HTTPServer(('127.0.0.1', PORT), H).serve_forever()
PYEOF
	python3 "$TMPHOME/mock_nim.py" "$port" >"$TMPHOME/mock_nim.log" 2>&1 &
	MOCK_PIDS+=($!)
	wait_for_port "$port"
}

start_server_with_config() {
	local cfg_content=$1
	printf '%s' "$cfg_content" > "$CFG"
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

stop_mock() {
	for i in "${!MOCK_PIDS[@]}"; do
		local pid=${MOCK_PIDS[$i]}
		if kill -0 "$pid" 2>/dev/null; then
			kill -TERM "$pid" 2>/dev/null
			wait "$pid" 2>/dev/null
		fi
		unset 'MOCK_PIDS[$i]'
	done
}

echo ""
echo "=== F-01: foundation ==="

if ! start_server; then
	echo "  server failed to start"
	exit 1
fi

RESP=$(curl -sS -D - -o /dev/null -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(curl -sS -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
if [[ "$STATUS" == "404" ]]; then pass "F01 unknown model status=404"; else fail "F01 unknown status (got $STATUS)"; fi
if [[ "$BODY" == *'"status":"no_match"'* ]]; then pass "F01 unknown body has status:no_match"; else fail "F01 unknown body (got $BODY)"; fi

STATUS=$(curl -sS -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{not json')
if [[ "$STATUS" == "400" ]]; then pass "F01 malformed body status=400"; else fail "F01 malformed status (got $STATUS)"; fi

LOG_LINES=$(wc -l < "$LOG")
if [[ "$LOG_LINES" == "1" ]]; then pass "F01 single log line (listening only)"; else fail "F01 log lines: $LOG_LINES (expected 1)"; fi

stop_server

OUTPUT=$("$BIN" --port 99999 2>&1 || true)
if [[ "$OUTPUT" == *"invalid --port value: 99999"* ]]; then pass "F01 --port 99999"; else fail "F01 --port (got: $OUTPUT)"; fi

OUTPUT=$("$BIN" --host 10.0.0.1 2>&1 || true)
if [[ "$OUTPUT" == *"invalid --host value: 10.0.0.1"* ]]; then pass "F01 --host 10.0.0.1"; else fail "F01 --host (got: $OUTPUT)"; fi

cat > "$CFG" <<'YAML'
models:
  claude-opus-4:
    provider: nim
   model: foo
YAML
OUTPUT=$("$BIN" 2>&1 || true)
rm -f "$CFG"
if [[ "$OUTPUT" == *"["*"]"* ]]; then
	pass "F01 malformed YAML produces line:col error"
else
	fail "F01 malformed YAML (got: $OUTPUT)"
fi

OUTPUT=$("$BIN" 2>&1 || true)
if [[ "$OUTPUT" == *"config file not found"* ]]; then pass "F01 no config"; else fail "F01 no config (got: $OUTPUT)"; fi

if start_server; then
	OUTPUT=$("$BIN" 2>&1 || true)
	if [[ "$OUTPUT" == *"bind: address already in use"* ]]; then
		pass "F01 port conflict"
	else
		fail "F01 port conflict (got: $OUTPUT)"
	fi
	stop_server
else
	fail "F01 could not start first instance"
fi

echo ""
echo "=== Phase 1: schema + provider registry ==="

if ! start_server; then
	echo "  server failed to start (Phase 1)"
	exit 1
fi

RESP=$(curl -sS -D - -o /tmp/p1_body http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
HEADER_PROV=$(printf '%s\n' "$RESP" | grep -i '^X-Freedius-Matched-Provider:' | tr -d '\r' | awk '{print $2}')
HEADER_MOD=$(printf '%s\n' "$RESP" | grep -i '^X-Freedius-Matched-Model:' | tr -d '\r' | awk '{print $2}')
BODY=$(cat /tmp/p1_body)
if [[ "$STATUS" == "500" ]]; then pass "P1.1 known model status=500 (no adapter)"; else fail "P1.1 status (got $STATUS)"; fi
if [[ "$HEADER_PROV" == "nim" ]]; then pass "P1.1 X-Freedius-Matched-Provider: nim"; else fail "P1.1 header prov (got $HEADER_PROV)"; fi
if [[ "$HEADER_MOD" == "meta/llama-3.1-70b-instruct" ]]; then pass "P1.1 X-Freedius-Matched-Model set"; else fail "P1.1 header mod (got $HEADER_MOD)"; fi
if [[ "$BODY" == *'"error":"provider not registered: nim"'* ]]; then pass "P1.1 body has 'provider not registered: nim'"; else fail "P1.1 body (got $BODY)"; fi

RESP=$(curl -sS -D - -o /tmp/p1_body2 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-sonnet-4"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p1_body2)
if [[ "$STATUS" == "500" ]]; then pass "P1.2 custom-mapped model status=500 (no adapter)"; else fail "P1.2 status (got $STATUS)"; fi
if [[ "$BODY" == *'"error":"provider not registered: custom"'* ]]; then pass "P1.2 body has 'provider not registered: custom'"; else fail "P1.2 body (got $BODY)"; fi

RESP=$(curl -sS -D - -o /tmp/p1_body3 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"unknown"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
if [[ "$STATUS" == "404" ]]; then pass "P1.3 unknown model still returns 404"; else fail "P1.3 status (got $STATUS)"; fi

stop_server

cat > "$CFG" <<'YAML'
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
YAML
OUTPUT=$("$BIN" 2>&1 || true)
rm -f "$CFG"
if [[ "$OUTPUT" == *"provider=custom but no base_url"* ]]; then pass "P1.4 config rejects custom without base_url"; else fail "P1.4 (got: $OUTPUT)"; fi

cat > "$CFG" <<'YAML'
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: ftp://example.com/v1/messages
YAML
OUTPUT=$("$BIN" 2>&1 || true)
rm -f "$CFG"
if [[ "$OUTPUT" == *"invalid scheme \"ftp\""* ]]; then pass "P1.5 config rejects non-http(s) base_url"; else fail "P1.5 (got: $OUTPUT)"; fi

cat > "$CFG" <<'YAML'
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: https://example.com/v1/messages
    api_key_env: "FOO=BAR"
YAML
OUTPUT=$("$BIN" 2>&1 || true)
rm -f "$CFG"
if [[ "$OUTPUT" == *"unsafe api_key_env"* ]]; then pass "P1.6 config rejects api_key_env with ="; else fail "P1.6 (got: $OUTPUT)"; fi

echo ""
if [[ $FAIL -eq 0 ]]; then
	echo "All automated checks passed (F-01 + Phase 1)"
else
	echo "$FAIL checks failed"
	exit 1
fi
