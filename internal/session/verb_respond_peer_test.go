//go:build linux || darwin

package session

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// These respond tests run a helper process in a pane and read its peer pid
// off the socket, which the helpers in human_origin_test.go do on linux and
// darwin only.

// TestRespondFromShellIsAGrant: without the grant, a caller outside every pane
// with no attach is refused; with it, the same caller may answer.
func TestRespondFromShellIsAGrant(t *testing.T) {
	skipWithoutPeerPID(t)
	d, sp := startTestDaemon(t)
	ag := startBlockedAgent(t, d, sp, "grant", "claude-code", "approve")
	params := map[string]any{"session": "grant", "window": ag.window, "action": "approve"}

	c := dialVerb(t, sp)
	if code := errCode(t, callVerb(t, c, "respond", params)); code != ErrVerbNotHuman {
		t.Fatalf("without the grant: %s, want %s", code, ErrVerbNotHuman)
	}
	d.respondFromShell = true
	res := result(t, callVerb(t, c, "respond", params))
	if res["sent"] != "1" {
		t.Errorf("with the grant: %v", res)
	}
}

// TestAPaneCannotRespondWithACopiedNonce holds respond to the rule every act
// as the person is held to: an agent in a pane that got the person's live
// nonce still cannot approve a prompt, its own or another pane's, and the
// grant does not change that.
func TestAPaneCannotRespondWithACopiedNonce(t *testing.T) {
	skipWithoutPeerPID(t)
	d, sp := startTestDaemon(t)
	ag := startBlockedAgent(t, d, sp, "paneh", "claude-code", "approve")
	d.respondFromShell = true
	tui := attachTUI(t, sp, "paneh")
	out := filepath.Join(t.TempDir(), "out")
	req := `{"id":1,"verb":"respond","params":{"session":"paneh","window":"` + ag.window + `","action":"approve","human_nonce":"` + tui.HumanNonce() + `"}}`
	runInPane(t, d, ag.sess, ag.other, helperCommand(t, sp, out, "send", req))
	var resp map[string]any
	if err := json.Unmarshal([]byte(waitHelper(t, out)), &resp); err != nil {
		t.Fatal(err)
	}
	if code := errCode(t, resp); code != ErrVerbNotHuman {
		t.Fatalf("respond from a pane with the person's nonce: %s, want %s", code, ErrVerbNotHuman)
	}
	if got := ag.received(); got != "" {
		t.Fatalf("ASSERTION: a pane's respond reached the agent: %q", got)
	}
}
