package tuie2e

import (
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// A panel opened over one pane that fills the screen. That pane is drawn by
// the fullscreen fast path, which skips the compositor and every overlay with
// it unless the panel turns the path off. A panel that did not stayed open
// and took every key while nothing of it was drawn.
//
// How these could pass wrongly, written down first:
//   - The fixture might not be on the fast path at all: a second pane, the
//     rail or a floating pane takes the path away and draws every panel. Each
//     test checks the pane's border spans the whole width before it opens
//     anything.
//   - The panel could be drawn by a panel under it. The editors open over
//     Settings, which turns the path off on its own, so Settings is sent away
//     first and the test waits for its footer to go.
//   - Waiting for text the pane or the dock already shows passes on a blank
//     panel. Each marker is text only that panel draws.
//   - Esc could close the panel and leave the keyboard with something else.
//     A command the shell has to compute is run after it.

// fullscreenFastPathOverlays lists the panels: how to leave each one up and
// alone over the pane, and text only it draws.
var fullscreenFastPathOverlays = []struct {
	name  string
	open  func(t *testing.T, term *tuitest.Terminal)
	shows string
}{
	{"copies", func(t *testing.T, term *tuitest.Terminal) {
		if err := term.SendKeys(tuitest.Ctrl('b'), "u"); err != nil {
			t.Fatal(err)
		}
	}, "No copies."},
	{"glyph picker", openOverSettings("glyph set"), "↑↓ preview"},
	{"dock editor", openOverSettings("components"), "Dock layout"},
	{"section editor", openOverSettings("sections"), "Rail sections"},
}

// openOverSettings opens a picker the way a person reaches it: the palette
// finds its Settings row, Enter opens it over Settings. A click on Settings
// raises it and a click on the pane outside both dismisses it, the front
// panel, which leaves the picker alone.
func openOverSettings(query string) func(t *testing.T, term *tuitest.Terminal) {
	return func(t *testing.T, term *tuitest.Terminal) {
		t.Helper()
		const settingsFooter = "tab section"
		if err := term.SendKeys(tuitest.Ctrl('p')); err != nil {
			t.Fatal(err)
		}
		waitPaletteOpen(t, term, "for "+query)
		if err := term.SendKeys(query, tuitest.Enter); err != nil {
			t.Fatal(err)
		}
		if err := term.WaitForText(settingsFooter, uiTimeout); err != nil {
			t.Fatalf("the palette did not open Settings on %q: %v\n%s", query, err, term.Snapshot())
		}
		if err := term.SendKeys(tuitest.Enter); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		col, row := screenCell(t, term, "Settings")
		mouseClick(t, term, col, row, tuitest.MouseLeft, 0)
		time.Sleep(insertGuard)
		mouseClick(t, term, 1, 20, tuitest.MouseLeft, 0)
		if err := term.WaitFor(func(s tuitest.Screen) bool {
			return !strings.Contains(s.Text(), settingsFooter)
		}, uiTimeout); err != nil {
			t.Fatalf("a click away did not dismiss Settings over the %q picker: %v\n%s", query, err, term.Snapshot())
		}
	}
}

// waitFillsScreen waits for the lone pane's top border to run the screen's
// whole width, which is the shape the fast path draws.
func waitFillsScreen(t *testing.T, term *tuitest.Terminal, when string) {
	t.Helper()
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		cols, _ := s.Size()
		top := []rune(s.Line(0))
		return len(top) >= cols && top[0] == '╭' && top[cols-1] == '╮'
	}, uiTimeout); err != nil {
		t.Fatalf("%s: the pane does not fill the screen: %v\n%s", when, err, term.Snapshot())
	}
}

// shellHasKeys runs a command the shell has to compute, so it passes only
// when the keys reach the pane.
func shellHasKeys(t *testing.T, term *tuitest.Terminal, marker string) {
	t.Helper()
	runInShell(t, term, "echo "+marker+"-$((6*7))", marker+"-42", shellTimeout)
}

// TestPanelsDrawOverALoneFullscreenPane opens each panel over one pane that
// fills the screen, checks it is drawn, closes it with Esc and checks the
// shell has the keyboard back.
func TestPanelsDrawOverALoneFullscreenPane(t *testing.T) {
	for _, tc := range fullscreenFastPathOverlays {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			term := startHome(t, base, "fp")
			waitFillsScreen(t, term, "before "+tc.name)

			tc.open(t, term)
			if err := term.WaitForText(tc.shows, uiTimeout); err != nil {
				t.Fatalf("ASSERTION: the %s is not drawn over a lone fullscreen pane: %v\n%s", tc.name, err, term.Snapshot())
			}
			saveFrame(t, term, "fast-path-"+strings.ReplaceAll(tc.name, " ", "-"))

			if err := term.SendKeys(tuitest.Esc); err != nil {
				t.Fatal(err)
			}
			if err := term.WaitFor(func(s tuitest.Screen) bool {
				return !strings.Contains(s.Text(), tc.shows)
			}, uiTimeout); err != nil {
				t.Fatalf("esc did not close the %s: %v\n%s", tc.name, err, term.Snapshot())
			}
			time.Sleep(insertGuard)
			shellHasKeys(t, term, "BACK")
			waitFillsScreen(t, term, "after "+tc.name)
			alive(t, term, "after the "+tc.name)
		})
	}
}
