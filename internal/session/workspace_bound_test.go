package session

import (
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// TestWorkspaceBoundFollowsTheSession pins that the workspace range check is the
// session's own, not a constant this package guessed. It used to be a hardcoded
// 9 duplicated here to avoid a config import, which meant a client configured
// for more workspaces could reach one the daemon then refused to switch to, and
// one configured for fewer got no error for a workspace it does not have.
func TestWorkspaceBoundFollowsTheSession(t *testing.T) {
	for _, tc := range []struct {
		name    string
		declare int
		ws      int
		wantErr bool
	}{
		{"unstated falls back to the default", 0, config.MaxWorkspaces, false},
		{"unstated reaches past the nine digit keys", 0, 12, false},
		{"unstated rejects past the default", 0, config.MaxWorkspaces + 1, true},
		{"a declared count accepts past nine", 12, 12, false},
		{"a declared count rejects past itself", 12, 13, true},
		{"a smaller session rejects inside the default", 3, 4, true},
		{"zero is out of range whatever the count", 12, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := newTestSession(t)
			if tc.declare > 0 {
				// This is how the count arrives in practice: a client reports it
				// with the rest of its state.
				st := sess.GetState()
				st.NumWorkspaces = tc.declare
				sess.UpdateState(st)
			}

			err := sess.SwitchDaemonWorkspace(tc.ws)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SwitchDaemonWorkspace(%d) error = %v, wantErr = %v", tc.ws, err, tc.wantErr)
			}
			if err == nil && sess.GetState().CurrentWorkspace != tc.ws {
				t.Fatalf("workspace = %d, want %d", sess.GetState().CurrentWorkspace, tc.ws)
			}
		})
	}
}

// TestWorkspaceCountIsReportedNotAssumed covers the query side: session info
// reported the constant regardless of what the session actually had.
func TestWorkspaceCountIsReportedNotAssumed(t *testing.T) {
	sess := newTestSession(t)
	st := sess.GetState()
	st.NumWorkspaces = 4
	st.LayoutMode = "scrolling"
	sess.UpdateState(st)

	data := buildSessionInfoData(sess, sess.GetState(), false, HostFocusUnknown)
	if got := data["num_workspaces"]; got != 4 {
		t.Fatalf("num_workspaces = %v, want 4", got)
	}
	if got := data["layout_mode"]; got != "scrolling" {
		t.Fatalf("layout_mode = %v, want scrolling", got)
	}
}

// TestDaemonOpsReachPastNine pins the headless paths on a session with more
// than nine workspaces in use: a pane opens on 12, another moves there, the
// session switches to it and names it, and none of it is refused or renumbered.
func TestDaemonOpsReachPastNine(t *testing.T) {
	sess := newTestSession(t)
	ids := map[int]string{}
	for ws := 1; ws <= 12; ws++ {
		w, err := sess.AddDaemonWindowWith(NewWindowOptions{Title: "p", Workspace: ws}, nil)
		if err != nil {
			t.Fatalf("open a pane on workspace %d: %v", ws, err)
		}
		ids[ws] = w.ID
	}
	// A second pane on 1, so the move leaves no workspace empty for the
	// daemon to close up.
	extra, err := sess.AddDaemonWindowWith(NewWindowOptions{Title: "extra", Workspace: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.MoveDaemonWindowToWorkspace(extra.ID, 12); err != nil {
		t.Fatalf("move to 12: %v", err)
	}
	if err := sess.SwitchDaemonWorkspace(12); err != nil {
		t.Fatalf("switch to 12: %v", err)
	}
	if err := sess.SetDaemonWorkspaceName(12, "twelve"); err != nil {
		t.Fatalf("name 12: %v", err)
	}
	st := sess.GetState()
	if st.CurrentWorkspace != 12 {
		t.Errorf("current workspace = %d, want 12", st.CurrentWorkspace)
	}
	on12 := 0
	for _, w := range st.Windows {
		if w.Workspace == 12 {
			on12++
		}
	}
	if on12 != 2 {
		t.Errorf("workspace 12 holds %d panes, want 2", on12)
	}
	if st.WorkspaceNames[12] != "twelve" {
		t.Errorf("workspace 12 name = %q, want twelve", st.WorkspaceNames[12])
	}
	if err := sess.MoveDaemonWindowToWorkspace(ids[1], config.MaxWorkspaces+1); err == nil {
		t.Errorf("a move past %d was not refused", config.MaxWorkspaces)
	}
}
