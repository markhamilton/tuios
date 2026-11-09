package tuie2e

import (
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// TestNarrowClientKeepsTheDesktopLayout is the phone case on a real daemon: a
// floating session with two panes is attached from a 160-column desktop, then
// from a 60-column phone (which detaches the desktop), and then from the
// desktop again. The phone tiles the panes for itself, and moves the focus so
// it pushes its state, but the session stays floating and every pane keeps
// the rectangle the desktop gave it, through the phone's attach and after it.
//
// Negative controls: with narrow_width = 0 the phone's panes are pushed at
// 60 columns and the check with the phone attached fails. Without the
// latestDisplaced call in handleAttach the session keeps the phone's 60x30
// for the desktop, which clamps its floating panes into it, and the last
// check fails.
func TestNarrowClientKeepsTheDesktopLayout(t *testing.T) {
	const name = "narrowdesk"
	base := t.TempDir()
	killDaemon(t, base)
	writeConfig(t, base, "[startup]\nopen_default_window = true\n\n[keybindings]\npreset = \"tmux\"\n")
	if out, err := tuiosCLI(t, base, "new", name, "--detach"); err != nil {
		t.Fatalf("create %s: %v\n%s", name, err, out)
	}
	attachIn(t, base, name, startOpts{cols: 160, rows: 48})
	clientShows(t, base, name)
	if out, err := tuiosCLI(t, base, "new-window", "second", "-s", name); err != nil {
		t.Fatalf("new-window: %v\n%s", err, out)
	}
	deadline := time.Now().Add(uiTimeout)
	for len(listPanes(t, base, name)) < 2 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(time.Second)
	desk := listPanes(t, base, name)
	if len(desk) != 2 {
		t.Fatalf("the session has %d panes, want 2", len(desk))
	}
	if info := sessionInfoOf(t, base, name); info.TilingMode != "floating" {
		t.Fatalf("the session starts %q, want floating", info.TilingMode)
	}

	same := func(when string) {
		t.Helper()
		now := listPanes(t, base, name)
		for id, want := range desk {
			got, ok := now[id]
			if !ok {
				t.Fatalf("%s: pane %s is gone", when, id)
			}
			if got.X != want.X || got.Y != want.Y || got.Width != want.Width || got.Height != want.Height {
				t.Errorf("%s: pane %s is at %d,%d %dx%d, want the desktop's %d,%d %dx%d",
					when, id[:8], got.X, got.Y, got.Width, got.Height, want.X, want.Y, want.Width, want.Height)
			}
		}
		if info := sessionInfoOf(t, base, name); info.TilingMode != "floating" {
			t.Errorf("%s: the session is %q, want floating", when, info.TilingMode)
		}
	}

	// Attaches as shipped: the desktop is detached.
	phone := attachSmall(t, base, name, startOpts{cols: 60, rows: 30, detachOthers: true})
	clientShows(t, base, name)
	// Two focus moves, each a push from the phone.
	for range 2 {
		if err := phone.SendKeys(tuitest.Ctrl('b'), "o"); err != nil {
			t.Fatalf("press ctrl+b o: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	time.Sleep(time.Second)
	same("with the phone attached")

	// The desktop attaches while the phone is still there, which detaches the
	// phone: the session takes the desktop's size at once.
	attachSmall(t, base, name, startOpts{cols: 160, rows: 48, detachOthers: true})
	clientShows(t, base, name)
	time.Sleep(time.Second)
	same("after the desktop attached again")
}
