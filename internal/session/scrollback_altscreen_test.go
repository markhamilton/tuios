package session

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"tuios.dev/vt"
	"tuios.dev/vt/snapshot"
)

// A pane on the alternate screen at the save, a full-screen program such as
// Claude Code, keeps that screen as a picture: the history file holds it beside
// the shell's history, and a restore lays it under the history as frozen rows
// with a dim label above it and the divider under it.

// altPane is an 80x8 emulator holding shell history and then a program on the
// alternate screen with a blank row inside its screen, a styled row, and blank
// rows under its last row.
func altPane(t *testing.T) vt.Terminal {
	t.Helper()
	src := vt.NewWithScrollback(80, 8, 1000)
	var in strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&in, "shell-%02d\r\n", i)
	}
	in.WriteString("$ prog\r\n")
	in.WriteString("\x1b[?1049h\x1b[H")
	in.WriteString("ALT-TOP\r\n\r\n\x1b[1;31mALT-RED\x1b[0m\r\n")
	in.WriteString("\x1b[6;1HALT-BOTTOM")
	if _, err := src.Write([]byte(in.String())); err != nil {
		t.Fatal(err)
	}
	if !src.IsAltScreen() {
		t.Fatal("the source never entered the alternate screen")
	}
	return src
}

const testAltLabel = "-- tuios: the program screen as it was at Sep 30 14:02 --"

// rowWhere is the index of the first row that starts with prefix, or -1.
func rowWhere(rows []string, prefix string) int {
	for i, r := range rows {
		if strings.HasPrefix(r, prefix) {
			return i
		}
	}
	return -1
}

// cellOfRow is the cell at column x of row (history rows first, then the
// screen), or nil.
func cellOfRow(term vt.Terminal, row, x int) *uv.Cell {
	if row < term.ScrollbackLen() {
		line := term.ScrollbackLine(row)
		if x < len(line) {
			return &line[x]
		}
		return nil
	}
	return term.CellAt(x, row-term.ScrollbackLen())
}

func TestHistoryCaptureOnAltScreenHoldsBothScreens(t *testing.T) {
	r := snapshot.CaptureHistory(altPane(t), 1000)
	alt := r.AltState()
	if alt == nil {
		t.Fatal("no program screen read from a pane on the alternate screen")
	}
	cp := *alt
	if err := cp.Unpack(); err != nil {
		t.Fatal(err)
	}
	text := func(rows [][]CellState) string {
		var b strings.Builder
		for _, row := range rows {
			for _, c := range row {
				b.WriteString(c.Content)
			}
			b.WriteByte('\n')
		}
		return b.String()
	}
	if got := text(cp.Screen); !strings.Contains(got, "ALT-TOP") || !strings.Contains(got, "ALT-BOTTOM") {
		t.Errorf("program screen lacks its rows:\n%s", got)
	}
	main := r.State(r.Len())
	if err := main.Unpack(); err != nil {
		t.Fatal(err)
	}
	got := text(append(main.Scrollback, main.Screen...))
	if !strings.Contains(got, "shell-01") || !strings.Contains(got, "$ prog") {
		t.Errorf("main history lacks the shell's rows:\n%s", got)
	}
	if strings.Contains(got, "ALT-") {
		t.Errorf("the program's screen leaked into the main history:\n%s", got)
	}
}

