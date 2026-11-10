package session

import (
	"maps"
	"slices"
	"strconv"
	"testing"
)

// A saved state can hold names of workspaces with no pane: builds from before
// dropEmptiedNamesLocked kept them. A load and a restore drop those, and only
// those.

// namedEverywhere is a saved session with workspaces 1 to 9 named and a pane
// on 1, 2, 3, 6 and 7 only, the shape of a state saved before names went with
// the last pane.
func namedEverywhere(name string) *SessionState {
	st := &SessionState{Name: name, Width: 120, Height: 40, WorkspaceNames: map[int]string{}}
	for ws := 1; ws <= 9; ws++ {
		st.WorkspaceNames[ws] = "ws-" + strconv.Itoa(ws)
	}
	for _, ws := range []int{1, 2, 3, 6, 7} {
		st.Windows = append(st.Windows, WindowState{ID: "win-" + strconv.Itoa(ws), Width: 60, Height: 40, Workspace: ws})
	}
	return st
}

func wantNames(t *testing.T, got map[int]string, workspaces ...int) {
	t.Helper()
	want := map[int]string{}
	for _, ws := range workspaces {
		want[ws] = "ws-" + strconv.Itoa(ws)
	}
	if !maps.Equal(got, want) {
		t.Errorf("workspace names = %v, want %v", got, want)
	}
}

// Negative control: drop both dropNamesOfEmptyWorkspaces calls and every
// check of names 4, 5, 8 and 9 fails.
func TestRestoreDropsNamesOfEmptyWorkspaces(t *testing.T) {
	defer useResurrectionDir(t.TempDir())()
	if err := SaveSessionForResurrection(namedEverywhere("work")); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadResurrectionState("work")
	if err != nil {
		t.Fatal(err)
	}
	wantNames(t, loaded.WorkspaceNames, 1, 2, 3, 6, 7)

	d := NewDaemon(&DaemonConfig{})
	defer d.manager.Shutdown()
	d.restoreAllSessions()
	sess := d.manager.GetSession("work")
	if sess == nil {
		t.Fatal("the session was not restored")
	}
	st := sess.GetState()
	if len(st.Windows) != 5 {
		t.Fatalf("%d panes came back, want 5", len(st.Windows))
	}
	wantNames(t, st.WorkspaceNames, 1, 2, 3, 6, 7)
	for ws := range st.WorkspaceNames {
		if !hasPanes(st, ws) {
			t.Errorf("workspace %d kept its name with no pane", ws)
		}
	}

	// The restore pass alone, for a state that did not come through a load.
	sess2, err := d.restoreSession(namedEverywhere("direct"))
	if err != nil {
		t.Fatal(err)
	}
	wantNames(t, sess2.GetState().WorkspaceNames, 1, 2, 3, 6, 7)

	// Inside the restored session the old rule holds: a name given to an
	// empty workspace survives its first pane.
	if err := sess.SetDaemonWorkspaceName(8, "later"); err != nil {
		t.Fatal(err)
	}
	if err := sess.SwitchDaemonWorkspace(8); err != nil {
		t.Fatal(err)
	}
	w, err := sess.AddDaemonWindow("c", nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.Workspace != 8 {
		t.Fatalf("the new pane went to workspace %d, want 8", w.Workspace)
	}
	if name := sess.GetState().WorkspaceNames[8]; name != "later" {
		t.Errorf("a name given to an empty workspace after a restore = %q after its first pane, want later", name)
	}
}

// A workspace whose only pane does not respawn holds no pane after the
// restore, and loses its name as if that pane had closed. A workspace with
// one pane that does respawn keeps its name.
func TestRestoreDropsTheNameOfAWorkspaceWhosePaneDidNotRespawn(t *testing.T) {
	defer useResurrectionDir(t.TempDir())()
	// Every shell fails to start. A placeholder runs /bin/sh by path, so the
	// panes with a kind still respawn.
	t.Setenv("SHELL", "/nonexistent/definitely-not-a-shell")
	st := &SessionState{Name: "half", Width: 120, Height: 40,
		WorkspaceNames: map[int]string{1: "ws-1", 2: "ws-2", 3: "ws-3"},
		Windows: []WindowState{
			{ID: "view-1", Width: 60, Height: 40, Workspace: 1, Kind: "markdown", URI: "a.md"},
			{ID: "shell-2", Width: 60, Height: 40, Workspace: 2},
			{ID: "view-3", Width: 60, Height: 40, Workspace: 3, Kind: "markdown", URI: "b.md"},
			{ID: "shell-3", Width: 60, Height: 40, Workspace: 3},
		},
	}
	d := NewDaemon(&DaemonConfig{})
	defer d.manager.Shutdown()
	sess, err := d.restoreSession(st)
	if err != nil {
		t.Fatal(err)
	}
	got := sess.GetState()
	var ids []string
	for _, w := range got.Windows {
		ids = append(ids, w.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"view-1", "view-3"}) {
		t.Fatalf("panes that came back = %v, want the two placeholders", ids)
	}
	wantNames(t, got.WorkspaceNames, 1, 3)
}
