package app

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// pastNineOS is a client at the default workspace count with one real pane on
// each of the given workspaces, focused on the first.
func pastNineOS(t *testing.T, current int, workspaces ...int) *OS {
	t.Helper()
	m := &OS{
		Settings:             config.Global,
		NumWorkspaces:        config.MaxWorkspaces,
		CurrentWorkspace:     current,
		FocusedWindow:        -1,
		WorkspaceFocus:       map[int]int{},
		WorkspaceLayouts:     map[int][]WindowLayout{},
		WorkspaceMasterRatio: map[int]float64{},
		WorkspaceHasCustom:   map[int]bool{},
		PendingResizes:       map[string][2]int{},
		Width:                160,
		Height:               40,
	}
	for i, ws := range workspaces {
		w := newTestWindow(t, "past-nine-"+strings.Repeat("x", i+1), 60, 20)
		w.Workspace = ws
		m.Windows = append(m.Windows, w)
		if ws == current && m.FocusedWindow < 0 {
			m.FocusedWindow = i
		}
	}
	return m
}

func windowWorkspace(m *OS, id string) int {
	for _, w := range m.Windows {
		if w.ID == id {
			return w.Workspace
		}
	}
	return -1
}

// TestNewOSHasMoreThanNineWorkspaces: a client starts with config.MaxWorkspaces
// workspaces, not the nine the digit keys reach.
//
// Negative control: put back the hardcoded 9 in NewOS and this fails.
func TestNewOSHasMoreThanNineWorkspaces(t *testing.T) {
	if got := NewOS(OSOptions{}).NumWorkspaces; got != config.MaxWorkspaces {
		t.Fatalf("NumWorkspaces = %d, want %d", got, config.MaxWorkspaces)
	}
	if got := NewOS(OSOptions{NumWorkspaces: 500}).NumWorkspaces; got != config.MaxWorkspaces {
		t.Fatalf("NumWorkspaces past the bound = %d, want it clamped to %d", got, config.MaxWorkspaces)
	}
}

// TestSwitchAndMovePastNine: a switch to 12 lands there, and a pane moved to 15
// goes there with the view.
func TestSwitchAndMovePastNine(t *testing.T) {
	m := pastNineOS(t, 1, 1, 1, 12)
	m.SwitchToWorkspace(12)
	if m.CurrentWorkspace != 12 {
		t.Fatalf("CurrentWorkspace = %d, want 12", m.CurrentWorkspace)
	}

	m.SwitchToWorkspace(1)
	moved := m.Windows[0].ID
	m.MoveWindowToWorkspaceAndFollow(0, 15)
	if got := windowWorkspace(m, moved); got != 15 {
		t.Fatalf("moved pane is on workspace %d, want 15", got)
	}
	if m.CurrentWorkspace != 15 {
		t.Fatalf("CurrentWorkspace after move-and-follow = %d, want 15", m.CurrentWorkspace)
	}

	// Past the bound is refused and changes nothing.
	m.SwitchToWorkspace(config.MaxWorkspaces + 1)
	if m.CurrentWorkspace != 15 {
		t.Fatalf("a switch past the bound moved the view to %d", m.CurrentWorkspace)
	}
}

// TestNextAndPrevWorkspaceReachPastNine: next and previous walk the workspaces
// that hold panes, 12 among them, and wrap at both ends.
//
// Negative control: bound cycleWorkspace by 9 and 9 -> next stays on 9.
func TestNextAndPrevWorkspaceReachPastNine(t *testing.T) {
	m := pastNineOS(t, 9, 1, 9, 12)
	m.NextWorkspace()
	if m.CurrentWorkspace != 12 {
		t.Fatalf("next from 9 = %d, want 12", m.CurrentWorkspace)
	}
	m.NextWorkspace()
	if m.CurrentWorkspace != 1 {
		t.Fatalf("next from 12 = %d, want 1 (wrap)", m.CurrentWorkspace)
	}
	m.PrevWorkspace()
	if m.CurrentWorkspace != 12 {
		t.Fatalf("prev from 1 = %d, want 12 (wrap)", m.CurrentWorkspace)
	}
}

// TestGotoWorkspacePrompt: tmux's prefix ' takes digits only, as many as the
// highest workspace number has, and enter switches there.
func TestGotoWorkspacePrompt(t *testing.T) {
	m := pastNineOS(t, 1, 1, 12)
	m.BeginGotoWorkspacePrompt()
	if !m.Renaming() || !m.workspacePromptOpen() {
		t.Fatal("the prompt did not open")
	}
	if title := m.RenameDialogTitle(); !strings.Contains(title, "go to workspace") || !strings.Contains(title, "1-"+strconv.Itoa(config.MaxWorkspaces)) {
		t.Errorf("title = %q, want the action and the range", title)
	}
	m.RenameAppend("1a")
	m.RenameAppend(" 2")
	m.RenameAppend("3")
	if m.RenameBuffer != "12" {
		t.Fatalf("buffer = %q, want 12: letters, blanks and a third digit are dropped", m.RenameBuffer)
	}
	m.CommitRename()
	if m.Renaming() {
		t.Error("the prompt stayed open after enter")
	}
	if m.CurrentWorkspace != 12 {
		t.Fatalf("CurrentWorkspace = %d, want 12", m.CurrentWorkspace)
	}
}

