package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Gaurav-Gosain/tuios/internal/session"
)

// attachForce is tuios attach --force: attach even from a pane of the session.
var attachForce bool

// attachDetachOthers is tuios attach -d. Detaching the other clients is the
// default now, so the flag is kept, hidden, so scripts that pass it still
// run. Its one effect left is to win over TUIOS_ATTACH_KEEP_OTHERS. It cannot
// be given together with --keep-others.
var attachDetachOthers bool

// attachKeepOthers is tuios attach -k: leave the session's other clients
// attached, the way attach worked before detaching them became the default.
var attachKeepOthers bool

// attachDetachesOthers reports whether this attach takes the session's other
// clients off it. A person who runs tuios attach (or a bare tuios, or tuios
// new on a session that exists) means to work in this terminal, so the
// default is yes, as tmux attach -d. -k says no. The attach a restart makes
// says no too: every client of the session comes back that way, and each one
// would take the others off in turn. TUIOS_ATTACH_KEEP_OTHERS=1 is -k for
// every attach, for a script or test suite that runs several clients of one
// session side by side.
func attachDetachesOthers() bool {
	if attachDetachOthers {
		return true
	}
	return !attachKeepOthers && !attachAfterRestart && os.Getenv(keepOthersEnv) != "1"
}

// keepOthersEnv names the variable that keeps the other clients attached.
const keepOthersEnv = "TUIOS_ATTACH_KEEP_OTHERS"

// olderDaemonKeepsOthers is the warning for a daemon that cannot detach the
// other clients on attach.
const olderDaemonKeepsOthers = "Warning: the daemon is older than this tuios. It cannot detach the other clients, so they stay attached. Run 'tuios restart-server' and attach again."

// nestedAllowed reports whether this attach may go through from a pane of its
// own session: --force, or TUIOS_ALLOW_NESTED=1.
func nestedAllowed() bool {
	return attachForce || session.NestedAllowedByEnv()
}

// explainNestedAttach words the refusal of an attach from inside the session
// for the command line. A named attach gets the daemon's message. An unnamed
// one (a bare tuios in a pane) did not ask to attach to anything in
// particular, so it is told where it is and what else there is. names are the
// sessions the daemon listed, or nil to ask for them here.
func explainNestedAttach(nested *session.NestedAttachError, names []string) error {
	if !nested.Unnamed {
		return &diagnosticError{What: nested.Error(), Err: nested}
	}
	if names == nil && session.IsDaemonRunning() {
		if client, err := dialVerb(); err == nil {
			if infos, err := listSessionInfos(client); err == nil {
				for _, info := range infos {
					names = append(names, info.Name)
				}
			}
			_ = client.Close()
		}
	}
	var others []string
	for _, n := range names {
		if n != nested.Session {
			others = append(others, n)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are inside session %q.", nested.Session)
	if len(others) > 0 {
		fmt.Fprintf(&b, " Other sessions: %s.", strings.Join(truncateList(others, 12), ", "))
		b.WriteString(" To show one of them in this pane, run 'tuios attach NAME'.")
	} else {
		b.WriteString(" There are no other sessions.")
	}
	b.WriteString(" To start a new session, run 'tuios new NAME'.")
	return &diagnosticError{What: b.String(), Err: nested}
}