func TestHistoryRestoreOrderIsHistoryScreenDividerShell(t *testing.T) {
	h := throughDisk(t, altPane(t), 1000)
	if h.AltScreen == nil {
		t.Fatal("the program screen did not survive the disk")
	}
	dst := newRestoredEmulator(80, 8, 1000, "/tmp/x", h)
	if dst.IsAltScreen() {
		t.Fatal("the restored pane is on the alternate screen")
	}
	rows := allRows(dst)
	joined := strings.Join(rows, "\n")
	first, prog := indexOf(rows, "shell-01"), indexOf(rows, "$ prog")
	label := indexOf(rows, testAltLabel)
	top, red, bottom := indexOf(rows, "ALT-TOP"), indexOf(rows, "ALT-RED"), indexOf(rows, "ALT-BOTTOM")
	divider := rowWhere(rows, "-- tuios: restored from ")
	if first < 0 || !(first < prog && prog < label && label < top && top < red && red < bottom && bottom < divider) {
		t.Fatalf("want history, label, screen, divider in that order; got first=%d prog=%d label=%d top=%d red=%d bottom=%d divider=%d\n%s",
			first, prog, label, top, red, bottom, divider, joined)
	}
	if label != prog+1 || top != label+1 {
		t.Errorf("the label is not right between the history and the screen:\n%s", joined)
	}
	// The blank row inside the program's screen stays; the blank rows under
	// its last row do not.
	if rows[top+1] != "" || red != top+2 {
		t.Errorf("the blank row inside the screen is lost:\n%s", joined)
	}
	if divider != bottom+1 {
		t.Errorf("blank rows between the screen and the divider:\n%s", joined)
	}
	if y := dst.CursorPosition().Y + dst.ScrollbackLen(); y != divider+1 {
		t.Errorf("cursor on row %d, want %d (under the divider)", y, divider+1)
	}
	if c := cellOfRow(dst, red, 0); c == nil || c.Style.Attrs&uv.AttrBold == 0 || c.Style.Fg != ansi.BasicColor(1) {
		t.Errorf("the program's styled cell lost its style: %+v", c)
	}
	col := strings.Index(rows[label], "tuios")
	if c := cellOfRow(dst, label, col); c == nil || c.Style.Attrs&uv.AttrFaint == 0 {
		t.Errorf("the label is not dim: %+v", c)
	}
}

// A narrower pane: the label wraps, and every row still comes back in order.
func TestHistoryProgramScreenAtANarrowWidth(t *testing.T) {
	src := vt.NewWithScrollback(20, 6, 1000)
	_, _ = src.Write([]byte("before\r\n$ prog\r\n\x1b[?1049h\x1b[HNARROW-ALT\x1b[4;1H" + strings.Repeat("z", 20)))
	h := throughDisk(t, src, 1000)
	for _, width := range []int{20, 12, 60} {
		dst := newRestoredEmulator(width, 6, 1000, "", h)
		joined := strings.Join(allRows(dst), "")
		at := func(s string) int { return strings.Index(joined, s) }
		if !(at("before") < at("$ prog") && at("$ prog") < at("-- tuios: the progr") &&
			at("-- tuios: the progr") < at("NARROW-ALT") && at("NARROW-ALT") < at("zzzz") && at("zzzz") < at("-- tuios: restored")) || at("before") < 0 {
			t.Errorf("width %d: rows out of order:\n%s", width, strings.Join(allRows(dst), "\n"))
		}
	}
}

// A pane on the main screen saves and restores as it did: no program screen,
// no label.
func TestHistoryMainScreenPaneHasNoProgramScreen(t *testing.T) {
	src := vt.NewWithScrollback(40, 6, 1000)
	_, _ = src.Write([]byte("plain\r\n$ "))
	if r := snapshot.CaptureHistory(src, 1000); r.AltState() != nil {
		t.Fatal("a pane on the main screen read a program screen")
	}
	// A program that left the alternate screen before the save leaves none.
	_, _ = src.Write([]byte("\x1b[?1049hGONE\x1b[?1049l"))
	h := throughDisk(t, src, 1000)
	if h.AltScreen != nil {
		t.Fatal("a program that quit before the save left a screen")
	}
	rows := allRows(newRestoredEmulator(40, 6, 1000, "", h))
	if rowWhere(rows, "-- tuios: the program screen") >= 0 || indexOf(rows, "GONE") >= 0 {
		t.Errorf("a main-screen pane came back with a program screen:\n%s", strings.Join(rows, "\n"))
	}
	if indexOf(rows, "plain") < 0 {
		t.Errorf("history lost:\n%s", strings.Join(rows, "\n"))
	}
}

// A program whose screen is blank saves no screen.
func TestHistoryBlankProgramScreenIsNotSaved(t *testing.T) {
	src := vt.NewWithScrollback(40, 6, 1000)
	_, _ = src.Write([]byte("x\r\n\x1b[?1049h"))
	if r := snapshot.CaptureHistory(src, 1000); r.AltState() != nil {
		t.Error("a blank program screen was read")
	}
}

