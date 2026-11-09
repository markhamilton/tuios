package tuie2e

import (
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// openSwitcherOn opens the session switcher, filters it to query and presses
// Enter, once the row want is on screen.
func openSwitcherOn(t *testing.T, term *tuitest.Terminal, want, query string) {
	t.Helper()
	time.Sleep(insertGuard)
	if err := term.SendKeys(tuitest.Ctrl('b'), "S"); err != nil {
		t.Fatalf("open the session switcher: %v", err)
	}
	if err := term.WaitForText(want, uiTimeout); err != nil {
		t.Fatalf("ASSERTION: the session switcher never listed %q: %v\n%s", want, err, term.Snapshot())
	}
	if err := term.SendKeys(query); err != nil {
		t.Fatalf("type %q: %v", query, err)
	}
	time.Sleep(300 * time.Millisecond)
	t.Logf("switcher filtered to %q:\n%s", query, term.Snapshot())
	if err := term.SendKeys(tuitest.Enter); err != nil {
		t.Fatalf("enter: %v", err)
	}
}

// noSwitchFailure fails the test if the screen shows a refused switch, which
// is what #196 put on screen: the rail identity sent to a daemon as a name.
func noSwitchFailure(t *testing.T, term *tuitest.Terminal, when string) {
	t.Helper()
	text := term.Screen().Text()
	if strings.Contains(text, "Switch failed") || strings.Contains(text, "path separator") {
		t.Fatalf("ASSERTION: %s the switch was refused:\n%s", when, term.Snapshot())
	}
}
