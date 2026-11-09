package tuie2e

import (
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// TestWorkspacesPastNine is the tmux preset on a real daemon with twelve
// workspaces: ctrl+b ' 1 2 enter goes to workspace 12, n and p wrap across it,
// and ctrl+b . 1 0 enter moves the focused pane to workspace 10 and follows it.
//
// Negative control: put the hardcoded 9 back in NewOS. ctrl+b c stops opening
// workspaces at 9 and the wait for workspace 10 fails.
func TestWorkspacesPastNine(t *testing.T) {
	term, base := panesSession(t, "pastnine", "\n[keybindings]\npreset = \"tmux\"\n")
	waitForSettledGeometryIn(t, base, "pastnine", 1)

	press := func(keys ...any) {
		t.Helper()
		if err := term.SendKeys(append([]any{tuitest.Ctrl('b')}, keys...)...); err != nil {
			t.Fatalf("press ctrl+b %v: %v", keys, err)
		}
	}
	panesOn := func(ws int) int {
		n := 0
		for _, p := range listPanes(t, base, "pastnine") {
			if p.Workspace == ws {
				n++
			}
		}
		return n
	}
	waitPanes := func(ws, want int) {
		t.Helper()
		deadline := time.Now().Add(uiTimeout)
		for time.Now().Before(deadline) {
			if panesOn(ws) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("workspace %d has %d panes, want %d", ws, panesOn(ws), want)
	}

	for ws := 2; ws <= 12; ws++ {
		press("c")
		waitWorkspace(t, base, "pastnine", ws)
		waitPanes(ws, 1)
	}

	press("1")
	waitWorkspace(t, base, "pastnine", 1)
	press("'", "1", "2", tuitest.Enter)
	waitWorkspace(t, base, "pastnine", 12)
	press("n")
	waitWorkspace(t, base, "pastnine", 1)
	press("p")
	waitWorkspace(t, base, "pastnine", 12)

	press(".", "1", "0", tuitest.Enter)
	// The pane leaves 12 empty, so the daemon closes it up; 10 holds both.
	waitWorkspace(t, base, "pastnine", 10)
	waitPanes(10, 2)
}
