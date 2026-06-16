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
export NIM_API_KEY="${NIM_API_KEY:-sk-test-nim}"

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

SAVED_NIM_KEY="$NIM_API_KEY"
unset NIM_API_KEY
cat > "$CFG" <<'YAML'
models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    api_key_env: NIM_API_KEY
YAML
OUTPUT=$("$BIN" 2>&1 || true)
if [[ "$OUTPUT" == *"NIM_API_KEY env var required"* ]]; then
	pass "F01 eager NIM_API_KEY check (config has nim, env unset)"
else
	fail "F01 eager NIM_API_KEY check (got: $OUTPUT)"
fi
export NIM_API_KEY="$SAVED_NIM_KEY"

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

cat > "$CFG" <<'YAML'
models:
  claude-opus-4:
    provider: zen
    model: zen-large
YAML
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p1_body http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-opus-4"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
HEADER_PROV=$(printf '%s\n' "$RESP" | grep -i '^X-Freedius-Matched-Provider:' | tr -d '\r' | awk '{print $2}')
HEADER_MOD=$(printf '%s\n' "$RESP" | grep -i '^X-Freedius-Matched-Model:' | tr -d '\r' | awk '{print $2}')
BODY=$(cat /tmp/p1_body)
if [[ "$STATUS" == "500" ]]; then pass "P1.1 zen model status=500 (in KnownProviders but no adapter)"; else fail "P1.1 status (got $STATUS)"; fi
if [[ "$HEADER_PROV" == "zen" ]]; then pass "P1.1 X-Freedius-Matched-Provider: zen"; else fail "P1.1 header prov (got $HEADER_PROV)"; fi
if [[ "$HEADER_MOD" == "zen-large" ]]; then pass "P1.1 X-Freedius-Matched-Model set"; else fail "P1.1 header mod (got $HEADER_MOD)"; fi
if [[ "$BODY" == *'"error":"provider not registered: zen"'* ]]; then pass "P1.1 body has 'provider not registered: zen'"; else fail "P1.1 body (got $BODY)"; fi

stop_server

cat > "$CFG" <<'YAML'
models:
  claude-sonnet-4:
    provider: go
    model: go-large
YAML
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p1_body2 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -d '{"model":"claude-sonnet-4"}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p1_body2)
if [[ "$STATUS" == "500" ]]; then pass "P1.2 go provider status=500 (in KnownProviders but no adapter)"; else fail "P1.2 status (got $STATUS)"; fi
if [[ "$BODY" == *'"error":"provider not registered: go"'* ]]; then pass "P1.2 body has 'provider not registered: go'"; else fail "P1.2 body (got $BODY)"; fi

stop_server

cat > "$CFG" <<'YAML'
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
YAML
start_server
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
echo "=== Phase 2: custom passthrough adapter ==="

CUSTOM_PORT=9091
start_mock_custom "$CUSTOM_PORT"

cat > "$CFG" <<YAML
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: http://127.0.0.1:${CUSTOM_PORT}/v1/messages
    api_key_env: MY_SHIM_API_KEY
YAML
export MY_SHIM_API_KEY="sk-test-custom"
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p2_body http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-sonnet-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
HEADER=$(printf '%s\n' "$RESP" | grep -i '^X-Mock-Custom:' | tr -d '\r' | awk '{print $2}')
BODY=$(cat /tmp/p2_body)
if [[ "$STATUS" == "200" ]]; then pass "P2.1 custom adapter non-stream status=200"; else fail "P2.1 status (got $STATUS, body: $BODY)"; fi
if [[ "$HEADER" == "served" ]]; then pass "P2.1 upstream header X-Mock-Custom forwarded"; else fail "P2.1 header (got $HEADER)"; fi
if [[ "$BODY" == *'"role": "assistant"'* ]] && [[ "$BODY" == *'"id": "msg_custom"'* ]]; then pass "P2.1 body is Anthropic-format JSON"; else fail "P2.1 body shape (got $BODY)"; fi

