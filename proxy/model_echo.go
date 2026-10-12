package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// rewriteTopLevelModel returns body with its top-level "model" field set to
// model, so the client sees the model name it asked for rather than the
// provider-specific name freedius remapped it to. Bodies that are not a JSON
// object, or that carry no top-level "model", are returned unchanged.
//
// Decoding uses json.Decoder.UseNumber so integer fields are re-emitted
// verbatim instead of being rounded through float64.
func rewriteTopLevelModel(body []byte, model string) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return body
	}
	if _, ok := obj["model"]; !ok {
		return body
	}
	obj["model"] = model
	rewritten, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return rewritten
}

// forwardSSEEchoingModel copies an upstream event stream to w verbatim,
// rewriting the top-level "model" of every JSON data payload to model.
// Non-data lines (event:, id:, comments), blank delimiters and the [DONE]
// sentinel are passed through untouched, and payloads that are not JSON
// objects (or carry no model) are forwarded as received.
func forwardSSEEchoingModel(
	w io.Writer,
	br *bufio.Reader,
	model string,
	flush func() error,
) error {
	for {
		lines, err := readAnthropicSSEEvent(br)
		if len(lines) > 0 {
			for _, line := range rewriteSSEEventModel(lines, model) {
				if _, writeErr := w.Write(line); writeErr != nil {
					return writeErr
				}
			}
			if flushErr := flush(); flushErr != nil {
				return flushErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// rewriteSSEEventModel returns one SSE event's lines with the top-level model
// of its data payload replaced. The lines are returned unchanged when there is
// nothing to rewrite. Multi-line data payloads are joined into a single data
// line, as the SSE format requires before JSON parsing.
func rewriteSSEEventModel(lines [][]byte, model string) [][]byte {
	payload := anthropicSSEDataPayload(lines)
	if len(payload) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
		return lines
	}
	rewritten := rewriteTopLevelModel(payload, model)
	if bytes.Equal(rewritten, payload) {
		return lines
	}

	out := make([][]byte, 0, len(lines))
	dataWritten := false
	for _, line := range lines {
		trimmed := bytes.TrimRight(line, "\r\n")
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			if dataWritten {
				continue
			}
			var buf bytes.Buffer
			buf.WriteString("data: ")
			buf.Write(rewritten)
			buf.Write(anthropicSSELineEnding(line))
			out = append(out, buf.Bytes())
			dataWritten = true
			continue
		}
		out = append(out, line)
	}
	return out
}
