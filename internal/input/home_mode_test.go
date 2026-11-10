package input

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Gaurav-Gosain/tuios/internal/app"
	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// Terminal mode is the home mode (appearance.return_to_window_mode = false, the
// default). These tests drive the real Update path, so the settle that runs
// after every message is part of what they check.

// homeModeOS is a local client with n panes on workspace 1, the first focused,
// in terminal mode. Each pane's input is captured. returnToWindow sets
// appearance.return_to_window_mode.
func homeModeOS(t *testing.T, returnToWindow bool, n int) (*app.OS, []*strings.Builder) {
	t.Helper()
	app.SetInputHandler(HandleInput)
	cfg := config.DefaultConfig()
	cfg.Appearance.ReturnToWindowMode = &returnToWindow
	settings := config.Global
	settings.ReturnToWindowMode = returnToWindow
	o := app.NewOS(app.OSOptions{
		UserConfig:      cfg,
		KeybindRegistry: config.NewKeybindRegistry(cfg),
		Settings:        &settings,
	})
	o.Width, o.Height = 160, 40
	o.EffectiveWidth, o.EffectiveHeight = 160, 40
	sent := make([]*strings.Builder, n)
	for i := range n {
		sent[i] = &strings.Builder{}
		w := multifocusPane(t, fmt.Sprintf("home-%d", i), false, sent[i])
		w.X, w.Y, w.Workspace, w.Z = i*50, 1, 1, i
		o.Windows = append(o.Windows, w)
	}
	o.CurrentWorkspace, o.FocusedWindow = 1, 0
	o.Mode = app.TerminalMode
	return o, sent
}

var (
	homeLeader = tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	homeEsc    = tea.KeyPressMsg{Code: tea.KeyEscape}
	homeEnter  = tea.KeyPressMsg{Code: tea.KeyEnter}
)

func homeKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func homePress(o *app.OS, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		o.Update(k)
	}
}

func wantMode(t *testing.T, o *app.OS, want app.Mode, when string) {
	t.Helper()
	if o.Mode != want {
		name := map[app.Mode]string{app.TerminalMode: "terminal", app.WindowManagementMode: "window"}
		t.Fatalf("%s: mode = %s, want %s", when, name[o.Mode], name[want])
	}
}

// addPane puts a new focused pane on the current workspace, the way a pane the
// daemon opens arrives.
func addPane(t *testing.T, o *app.OS, id string) *strings.Builder {
	t.Helper()
	sent := &strings.Builder{}
	w := multifocusPane(t, id, false, sent)
	w.Workspace = o.CurrentWorkspace
	o.Windows = append(o.Windows, w)
	o.FocusWindow(len(o.Windows) - 1)
	return sent
}

func TestPlainEscInTerminalModeReachesThePane(t *testing.T) {
	o, sent := homeModeOS(t, false, 1)
	homePress(o, homeEsc)
	wantMode(t, o, app.TerminalMode, "after esc")
	if got := sent[0].String(); got != "\x1b" {
		t.Fatalf("the pane got %q, want the esc byte", got)
	}
}

func TestLeaderEscEntersWindowModeAndItStays(t *testing.T) {
	o, sent := homeModeOS(t, false, 3)
	homePress(o, homeLeader, homeEsc)
	wantMode(t, o, app.WindowManagementMode, "after leader esc")
	if !o.WindowModeChosen() {
		t.Fatal("leader esc did not mark window mode as chosen")
	}
	if sent[0].Len() != 0 {
		t.Fatalf("leader esc typed %q into the pane", sent[0].String())
	}

	// w closes the focused pane, and the user stays in window mode.
	homePress(o, homeKey('w'))
	if len(o.Windows) != 2 {
		t.Fatalf("w left %d panes, want 2", len(o.Windows))
	}
	wantMode(t, o, app.WindowManagementMode, "after w closed a pane")

	// Tab cycles, and the mode still stays.
	homePress(o, tea.KeyPressMsg{Code: tea.KeyTab})
	wantMode(t, o, app.WindowManagementMode, "after tab")

	// i goes back to terminal mode, and the next key reaches the pane.
	homePress(o, homeKey('i'))
	wantMode(t, o, app.TerminalMode, "after i")
	homePress(o, homeKey('z'))
	reached := false
	for _, s := range sent {
		reached = reached || strings.Contains(s.String(), "z")
	}
	if !reached {
		t.Fatal("a key typed after i reached no pane")
	}
}

func TestWindowModeStaysWhenTheLastPaneClosesAndOneOpens(t *testing.T) {
	o, _ := homeModeOS(t, false, 1)
	homePress(o, homeLeader, homeEsc, homeKey('w'))
	if len(o.Windows) != 0 {
		t.Fatalf("w left %d panes, want 0", len(o.Windows))
	}
	wantMode(t, o, app.WindowManagementMode, "with no pane")
	addPane(t, o, "late")
	o.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	wantMode(t, o, app.WindowManagementMode, "a chosen window mode after a new pane")
}

