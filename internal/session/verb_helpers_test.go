package session

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Helpers the verb tests share.

func sendJSON(t *testing.T, c *verbConn, id int, params map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return c.call(t, fmt.Sprintf(`{"id":%d,"verb":"send-agent-message","params":%s}`, id, raw))
}

// mustRefuse fails with an ASSERTION when resp is not an error with this code.
func mustRefuse(t *testing.T, resp map[string]any, code, why string) {
	t.Helper()
	e, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("ASSERTION: %s: %v", why, resp)
	}
	if got, _ := e["code"].(string); got != code {
		t.Fatalf("ASSERTION: %s: refused with %s, want %s", why, got, code)
	}
}

func readAll(t *testing.T, c *verbConn, session string) []map[string]any {
	t.Helper()
	res := result(t, c.call(t, fmt.Sprintf(`{"id":99,"verb":"read-agent-messages","params":{"session":%q,"peek":true,"limit":256}}`, session)))
	msgs, _ := res["messages"].([]any)
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.(map[string]any))
	}
	return out
}

// mustJSON marshals a value that cannot fail to marshal.
func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return raw
}
