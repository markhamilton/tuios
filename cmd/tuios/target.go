package main

import (
	"encoding/json"

	"github.com/Gaurav-Gosain/tuios/internal/session"
)

// verbTarget is a resolved address: the daemon to talk to, and the session
// and window to pass to it.
type verbTarget struct {
	client  *session.VerbClient
	session string
	window  string
}

// dialTarget connects to the daemon for a session flag and a window flag. The
// caller closes the result.
func dialTarget(sessionFlag, windowFlag string) (*verbTarget, error) {
	client, err := dialVerb()
	if err != nil {
		return nil, err
	}
	return &verbTarget{client: client, session: sessionFlag, window: windowFlag}, nil
}

// dialSessionTarget is dialTarget for a verb with no window.
func dialSessionTarget(sessionFlag string) (*verbTarget, error) {
	return dialTarget(sessionFlag, "")
}

// Close closes the connection.
func (t *verbTarget) Close() {
	if t != nil && t.client != nil {
		_ = t.client.Close()
	}
}

// params puts the resolved session, and the window when the verb has one,
// into a parameter map. The window is set only when the caller passed the
// key, because a verb without a window parameter must not be sent one.
//
//go:noinline
func (t *verbTarget) params(p map[string]any) map[string]any {
	if p == nil {
		p = map[string]any{}
	}
	p["session"] = t.session
	if _, has := p["window"]; has {
		p["window"] = t.window
	}
	return p
}

// explain is explainVerbError for a verb this target ran.
func (t *verbTarget) explain(verb string, err error) error {
	return explainVerbError(verb, err)
}

// printVerbResultOn is printVerbResult for a verb this target ran.
func printVerbResultOn(_ *verbTarget, raw json.RawMessage, jsonOutput bool) error {
	return printVerbResult(raw, jsonOutput)
}