// A resumed agent that takes the alternate screen draws over nothing restored,
// and when it quits the restored rows are there once, as they were.
func TestHistoryResumedProgramDoesNotDuplicateTheScreen(t *testing.T) {
	h := throughDisk(t, altPane(t), 1000)
	dst := newRestoredEmulator(80, 8, 1000, "", h)
	before := allRows(dst)
	_, _ = dst.Write([]byte("$ claude --resume x\r\n\x1b[?1049h\x1b[HRESUMED-AGENT"))
	if !dst.IsAltScreen() {
		t.Fatal("the resumed program did not take the alternate screen")
	}
	for y := range dst.Height() {
		if r := rowText(screenRow(dst, y)); strings.HasPrefix(r, "ALT-") || strings.HasPrefix(r, "-- tuios") {
			t.Errorf("restored row %q shows on the resumed program's screen", r)
		}
	}
	_, _ = dst.Write([]byte("\x1b[?1049l"))
	rows := allRows(dst)
	count := func(s string) (n int) {
		for _, r := range rows {
			if r == s {
				n++
			}
		}
		return n
	}
	for _, s := range []string{"ALT-TOP", "ALT-BOTTOM", testAltLabel, "shell-01"} {
		if n := count(s); n != 1 {
			t.Errorf("%q shows %d times after the program quit, want once:\n%s", s, n, strings.Join(rows, "\n"))
		}
	}
	if indexOf(rows, "RESUMED-AGENT") >= 0 {
		t.Errorf("the resumed program's screen stayed after it quit")
	}
	if indexOf(before, "ALT-BOTTOM") != indexOf(rows, "ALT-BOTTOM") {
		t.Errorf("the restored rows moved: row %d before, %d after", indexOf(before, "ALT-BOTTOM"), indexOf(rows, "ALT-BOTTOM"))
	}
}

// Through the session's saver and the daemon's restore.
func TestDaemonRestoreShowsTheProgramScreen(t *testing.T) {
	defer useResurrectionDir(t.TempDir())()
	pol := ResolveHistoryPolicy(nil, 0, 0)
	s, state := historyTestSession(t, "altd", pol, map[string]string{
		"win-1": "OLD-LINE\r\n$ claude\r\n\x1b[?1049h\x1b[HCLAUDE-SCREEN-MARK\x1b[5;1H> input box",
	})
	s.saveHistory(state, true)
	h := loadHistory("altd")["win-1"]
	if h == nil || h.AltScreen == nil {
		t.Fatalf("the saver did not keep the program screen: %+v", h)
	}

	d := NewDaemon(&DaemonConfig{History: pol})
	defer d.manager.Shutdown()
	sess, err := d.restoreSession(&SessionState{Name: "altd", Width: 80, Height: 24,
		Windows: []WindowState{{ID: "win-1", Width: 80, Height: 24}}})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	pty := sess.GetPTY(sess.GetState().Windows[0].PTYID)
	if pty == nil {
		t.Fatal("no pane")
	}
	pty.terminalMu.RLock()
	rows := allRows(pty.terminal)
	alt := pty.terminal.IsAltScreen()
	pty.terminalMu.RUnlock()
	joined := strings.Join(rows, "\n")
	if alt {
		t.Error("the restored pane is on the alternate screen")
	}
	old, label := indexOf(rows, "OLD-LINE"), rowWhere(rows, "-- tuios: the program screen")
	mark, box := indexOf(rows, "CLAUDE-SCREEN-MARK"), indexOf(rows, "> input box")
	divider := rowWhere(rows, "-- tuios: restored from ")
	if old < 0 || !(old < label && label < mark && mark < box && box < divider) {
		t.Fatalf("want history, label, screen, divider; got old=%d label=%d mark=%d box=%d divider=%d\n%s",
			old, label, mark, box, divider, joined)
	}
}

