package session

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestUnnamedWorkspaceStateIsUnchanged is the byte-level compatibility check for
// workspaces: naming one workspace and clearing it again must leave the state
// exactly as it started, with no empty map and no key in the serialized form.
func TestUnnamedWorkspaceStateIsUnchanged(t *testing.T) {
	sess := newTestSession(t)
	if _, err := sess.AddDaemonWindow("shell", nil); err != nil {
		t.Fatalf("AddDaemonWindow: %v", err)
	}

	baseline, err := json.Marshal(sess.GetState())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(baseline), `"workspace_names"`) {
		t.Fatalf("an untouched session carries workspace_names: %s", baseline)
	}

	if err := sess.SetDaemonWorkspaceName(2, "review"); err != nil {
		t.Fatalf("SetDaemonWorkspaceName: %v", err)
	}
	if err := sess.SetDaemonWorkspaceName(2, ""); err != nil {
		t.Fatalf("SetDaemonWorkspaceName clear: %v", err)
	}
	after, err := json.Marshal(sess.GetState())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(after), `"workspace_names"`) {
		t.Fatalf("a cleared workspace name left a key behind: %s", after)
	}
}

// TestWorkspaceNameReachesEveryClient checks the name is announced on the state
// push and that a client sync which omits it does not wipe it, which is what
// makes it visible to a second client rather than only the one that set it.
func TestWorkspaceNameReachesEveryClient(t *testing.T) {
	sess := newTestSession(t)
	pushes := recordStateSink(sess)

	if err := sess.SetDaemonWorkspaceName(3, "review"); err != nil {
		t.Fatalf("SetDaemonWorkspaceName: %v", err)
	}
	got := pushes()
	if len(got) != 1 || got[0].WorkspaceNames[3] != "review" {
		t.Fatalf("pushed workspace names = %v", got)
	}

	incoming := sess.GetState()
	incoming.WorkspaceNames = nil
	sess.UpdateState(incoming)

	if name := sess.GetState().WorkspaceNames[3]; name != "review" {
		t.Fatalf("a client sync wiped the workspace name: %q", name)
	}
}

// TestWorkspaceNameSurvivesResurrection checks a named workspace outlives the
// daemon.
func TestWorkspaceNameSurvivesResurrection(t *testing.T) {
	t.Cleanup(useResurrectionDir(t.TempDir()))

	sess := newTestSession(t)
	if err := sess.SetDaemonWorkspaceName(3, "review"); err != nil {
		t.Fatalf("SetDaemonWorkspaceName: %v", err)
	}
	state := sess.GetState()
	state.Name = "work"
	// A pane on the workspace: the name of a workspace with none is dropped
	// on load (TestRestoreDropsNamesOfEmptyWorkspaces).
	state.Windows = append(state.Windows, WindowState{ID: "win-3", Workspace: 3})
	if err := SaveSessionForResurrection(state); err != nil {
		t.Fatalf("SaveSessionForResurrection: %v", err)
	}

	loaded, err := LoadResurrectionState("work")
	if err != nil {
		t.Fatalf("LoadResurrectionState: %v", err)
	}
	if loaded.WorkspaceNames[3] != "review" {
		t.Fatalf("restored workspace names = %v, want 3=review", loaded.WorkspaceNames)
	}
}