RESP=$(curl -sS -D - -o /tmp/p2_sse http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -H 'accept: text/event-stream' \
	-d '{"model":"claude-sonnet-4","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
CT=$(printf '%s\n' "$RESP" | grep -i '^Content-Type:' | tr -d '\r' | awk '{print $2}')
SSE=$(cat /tmp/p2_sse)
if [[ "$STATUS" == "200" ]]; then pass "P2.2 custom adapter streaming status=200"; else fail "P2.2 status (got $STATUS)"; fi
if [[ "$CT" == "text/event-stream" ]]; then pass "P2.2 Content-Type text/event-stream"; else fail "P2.2 CT (got $CT)"; fi
if [[ "$SSE" == *"event: message_start"* ]] && [[ "$SSE" == *"event: content_block_delta"* ]] && [[ "$SSE" == *"event: message_stop"* ]]; then
	pass "P2.2 SSE events: message_start, content_block_delta, message_stop"
else
	fail "P2.2 SSE events missing (got: $SSE)"
fi

stop_server
stop_mock
unset MY_SHIM_API_KEY

cat > "$CFG" <<YAML
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: http://127.0.0.1:${CUSTOM_PORT}/v1/messages
    api_key_env: MY_SHIM_API_KEY
YAML
start_mock_custom "$CUSTOM_PORT"
export MY_SHIM_API_KEY="sk-wrong-key-fails-mock-auth"
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p2_401 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-sonnet-4","messages":[]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p2_401)
if [[ "$STATUS" == "401" ]]; then pass "P2.3 upstream 401 forwarded verbatim"; else fail "P2.3 status (got $STATUS)"; fi
if [[ "$BODY" == *'"unauthorized"'* ]]; then pass "P2.3 upstream body forwarded verbatim"; else fail "P2.3 body (got $BODY)"; fi

stop_server
stop_mock
unset MY_SHIM_API_KEY

cat > "$CFG" <<YAML
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: http://127.0.0.1:1/v1/messages
    api_key_env: MY_SHIM_API_KEY
YAML
export MY_SHIM_API_KEY="sk-test-custom"
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p2_502 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-sonnet-4","messages":[]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p2_502)
if [[ "$STATUS" == "502" ]]; then pass "P2.4 unreachable upstream returns 502"; else fail "P2.4 status (got $STATUS)"; fi
if [[ "$BODY" == *"upstream_unreachable"* ]]; then pass "P2.4 body has upstream_unreachable"; else fail "P2.4 body (got $BODY)"; fi

stop_server
unset MY_SHIM_API_KEY

cat > "$CFG" <<YAML
models:
  claude-sonnet-4:
    provider: custom
    model: my-sonnet-shim
    base_url: http://127.0.0.1:1/v1/messages
    api_key_env: MY_SHIM_API_KEY
YAML
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p2_noenv http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-sonnet-4","messages":[]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p2_noenv)
if [[ "$STATUS" == "502" ]]; then pass "P2.5 missing env var returns 502"; else fail "P2.5 status (got $STATUS)"; fi
if [[ "$BODY" == *"upstream error"* ]] || [[ "$BODY" == *"MY_SHIM_API_KEY"* ]]; then
	pass "P2.5 body mentions env var / upstream error"
else
	fail "P2.5 body (got $BODY)"
fi

stop_server
rm -f "$CFG"

echo ""
echo "=== Phase 3: NIM adapter + translation module ==="

NIM_PORT=9092
start_mock_nim "$NIM_PORT"

cat > "$CFG" <<YAML
models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    api_key_env: NIM_API_KEY
YAML
export NIM_API_KEY="sk-test-nim"
export NIM_BASE_URL="http://127.0.0.1:${NIM_PORT}"
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p3_sse http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -H 'accept: text/event-stream' \
	-d '{"model":"claude-opus-4","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"Say hi in 5 words or fewer"}]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
CT=$(printf '%s\n' "$RESP" | grep -i '^Content-Type:' | tr -d '\r' | awk '{print $2}')
SSE=$(cat /tmp/p3_sse)
if [[ "$STATUS" == "200" ]]; then pass "P3.1 NIM streaming status=200"; else fail "P3.1 status (got $STATUS)"; fi
if [[ "$CT" == "text/event-stream" ]]; then pass "P3.1 Content-Type text/event-stream"; else fail "P3.1 CT (got $CT)"; fi
for ev in "event: message_start" "event: content_block_start" "event: content_block_delta" "event: content_block_stop" "event: message_delta" "event: message_stop"; do
	if [[ "$SSE" == *"$ev"* ]]; then
		pass "P3.1 SSE event present: $ev"
	else
		fail "P3.1 SSE event missing: $ev"
	fi
done
if [[ "$SSE" == *'"text_delta"'* ]]; then pass "P3.1 SSE has text_delta"; else fail "P3.1 no text_delta: $SSE"; fi
if [[ "$SSE" == *'"stop_reason":"end_turn"'* ]]; then pass "P3.1 stop_reason=end_turn"; else fail "P3.1 stop_reason: $SSE"; fi
if [[ "$SSE" == *"\n\n\n"* ]]; then fail "P3.1 output contains \\n\\n\\n (json.Encoder trap)"; else pass "P3.1 no triple-newline corruption"; fi

RESP=$(curl -sS -D - -o /tmp/p3_tool http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' -H 'accept: text/event-stream' \
	-d '{"model":"claude-opus-4","max_tokens":100,"stream":true,"tools":[{"name":"get_weather","description":"Get weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],"messages":[{"role":"user","content":"weather in Paris?"}]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
TOOL=$(cat /tmp/p3_tool)
if [[ "$STATUS" == "200" ]]; then pass "P3.2 NIM with tools status=200"; else fail "P3.2 status (got $STATUS)"; fi
if [[ "$TOOL" == *'"type":"tool_use"'* ]]; then pass "P3.2 tool_use block emitted"; else fail "P3.2 no tool_use: $TOOL"; fi
if [[ "$TOOL" == *'"name":"get_weather"'* ]]; then pass "P3.2 tool name get_weather"; else fail "P3.2 tool name missing: $TOOL"; fi
if [[ "$TOOL" == *'"partial_json"'* ]]; then pass "P3.2 input_json_delta emitted"; else fail "P3.2 no partial_json: $TOOL"; fi
if [[ "$TOOL" == *'"stop_reason":"tool_use"'* ]]; then pass "P3.2 stop_reason=tool_use"; else fail "P3.2 stop_reason: $TOOL"; fi

stop_server
stop_mock
unset NIM_API_KEY
unset NIM_BASE_URL

cat > "$CFG" <<YAML
models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    api_key_env: NIM_API_KEY
YAML
export NIM_API_KEY="sk-wrong-key-mock-rejects"
export NIM_BASE_URL="http://127.0.0.1:${NIM_PORT}"
start_mock_nim "$NIM_PORT"
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p3_401 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p3_401)
if [[ "$STATUS" == "401" ]]; then pass "P3.3 NIM 401 forwarded verbatim"; else fail "P3.3 status (got $STATUS)"; fi
if [[ "$BODY" == *"invalid api key"* ]]; then pass "P3.3 NIM body forwarded"; else fail "P3.3 body: $BODY"; fi

stop_server
stop_mock
unset NIM_API_KEY
unset NIM_BASE_URL

cat > "$CFG" <<YAML
models:
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    api_key_env: NIM_API_KEY
YAML
export NIM_API_KEY="sk-test-nim"
export NIM_BASE_URL="http://127.0.0.1:1"
"$BIN" > "$LOG" 2>&1 &
SERVER_PID=$!
for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sS -o /dev/null http://127.0.0.1:8080/v1/messages 2>/dev/null; then
		break
	fi
	sleep 0.1
done

RESP=$(curl -sS -D - -o /tmp/p3_502 http://127.0.0.1:8080/v1/messages \
	-H 'content-type: application/json' \
	-d '{"model":"claude-opus-4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}')
STATUS=$(printf '%s\n' "$RESP" | head -1 | awk '{print $2}')
BODY=$(cat /tmp/p3_502)
if [[ "$STATUS" == "502" ]]; then pass "P3.4 NIM unreachable returns 502"; else fail "P3.4 status (got $STATUS)"; fi
if [[ "$BODY" == *"upstream error"* ]] || [[ "$BODY" == *"upstream_unreachable"* ]]; then
	pass "P3.4 body has upstream error indicator"
else
	fail "P3.4 body: $BODY"
fi

stop_server
rm -f "$CFG"
unset NIM_API_KEY
unset NIM_BASE_URL

echo ""
if [[ $FAIL -eq 0 ]]; then
	echo "All automated checks passed (F-01 + Phase 1 + Phase 2 + Phase 3)"
else
	echo "$FAIL checks failed"
	exit 1
fi