// The byte bound trims the oldest history rows and keeps the program screen.
func TestHistoryBoundKeepsTheProgramScreen(t *testing.T) {
	defer useResurrectionDir(t.TempDir())()
	pol := ResolveHistoryPolicy(nil, 5000, 16) // 16 KiB a pane
	s, state := historyTestSession(t, "altb", pol, map[string]string{
		"win": manyLines("row", 4000) + "$ prog\r\n\x1b[?1049h\x1b[HALT-KEPT\x1b[6;1HALT-LAST",
	})
	s.saveHistory(state, true)
	fi, err := os.Stat(testHistoryPath("altb", "win"))
	if err != nil {
		t.Fatalf("not saved: %v", err)
	}
	if fi.Size() > 16<<10 {
		t.Errorf("pane file is %d bytes, over its 16 KiB bound", fi.Size())
	}
	h := loadHistory("altb")["win"]
	if h == nil || h.AltScreen == nil {
		t.Fatalf("the bounded file lost the program screen: %+v", h)
	}
	if n := packedScrollbackRows(t, h.State); n == 0 || n >= 4000 {
		t.Errorf("kept %d history rows, want some but not all", n)
	}
	rows := allRows(newRestoredEmulator(40, 6, 5000, "", h))
	newest := strings.TrimRight(fmt.Sprintf("row-%05d %s", 3999, strings.Repeat("x", 3999%31)), " ")
	if indexOf(rows, newest) < 0 || indexOf(rows, "row-00000") >= 0 {
		t.Errorf("want the newest rows kept and the oldest gone")
	}
	if indexOf(rows, "ALT-KEPT") < 0 || indexOf(rows, "ALT-LAST") < 0 {
		t.Errorf("the program screen did not come back:\n%s", strings.Join(rows[len(rows)-12:], "\n"))
	}
}

func TestHistoryOffSavesNoProgramScreen(t *testing.T) {
	dir := t.TempDir()
	defer useResurrectionDir(dir)()
	s, state := historyTestSession(t, "altoff", HistoryPolicy{}, map[string]string{"win": "x\r\n\x1b[?1049hSECRET"})
	s.saveHistory(state, true)
	if _, err := os.Stat(filepath.Join(dir, historyDirName)); !os.IsNotExist(err) {
		t.Errorf("history saved with the setting off: %v", err)
	}
}

// savedHistoryBeforeAltScreen is the history record as builds before the
// program screen wrote and read it.
type savedHistoryBeforeAltScreen struct {
	Version int
	SavedAt time.Time
	State   *TerminalState
}

func gzipGob(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := gob.NewEncoder(zw).Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A file from before the program screen loads here, and a file from here loads
// in a build from before it, without the screen.
func TestHistoryFormatReadsBothWays(t *testing.T) {
	src := vt.NewWithScrollback(40, 6, 1000)
	_, _ = src.Write([]byte("OLD-FORMAT\r\n$ "))
	old := gzipGob(t, &savedHistoryBeforeAltScreen{Version: historyVersion, SavedAt: time.Now(), State: historyStateOf(src, 1000)})
	h, err := decodeHistory(bytes.NewReader(old))
	if err != nil {
		t.Fatalf("an older file does not load: %v", err)
	}
	if h.AltScreen != nil {
		t.Error("an older file read as holding a program screen")
	}
	if rows := allRows(newRestoredEmulator(40, 6, 1000, "", h)); indexOf(rows, "OLD-FORMAT") < 0 {
		t.Errorf("an older file's history did not come back:\n%s", strings.Join(rows, "\n"))
	}

	// The other way: what an older build's decoder does with a new file.
	r := snapshot.CaptureHistory(altPane(t), 1000)
	data, err := encodeHistory(&savedHistory{Version: historyVersion, SavedAt: time.Now(), State: r.State(r.Len()), AltScreen: r.AltState()})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var back savedHistoryBeforeAltScreen
	if err := gob.NewDecoder(zr).Decode(&back); err != nil {
		t.Fatalf("an older build cannot read a new file: %v", err)
	}
	if back.Version != historyVersion || back.State == nil || back.State.Width != 80 {
		t.Errorf("an older build reads a new file wrong: %+v", back)
	}
}

// A program screen that is not of the main screen's size is dropped, and the
// history it came with still loads.
func TestHistoryMismatchedProgramScreenIsDropped(t *testing.T) {
	r := snapshot.CaptureHistory(altPane(t), 1000)
	alt := r.AltState()
	alt.Width = 41
	data, err := encodeHistory(&savedHistory{Version: historyVersion, SavedAt: time.Now(), State: r.State(r.Len()), AltScreen: alt})
	if err != nil {
		t.Fatal(err)
	}
	h, err := decodeHistory(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the history was refused with the bad screen: %v", err)
	}
	if h.AltScreen != nil {
		t.Error("a program screen of another size was kept")
	}
}
