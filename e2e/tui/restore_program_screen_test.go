package tuie2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// TestRestoredPaneShowsTheProgramScreen is the screen of a full-screen program
// surviving the daemon, read off the frame a real client renders.
//
// A pane runs a program that takes the alternate screen, draws a marker and
// waits, the way Claude Code does. The daemon is stopped while the program is
// up and a new one restores the session. The frame has to show, in this order:
// the dim label, the program's marker, the divider, and a shell that answers a
// command under it.
//
// The ways it could pass without testing that:
//   - The marker could be the typed command echoed back. The shell computes it
//     with arithmetic, so PROG-42-SCREEN appears only as output.
//   - The pane read could be the one from before the restart. The session is
//     checked to be marked restored first, which only a new daemon does, and
//     the label and the divider are written only by a restore.
func TestRestoredPaneShowsTheProgramScreen(t *testing.T) {
	const session = "e2e-progscreen"
	const marker = "PROG-42-SCREEN"
	artifacts := artifactDir(t)
	base := t.TempDir()
	killDaemon(t, base)

	if out, err := tuiosCLI(t, base, "new", session, "--detach"); err != nil {
		t.Fatalf("create the session: %v\n%s", err, out)
	}
	first := startIn(t, base, startOpts{cols: 120, rows: 40, args: []string{"attach", session}})
	if err := first.WaitFor(func(s tuitest.Screen) bool { return countWindows(s) == 1 }, bootTimeout); err != nil {
		t.Fatalf("the client never attached: %v\n%s", err, first.Snapshot())
	}
	time.Sleep(insertGuard + 150*time.Millisecond)
	runInShell(t, first, `printf '\033[?1049h\033[H'; echo "PROG-$((6*7))-SCREEN"; sleep 600`, marker, shellTimeout)
	saveArtifact(t, first, artifacts, "before-restart")

	// kill-server saves every pane's history on the way out, with the program
	// still on the alternate screen.
	if out, err := tuiosCLI(t, base, "kill-server"); err != nil {
		t.Fatalf("kill-server: %v\n%s", err, out)
	}
	waitExit(t, first, "after kill-server")
	files, _ := filepath.Glob(filepath.Join(xdgDir(base, "XDG_STATE_HOME"), "tuios", "sessions", "scrollback", session, "*.hist.gz"))
	if len(files) != 1 {
		t.Fatalf("want one saved history file, got %v", files)
	}

	if out, err := tuiosCLI(t, base, "new", session+"-trigger", "--detach"); err != nil {
		t.Fatalf("start a fresh daemon: %v\n%s", err, out)
	}
	if info := waitForSessionInfo(t, base, session); !info.Restored {
		t.Fatal("the session is not marked restored, so its pane is not the respawned one")
	}

	second := startIn(t, base, startOpts{cols: 120, rows: 40, args: []string{"attach", session}})
	if err := second.WaitFor(func(s tuitest.Screen) bool {
		return strings.Contains(s.Text(), "restored from") && strings.Contains(s.Text(), marker)
	}, bootTimeout); err != nil {
		t.Fatalf("ASSERTION: the restored pane does not show the program's screen and the divider: %v\n%s", err, second.Snapshot())
	}
	time.Sleep(insertGuard + 150*time.Millisecond)
	runInShell(t, second, `echo "AFTER-$((40+2))-RESTORE"`, "AFTER-42-RESTORE", shellTimeout)
	if err := second.WaitStable(uiTimeout); err != nil {
		t.Fatalf("the screen never settled: %v", err)
	}
	saveArtifact(t, second, artifacts, "after-restart")

	scr := second.Screen()
	_, rows := scr.Size()
	labelRow, markRow, divRow, newRow := -1, -1, -1, -1
	for y := range rows {
		line := scr.Line(y)
		switch {
		case strings.Contains(line, "-- tuios: the program screen as it was at"):
			labelRow = y
		case strings.Contains(line, marker) && !strings.Contains(line, "echo"):
			markRow = y
		case strings.Contains(line, "-- tuios: restored from"):
			divRow = y
		case strings.Contains(line, "AFTER-42-RESTORE") && !strings.Contains(line, "echo"):
			newRow = y
		}
	}
	if labelRow < 0 || !(labelRow < markRow && markRow < divRow && divRow < newRow) {
		t.Fatalf("ASSERTION: want the label, the program's screen, the divider, then the new output; rows label=%d screen=%d divider=%d new=%d\n%s",
			labelRow, markRow, divRow, newRow, second.Snapshot())
	}
	line := scr.Line(labelRow)
	col := strings.Index(line, "-- tuios")
	if c := scr.Cell(col+3, labelRow); !c.Faint {
		t.Errorf("the label is not dim: %+v", c)
	}
	alive(t, second, "after restoring the program screen")
}
