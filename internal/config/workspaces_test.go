package config

import (
	"fmt"
	"strings"
	"testing"
)

// TestMaxWorkspacesReachesPastNine pins the bound itself: past the nine digit
// keys, and short of the scratch workspaces, which start at 1000
// (session.ScratchWorkspaceBase; this package cannot import session).
func TestMaxWorkspacesReachesPastNine(t *testing.T) {
	if MaxWorkspaces <= 9 {
		t.Fatalf("MaxWorkspaces = %d, want more than the nine digit keys reach", MaxWorkspaces)
	}
	if MaxWorkspaces >= 1000 {
		t.Fatalf("MaxWorkspaces = %d runs into the scratch workspaces at 1000", MaxWorkspaces)
	}
}

// TestSessionWorkspaceAcceptsPastNine: an edge's session_workspace may pin
// a workspace past 9, up to MaxWorkspaces, and is refused past that.
//
// Negative control: put the old > 9 check back and the 12 and 99 rows fail.
func TestSessionWorkspaceAcceptsPastNine(t *testing.T) {
	for _, tc := range []struct {
		ws   int
		warn bool
	}{
		{0, false}, {9, false}, {12, false}, {MaxWorkspaces, false},
		{MaxWorkspaces + 1, true}, {-1, true},
	} {
		cfg := DefaultConfig()
		cfg.Appearance.Sidebar.Left = &SidebarEdgeConfig{SessionWorkspace: tc.ws}
		warned := false
		for _, w := range ValidateConfig(cfg).Warnings {
			if w.Key == "session_workspace" {
				warned = true
				if !strings.Contains(w.Message, fmt.Sprintf("1-%d", MaxWorkspaces)) {
					t.Errorf("session_workspace %d: message %q does not name the range", tc.ws, w.Message)
				}
			}
		}
		if warned != tc.warn {
			t.Errorf("session_workspace %d: warned = %v, want %v", tc.ws, warned, tc.warn)
		}
	}
}

// TestWorkspaceNumberPromptKeys pins the keys that reach a workspace past 9:
// tmux's ' and . after the leader in the default (tmux) preset, with the path
// picker that . displaced moved to the tuios menu, and ' and . in the
// workspace menu of the tuios preset.
//
// Negative control: drop goto_workspace_prompt from presets/tmux.toml and
// the leader ' row fails.
func TestWorkspaceNumberPromptKeys(t *testing.T) {
	tmux := NewKeybindRegistry(LoadedDefaultConfig())
	for key, want := range map[string]string{
		"'": "goto_workspace_prompt",
		".": "move_to_workspace_prompt",
		"w": "prefix_workspace_switcher",
	} {
		if got := tmux.GetPrefixAction(key); got != want {
			t.Errorf("tmux preset: leader %s runs %q, want %q", key, got, want)
		}
	}
	if got := tmux.GetTuiosPrefixAction("."); got != "pick_path" {
		t.Errorf("tmux preset: the tuios menu . runs %q, want pick_path", got)
	}

	tuios := NewKeybindRegistry(presetConfig(t, PresetTuios, ""))
	for key, want := range map[string]string{
		"'": "goto_workspace_prompt",
		".": "move_to_workspace_prompt",
	} {
		if got := tuios.GetWorkspacePrefixAction(key); got != want {
			t.Errorf("tuios preset: workspace menu %s runs %q, want %q", key, got, want)
		}
	}
	if got := tuios.GetPrefixAction("."); got != "pick_path" {
		t.Errorf("tuios preset: leader . runs %q, want pick_path (unchanged)", got)
	}
	for _, action := range []string{"goto_workspace_prompt", "move_to_workspace_prompt"} {
		if ActionDescriptions[action] == "" {
			t.Errorf("%s has no description", action)
		}
	}
}
