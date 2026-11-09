package tuie2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Gaurav-Gosain/tuitest"
)

// The rail's hierarchy, on screen.
//
// The complaint these are the answer to: a machine's row and a session's row
// sat at the same indent, in the same weight and in the same ink, so the only
// thing separating "this is a computer" from "this is a shell on it" was one
// dim glyph. What a person reads at a glance is weight and position, and the
// rail spent neither.
//
// Every claim here is read off a real client's grid, attributes included. A
// plain snapshot cannot see a heading: bold is what carries the level, and it
// is invisible in text.

// railNameCol is the column a name starts on in a rail line, counting from the
// rail's own first column: the gutter, the glyph, and a cell of air.
const railNameCol = 3

// railNarrowWidth is the narrow rail's width, the least the rail draws names
// at. It mirrors config.SidebarNarrowWidth.
const railNarrowWidth = 16

// nameColOf is the column the given name starts at on its rail row, or -1.
func nameColOf(s tuitest.Screen, row int, name string) int {
	if row < 0 {
		return -1
	}
	line := railLine(s, row)
	before, _, ok := strings.Cut(line, name)
	if !ok {
		return -1
	}
	// Byte offset to cell column: the rail's marks are multi-byte.
	return len([]rune(before))
}

// ruledAfter reports whether a heading's rule runs out of the name on this row.
// It looks for the rule glyph anywhere to the right of where the name ends,
// which is where the heading draws it and where no item row draws anything.
func ruledAfter(s tuitest.Screen, row, fromCol int) bool {
	w, _ := s.Size()
	for col := fromCol; col < w; col++ {
		if s.Cell(col, row).Content == "\u2500" {
			return true
		}
	}
	return false
}

// boldAt reports whether the cell at (col, row) is drawn bold.
func boldAt(s tuitest.Screen, col, row int) bool {
	return s.Cell(col, row).Bold
}

// writeHierarchyConfig names a rail width, one host the stand-in reaches and
// one that cannot be reached.
func writeHierarchyConfig(t *testing.T, base, tuiosPath, remoteCommand string, width int) {
	t.Helper()
	dir := filepath.Join(base, "XDG_CONFIG_HOME", "tuios")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	body := "[appearance.sidebar]\nwidth = " + strconv.Itoa(width) + "\n\n" +
		"[hosts.oci]\n" +
		"addr = \"someone@oci\"\n" +
		"command = \"" + remoteCommand + "\"\n" +
		"connect_timeout = 5\n\n" +
		"[hosts.work]\n" +
		"addr = \"someone@poweredoff\"\n" +
		"command = \"/nonexistent/tuios\"\n" +
		"connect_timeout = 2\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_ = tuiosPath
}

// TestTheRailWithNumbersOn is the opt-in half of the switch numbers: with
// appearance.sidebar.show_numbers on, the session's row carries the number
// switch_session_N opens, and the name steps two cells past the quiet rail's
// name column to make the room.
func TestTheRailWithNumbersOn(t *testing.T) {
	cfg := strings.Replace(railConfig(28), "[appearance.sidebar]\n",
		"[appearance.sidebar]\nshow_numbers = true\n", 1)
	term, _ := railClient(t, "e2e", cfg, startOpts{cols: 120, rows: 30})
	railShows(t, term, "sessions")

	s := term.Screen()
	row := railRowOf(s, "1 e2e")
	if row < 0 {
		t.Fatalf("ASSERTION: the rail with numbers on drew no numbered session row\n%s", term.Snapshot())
	}
	if col := nameColOf(s, row, "e2e"); col != railNameCol+2 {
		t.Errorf("ASSERTION: the numbered session's name starts at column %d, want %d\n%s",
			col, railNameCol+2, term.Snapshot())
	}
	saveFrame(t, term, "hier-after-numbers-on")
	alive(t, term, "after the numbered rail")
}
