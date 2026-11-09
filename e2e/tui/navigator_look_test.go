package tuie2e

import (
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// The navigator's look: framed boxes, a tree drawn with guides, a selected
// row with a bar, and a preview that shows a pane's screen in its own
// colours, without letting anything but those colours through.
//
// How these could pass wrongly, written down first:
//   - A coloured preview could be the coloured row of the list, or the
//     search line, and not the preview. Each colour check reads only the
//     cells right of the list box's right border.
//   - The red could come from the theme or the chrome and not from the pane.
//     No theme is on, so the pane's red reaches the host as SGR 31, index 1,
//     and the chrome is drawn in RGB or in other slots.
//   - The current session's preview could be read from the load and the
//     other session's from the client, so one path is never run. The test
//     reads one pane of each.
//   - A link or a clipboard write could reach the host on a frame the test
//     did not look at. The host copy holds every byte tuios wrote, from the
//     start, and it is read after the preview is on screen.
//   - The guides could be drawn on rows of the preview and not the list. The
//     guide check reads the list box's columns only.
//
// Negative controls, all confirmed red (see NEGATIVE_CONTROLS.md).

// runShown types command into a pane and waits for its screen to hold want.
func runShown(t *testing.T, base, sess, window, command, want string) {
	t.Helper()
	if o, err := tuiosCLI(t, base, "send-text", "-s", sess, "-w", window, command); err != nil {
		t.Fatalf("send-text: %v\n%s", err, o)
	}
	if o, err := tuiosCLI(t, base, "send-keys", "-s", sess, "-w", window, "Enter"); err != nil {
		t.Fatalf("send-keys: %v\n%s", err, o)
	}
	deadline := time.Now().Add(uiTimeout)
	for time.Now().Before(deadline) {
		out, _ := tuiosCLI(t, base, "capture-pane", "-s", sess, "-w", window)
		for l := range strings.SplitSeq(out, "\n") {
			if strings.TrimSpace(l) == want || strings.HasSuffix(strings.TrimSpace(l), want) && !strings.Contains(l, "printf") {
				return
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("%s in %s never printed %s", window, sess, want)
}

// navLookOpen opens the navigator once it lists the session on build. The
// client learns of the other machine's sessions a moment after the link is
// up, and the navigator lists what the client holds when it opens.
func navLookOpen(t *testing.T, term *tuitest.Terminal) {
	t.Helper()
	deadline := time.Now().Add(uiTimeout)
	for {
		openNavigator(t, term, "home", "work")
		if term.WaitFor(func(s tuitest.Screen) bool { return strings.Contains(s.Text(), "far-shell") }, 2*time.Second) == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the navigator never listed far-shell\n%s", term.Snapshot())
		}
		sendKeys(t, term, tuitest.Esc)
		time.Sleep(500 * time.Millisecond)
	}
}

// navLookWait waits for every marker, then for the screen to settle.
func navLookWait(t *testing.T, term *tuitest.Terminal, what string, markers ...string) {
	t.Helper()
	waitScreen(t, term, what, markers...)
	if err := term.WaitStable(uiTimeout); err != nil {
		t.Fatalf("the screen never settled: %v", err)
	}
}
