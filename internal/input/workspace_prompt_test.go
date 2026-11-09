package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Gaurav-Gosain/tuios/internal/app"
)

// TestLeaderQuoteGoesToAWorkspacePastNine drives tmux's prefix ' through the
// real key path with the default (tmux) keys: leader ' 1 2 enter shows
// workspace 12, and none of the keys reaches the shell.
//
// Negative control: drop goto_workspace_prompt from presets/tmux.toml and
// the 1 and 2 go to the shell.
func TestLeaderQuoteGoesToAWorkspacePastNine(t *testing.T) {
	o, pty := osWithFocusedPane(t, legacyConfig(t, ""), app.TerminalMode)
	o = pressKeys(o, "leader", "'")
	if !o.Renaming() {
		t.Fatal("leader ' did not open the workspace prompt")
	}
	o = pressKeys(o, "1", "2")
	o, _ = HandleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter}, o)
	if o.CurrentWorkspace != 12 {
		t.Fatalf("CurrentWorkspace = %d, want 12", o.CurrentWorkspace)
	}
	if o.Renaming() {
		t.Error("the prompt stayed open after enter")
	}
	if len(pty.got) != 0 {
		t.Errorf("the shell was typed %q", pty.got)
	}
}

// TestLeaderDotMovesThePanePastNine: leader . 1 5 enter moves the focused
// pane to workspace 15 and follows it. esc on the prompt moves nothing.
func TestLeaderDotMovesThePanePastNine(t *testing.T) {
	o, _ := osWithFocusedPane(t, legacyConfig(t, ""), app.TerminalMode)
	pane := o.Windows[0]

	o = pressKeys(o, "leader", ".", "1", "5")
	o, _ = HandleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape}, o)
	if pane.Workspace != 1 || o.Renaming() {
		t.Fatalf("esc moved the pane to %d or left the prompt open", pane.Workspace)
	}

	o = pressKeys(o, "leader", ".", "1", "5")
	o, _ = HandleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter}, o)
	if pane.Workspace != 15 {
		t.Fatalf("the pane is on workspace %d, want 15", pane.Workspace)
	}
	if o.CurrentWorkspace != 15 {
		t.Fatalf("CurrentWorkspace = %d, want 15 (follow)", o.CurrentWorkspace)
	}
}
