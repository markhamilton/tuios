package tuie2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// terminfoEntry reads the compiled terminfo entry for name from the system
// folders, in either layout.
func terminfoEntry(t *testing.T, name string) []byte {
	t.Helper()
	for _, dir := range []string{"/usr/share/terminfo", "/lib/terminfo", "/etc/terminfo", "/usr/lib/terminfo"} {
		for _, sub := range []string{name[:1], fmt.Sprintf("%x", name[0])} {
			if data, err := os.ReadFile(filepath.Join(dir, sub, name)); err == nil {
				return data
			}
		}
	}
	t.Fatalf("no compiled terminfo entry for %s on this machine", name)
	return nil
}

// paneTerms asks session's shell for its TERM and COLORTERM and returns them
// as one "TERM=... COLORTERM=..." string, and the pane's text. Each is printed
// on a line of its own, so a 65 byte TERM still fits the 80 column pane tuios
// new -d makes. The verb cases ask for a wider one, for the 65 byte COLORTERM.
func paneTerms(t *testing.T, base, session string) (got, pane string) {
	t.Helper()
	if out, err := tuiosCLI(t, base, "send-keys", "-s", session, "-l",
		`printf 'TERM=%s END\nCOLORTERM=%s END\n' "$TERM" "$COLORTERM"`+"\r"); err != nil {
		t.Fatalf("ask the shell of %s for its TERM: %v\n%s", session, err, out)
	}
	deadline := time.Now().Add(shellTimeout)
	for {
		pane, _ = tuiosCLI(t, base, "capture-pane", "-s", session)
		var term, colorTerm string
		for line := range strings.SplitSeq(pane, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasSuffix(line, " END") || strings.Contains(line, "%s") {
				continue
			}
			line = strings.TrimSuffix(line, " END")
			switch {
			case strings.HasPrefix(line, "TERM="):
				term = line
			case strings.HasPrefix(line, "COLORTERM="):
				colorTerm = line
			}
		}
		if term != "" && colorTerm != "" {
			return term + " " + colorTerm, pane
		}
		if time.Now().After(deadline) {
			t.Fatalf("the shell of %s never printed its TERM and COLORTERM:\n%s", session, pane)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