// namedTwoWorkspaces is a session with pane a on workspace 1 ("main") and pane
// b on workspace 2 ("review"), workspace 2 on screen, and return_when_empty
// off so an emptied workspace stays on screen: the case the name used to
// outlive, because a shown workspace still exists.
func namedTwoWorkspaces(t *testing.T) (sess *Session, b WindowState) {
	t.Helper()
	sess = newTestSession(t)
	if _, err := sess.AddDaemonWindow("a", nil); err != nil {
		t.Fatalf("AddDaemonWindow: %v", err)
	}
	b, err := sess.AddDaemonWindow("b", nil)
	if err != nil {
		t.Fatalf("AddDaemonWindow: %v", err)
	}
	if err := sess.mutateState(func(st *SessionState) error {
		st.Options = map[string]string{optionReturnWhenEmpty: "false"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := sess.MoveDaemonWindowToWorkspace(b.ID, 2); err != nil {
		t.Fatalf("MoveDaemonWindowToWorkspace: %v", err)
	}
	if err := sess.SwitchDaemonWorkspace(2); err != nil {
		t.Fatalf("SwitchDaemonWorkspace: %v", err)
	}
	if err := sess.SetDaemonWorkspaceName(1, "main"); err != nil {
		t.Fatal(err)
	}
	if err := sess.SetDaemonWorkspaceName(2, "review"); err != nil {
		t.Fatal(err)
	}
	return sess, b
}

// TestEmptiedWorkspaceLosesItsName: every way the last pane leaves a
// workspace takes the workspace's name with it, so a new workspace at that
// number starts unnamed. The other workspace's name stays.
//
// Negative control: drop the dropEmptiedNamesLocked call and every row fails,
// because workspace 2 is still on screen and so still exists.
func TestEmptiedWorkspaceLosesItsName(t *testing.T) {
	cases := []struct {
		name   string
		remove func(t *testing.T, sess *Session, b WindowState)
	}{
		{"closed", func(t *testing.T, sess *Session, b WindowState) {
			if _, err := sess.CloseDaemonWindow(b.ID); err != nil {
				t.Fatalf("CloseDaemonWindow: %v", err)
			}
		}},
		{"moved to another workspace", func(t *testing.T, sess *Session, b WindowState) {
			if err := sess.MoveDaemonWindowToWorkspace(b.ID, 1); err != nil {
				t.Fatalf("MoveDaemonWindowToWorkspace: %v", err)
			}
		}},
		{"process exited", func(t *testing.T, sess *Session, b WindowState) {
			sess.notePaneExitedAlone(b.PTYID, false, 0)
			if _, err := sess.CloseDaemonWindow(b.ID); err != nil {
				t.Fatalf("CloseDaemonWindow: %v", err)
			}
		}},
		{"closed by a client push", func(t *testing.T, sess *Session, b WindowState) {
			incoming := sess.GetState()
			incoming.BaseVersion = incoming.Version
			incoming.WorkspaceNames = nil // a client never sends them
			incoming.Windows = slices.DeleteFunc(incoming.Windows, func(w WindowState) bool { return w.ID == b.ID })
			if sess.UpdateState(incoming) {
				t.Error("the push was accepted as it was, so its sender is not told the name went")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess, b := namedTwoWorkspaces(t)
			c.remove(t, sess, b)

			st := sess.GetState()
			if hasPanes(st, 2) {
				t.Fatal("workspace 2 still holds a pane, so this case proves nothing")
			}
			if st.CurrentWorkspace != 2 {
				t.Fatalf("workspace on screen = %d, want the emptied 2", st.CurrentWorkspace)
			}
			if name, ok := st.WorkspaceNames[2]; ok {
				t.Errorf("the emptied workspace kept its name %q", name)
			}
			if st.WorkspaceNames[1] != "main" {
				t.Errorf("workspace 1's name = %q, want main", st.WorkspaceNames[1])
			}

			// A new pane there starts an unnamed workspace.
			if _, err := sess.AddDaemonWindow("c", nil); err != nil {
				t.Fatalf("AddDaemonWindow: %v", err)
			}
			if name := sess.GetState().WorkspaceNames[2]; name != "" {
				t.Errorf("a new workspace at 2 is named %q", name)
			}
		})
	}
}

// TestNameKeptUntilTheLastPaneLeaves: a close that leaves a pane behind keeps
// the name, and a name given to an empty workspace survives its first pane.
//
// Negative control: drop emptiness to "holds no pane now" without the
// heldBefore check and the second half fails.
func TestNameKeptUntilTheLastPaneLeaves(t *testing.T) {
	sess, b := namedTwoWorkspaces(t)
	extra, err := sess.AddDaemonWindow("b2", nil)
	if err != nil {
		t.Fatalf("AddDaemonWindow: %v", err)
	}
	if extra.Workspace != 2 {
		t.Fatalf("the new pane went to workspace %d, want 2", extra.Workspace)
	}
	if _, err := sess.CloseDaemonWindow(b.ID); err != nil {
		t.Fatalf("CloseDaemonWindow: %v", err)
	}
	if name := sess.GetState().WorkspaceNames[2]; name != "review" {
		t.Fatalf("closing one of two panes changed the name to %q", name)
	}

	if _, err := sess.CloseDaemonWindow(extra.ID); err != nil {
		t.Fatalf("CloseDaemonWindow: %v", err)
	}
	if err := sess.SetDaemonWorkspaceName(2, "fresh"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AddDaemonWindow("c", nil); err != nil {
		t.Fatalf("AddDaemonWindow: %v", err)
	}
	if name := sess.GetState().WorkspaceNames[2]; name != "fresh" {
		t.Fatalf("a name given to the empty workspace = %q after its first pane, want fresh", name)
	}
}
