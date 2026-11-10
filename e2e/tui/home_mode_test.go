package tuie2e

import (
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuitest"
)

// TestTerminalModeIsTheHomeMode drives the fork's default,
// appearance.return_to_window_mode = false, which the rest of the suite pins
// on (see keyPins). Terminal mode is the home mode: a new pane and a leader
// split leave the user typing, leader esc is the way into window mode, window
// mode stays while w closes a pane and n opens one, i leaves it, and a plain
// esc in terminal mode goes to the program in the pane.
//
// NEGATIVE CONTROL: with return_to_window_mode = true in the config below, the
// first runInShell fails: the keys of the command are window-manager keys.
func TestTerminalModeIsTheHomeMode(t *testing.T) {
	base := t.TempDir()
	// Tiled, because a split needs a tiled layout.
	writeConfig(t, base, "[startup]\ntiled = true\n\n[appearance]\nreturn_to_window_mode = false\n")
	term := startIn(t, base, startOpts{})
	waitBoot(t, term)

	// The splash is window mode, which nothing chose: n opens a pane, and
	// the pane takes the keyboard with no i.
	newWindow(t, term)
	runInShell(t, term, "echo $((40+2))HOME", "42HOME", shellTimeout)

	// A leader split keeps terminal mode.
	send(t, term, tuitest.Ctrl('b'), "-")
	waitWindowCount(t, term, 2, "leader split")
	runInShell(t, term, "echo $((50+5))HOME", "55HOME", shellTimeout)

	// Leader esc is the way into window mode, and it stays there.
	send(t, term, tuitest.Ctrl('b'), tuitest.Esc)
	if err := term.WaitForText("Window management mode", uiTimeout); err != nil {
		t.Fatalf("leader esc did not enter window mode: %v\n%s", err, term.Snapshot())
	}
	send(t, term, "w")
	waitWindowCount(t, term, 1, "w in window mode")
	newWindow(t, term)

	// i leaves window mode, and typing reaches the shell again.
	enterTerminalMode(t, term)
	runInShell(t, term, "echo $((60+7))HOME", "67HOME", shellTimeout)

	// A plain esc in terminal mode goes to the program: cat -v shows it.
	send(t, term, "cat -v", tuitest.Enter)
	// Apart, so the esc does not read as alt and the next letter.
	send(t, term, tuitest.Esc)
	send(t, term, "ESCSEEN", tuitest.Enter)
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		return strings.Count(s.Text(), "^[ESCSEEN") >= 1
	}, uiTimeout); err != nil {
		t.Fatalf("esc in terminal mode did not reach the program: %v\n%s", err, term.Snapshot())
	}
	send(t, term, tuitest.Ctrl('c'))
	alive(t, term, "after the esc check")
}