func TestLeaderCloseOfTheLastPaneThenANewPaneTypes(t *testing.T) {
	o, _ := homeModeOS(t, false, 1)
	homePress(o, homeLeader, homeKey('x'))
	if len(o.Windows) != 0 {
		t.Fatalf("leader x left %d panes, want 0", len(o.Windows))
	}
	// Nothing to type into: window mode, so the splash keys work.
	wantMode(t, o, app.WindowManagementMode, "with no pane")
	if o.WindowModeChosen() {
		t.Fatal("the empty workspace's window mode counts as chosen")
	}
	// esc on the splash does not make that window mode stay.
	homePress(o, homeEsc)
	if o.WindowModeChosen() {
		t.Fatal("esc on the splash made window mode stay")
	}

	sent := addPane(t, o, "late")
	o.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	wantMode(t, o, app.TerminalMode, "after a new pane opened")
	homePress(o, homeKey('k'))
	if !strings.Contains(sent.String(), "k") {
		t.Fatalf("the new pane got %q, want the k typed after it opened", sent.String())
	}
}

func TestLeaderCloseKeepsTerminalMode(t *testing.T) {
	o, sent := homeModeOS(t, false, 2)
	homePress(o, homeLeader, homeKey('x'))
	if len(o.Windows) != 1 {
		t.Fatalf("leader x left %d panes, want 1", len(o.Windows))
	}
	wantMode(t, o, app.TerminalMode, "after leader x")
	homePress(o, homeKey('q'))
	if !strings.Contains(sent[1].String(), "q") {
		t.Fatalf("the remaining pane got %q, want q", sent[1].String())
	}
}

func TestWorkspaceSwitchThroughAnEmptyWorkspaceKeepsTerminalMode(t *testing.T) {
	o, _ := homeModeOS(t, false, 1)
	homePress(o, homeLeader, homeKey('w'), homeKey('2'))
	if o.CurrentWorkspace != 2 {
		t.Fatalf("workspace %d, want 2", o.CurrentWorkspace)
	}
	wantMode(t, o, app.WindowManagementMode, "on the empty workspace")
	homePress(o, homeLeader, homeKey('w'), homeKey('1'))
	if o.CurrentWorkspace != 1 {
		t.Fatalf("workspace %d, want 1", o.CurrentWorkspace)
	}
	wantMode(t, o, app.TerminalMode, "back on the workspace with a pane")
}

func TestOptOutRestoresTheOldAutoReturn(t *testing.T) {
	o, _ := homeModeOS(t, true, 1)
	homePress(o, homeLeader, homeKey('w'), homeKey('2'))
	homePress(o, homeLeader, homeKey('w'), homeKey('1'))
	wantMode(t, o, app.WindowManagementMode, "return_to_window_mode after an empty workspace")

	o, _ = homeModeOS(t, true, 1)
	homePress(o, homeLeader, homeKey('r'))
	wantMode(t, o, app.WindowManagementMode, "return_to_window_mode after rename")
	homePress(o, homeEsc)
	wantMode(t, o, app.WindowManagementMode, "return_to_window_mode after the rename closed")

	o, _ = homeModeOS(t, true, 2)
	o.MinimizeWindow(1)
	o.Mode = app.TerminalMode
	o.RestoreWindow(1)
	wantMode(t, o, app.WindowManagementMode, "return_to_window_mode after a restore")

	// The startup window mode with a pane stays, as upstream.
	o, _ = homeModeOS(t, true, 1)
	o.Mode = app.WindowManagementMode
	o.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	wantMode(t, o, app.WindowManagementMode, "return_to_window_mode at startup")
}

func TestRenameReturnsToThePriorMode(t *testing.T) {
	o, sent := homeModeOS(t, false, 1)
	homePress(o, homeLeader, homeKey('r'))
	if !o.Renaming() {
		t.Fatal("leader r opened no rename editor")
	}
	homePress(o, homeKey('a'), homeKey('b'))
	if sent[0].Len() != 0 {
		t.Fatalf("keys typed into the rename editor reached the pane: %q", sent[0].String())
	}
	homePress(o, homeEnter)
	if o.Renaming() {
		t.Fatal("enter did not close the rename editor")
	}
	wantMode(t, o, app.TerminalMode, "after a rename started in terminal mode")

	// From a chosen window mode, the rename returns there.
	homePress(o, homeLeader, homeEsc, homeLeader, homeKey('r'), homeEsc)
	if o.Renaming() {
		t.Fatal("esc did not close the rename editor")
	}
	wantMode(t, o, app.WindowManagementMode, "after a rename started in window mode")
}

func TestRestoreFromTheDockKeepsTerminalMode(t *testing.T) {
	o, _ := homeModeOS(t, false, 2)
	o.MinimizeWindow(1)
	o.Mode = app.TerminalMode
	o.RestoreWindow(1)
	wantMode(t, o, app.TerminalMode, "after a restore")
}

func TestStartupWindowModeEndsWithAFocusedPane(t *testing.T) {
	o, _ := homeModeOS(t, false, 1)
	o.Mode = app.WindowManagementMode // a fresh client's zero mode
	o.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	wantMode(t, o, app.TerminalMode, "after the first message")
}

func TestTitleBarDragFromTerminalModeGivesTerminalModeBack(t *testing.T) {
	o, _ := homeModeOS(t, false, 2)
	w := o.Windows[1]
	x, y := w.X+w.Width/2, w.Y
	if _, _, in := w.ScreenToTerminal(x, y); in {
		t.Fatalf("(%d,%d) is in the content, want the frame", x, y)
	}
	o.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	o.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x + 6, Y: y + 2})
	o.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x + 6, Y: y + 2})
	wantMode(t, o, app.TerminalMode, "after a title-bar drag")
	if o.FocusedWindow != 1 {
		t.Fatalf("focused %d, want the dragged pane 1", o.FocusedWindow)
	}
}
