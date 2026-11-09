package tuie2e

import (
	"os"
	"path/filepath"
	"testing"
)

// The review fixes to stream-pane and attach-presence: the nonce scope of a
// presence, a lease that a silent client stops renewing, lease frames behind
// input that a pane does not read, and the snapshot charged to the read
// budget.
//
// NEGATIVE CONTROLS (e2e/tui/NEGATIVE_CONTROLS.md, "stream-pane review
// fixes"):
//   - with humanNonceFor not checking covers, a presence made for one
//     session answers in another, and TestPresenceNonceScope fails at "a
//     presence for session phone answered in session other".
//   - with the expireLease call left out of paneStream.stream, the silent
//     stream holds its lease, and TestStreamPaneLeaseExpires fails at "the
//     lease of a silent client did not end".
//   - with I frames written to the pane on the goroutine that reads the
//     client, TestStreamPaneLeaseBehindBlockedInput fails at "a lease frame
//     behind input the pane does not read".
//   - with the read budget charge left out of snapshotSubscribe,
//     TestStreamPaneSnapshotCharged fails at "a second snapshot was taken
//     while the first held the budget".

// otherSession is a second session, for the nonce scope.
const otherSession = "other"

// writeReviewArtifact saves a short record of what a test saw under
// TUIOS_E2E_FRAMES, when it is set.
func writeReviewArtifact(t *testing.T, name, body string) {
	t.Helper()
	dir := os.Getenv("TUIOS_E2E_FRAMES")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("artifact dir: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Logf("write artifact: %v", err)
	}
}