// TestGotoWorkspacePromptRefusesOutOfRange: 0, and an empty line, change
// nothing; 0 says the range.
func TestGotoWorkspacePromptRefusesOutOfRange(t *testing.T) {
	m := pastNineOS(t, 1, 1, 12)
	m.BeginGotoWorkspacePrompt()
	m.RenameAppend("0")
	m.CommitRename()
	if m.CurrentWorkspace != 1 {
		t.Fatalf("workspace 0 switched the view to %d", m.CurrentWorkspace)
	}
	found := false
	for _, n := range m.Notifications {
		if strings.Contains(n.Message, "from 1 to "+strconv.Itoa(config.MaxWorkspaces)) {
			found = true
		}
	}
	if !found {
		t.Error("an out-of-range number did not say the range")
	}

	m.BeginGotoWorkspacePrompt()
	m.CommitRename()
	if m.CurrentWorkspace != 1 || m.Renaming() {
		t.Fatalf("an empty prompt changed the view (%d) or stayed open", m.CurrentWorkspace)
	}
}

// TestMoveToWorkspacePrompt: the pane focused when the prompt opened goes to
// the number typed, and the view follows it.
func TestMoveToWorkspacePrompt(t *testing.T) {
	m := pastNineOS(t, 1, 1, 1, 12)
	m.FocusedWindow = 1
	moving := m.Windows[1].ID
	m.BeginMoveToWorkspacePrompt()
	if m.RenameTargetID != moving {
		t.Fatalf("the prompt targets %q, want the focused pane %q", m.RenameTargetID, moving)
	}
	if !strings.Contains(m.RenameDialogTitle(), "move pane to workspace") {
		t.Errorf("title = %q", m.RenameDialogTitle())
	}
	m.RenameAppend("12")
	m.CommitRename()
	if got := windowWorkspace(m, moving); got != 12 {
		t.Fatalf("the pane is on workspace %d, want 12", got)
	}
	if got := windowWorkspace(m, m.Windows[0].ID); got != 1 {
		t.Fatalf("the other pane moved too, to %d", got)
	}
	if m.CurrentWorkspace != 12 {
		t.Fatalf("CurrentWorkspace = %d, want 12 (follow)", m.CurrentWorkspace)
	}
}

// TestMoveToWorkspacePromptNeedsAPane: with no pane focused the prompt does
// not open.
func TestMoveToWorkspacePromptNeedsAPane(t *testing.T) {
	m := pastNineOS(t, 3, 1)
	m.FocusedWindow = -1
	m.BeginMoveToWorkspacePrompt()
	if m.Renaming() {
		t.Fatal("the move prompt opened with no pane to move")
	}
}

// TestWorkspaceListsShowPastNine: the dock strip, and the workspace chooser
// (tmux's prefix w), list workspaces 10-12 with their numbers and pane counts.
func TestWorkspaceListsShowPastNine(t *testing.T) {
	m := pastNineOS(t, 11, 1, 10, 11, 12, 12)

	if got := m.occupiedWorkspaces(); !slices.Equal(got, []int{1, 10, 11, 12}) {
		t.Fatalf("occupied workspaces = %v, want [1 10 11 12]", got)
	}
	dock, _ := m.renderDockString()
	var drawn []int
	for _, h := range m.dockWorkspaceHits {
		drawn = append(drawn, h.Workspace)
	}
	for _, ws := range []int{10, 11, 12} {
		if !slices.Contains(drawn, ws) {
			t.Errorf("the dock drew no pill for workspace %d (pills: %v)", ws, drawn)
		}
		if !strings.Contains(dock, strconv.Itoa(ws)) {
			t.Errorf("the dock text does not show %d", ws)
		}
	}

	m.OpenWorkspaceSwitcher()
	var twelve *WorkspaceItem
	for i := range m.WorkspaceSwitcherItems {
		if m.WorkspaceSwitcherItems[i].Number == 12 {
			twelve = &m.WorkspaceSwitcherItems[i]
		}
	}
	if twelve == nil || twelve.Panes != 2 {
		t.Fatalf("the chooser row for 12 = %+v, want 2 panes", twelve)
	}
	if cur := m.WorkspaceSwitcherItems[m.WorkspaceSwitcherSelected]; cur.Number != 11 {
		t.Errorf("the chooser opened on %d, want the current workspace 11", cur.Number)
	}
	m.WorkspaceSwitcherQuery = "12"
	m.WorkspaceSwitcherActivate(0)
	if m.CurrentWorkspace != 12 {
		t.Fatalf("picking 12 in the chooser went to %d", m.CurrentWorkspace)
	}
}
