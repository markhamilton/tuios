package session

import (
	"os"
	"path/filepath"
)

// The folder a pane without admin is held to: a window it opens starts there,
// and what is typed into a pane whose folder is wider is refused.

// paneFolder is the folder the files grant holds a pane to: its session's
// worktree when tuios made it, else the folder the pane started in. Both are
// fixed by the daemon. The pane's working folder is not used: the pane moves
// it with cd, or with an OSC 7 it prints, so a pane could name / as its
// folder. A detected worktree is not used either, since it follows the first
// pane's folder. It is "" when there is no fixed folder, and for the home
// folder or any folder above it, and then the copy is refused.
func (d *Daemon) paneFolder(pa *paneAuth) string {
	return d.windowFolder(pa.window)
}

// windowFolder is paneFolder for a window id.
func (d *Daemon) windowFolder(windowID string) string {
	sess := d.sessionHoldingWindow(windowID)
	if sess == nil {
		return ""
	}
	dir := ""
	if wt := sess.Worktree(); wt != nil && wt.Managed && wt.Path != "" {
		dir = filepath.Clean(wt.Path)
	} else {
		dir = sess.windowStartDir(windowID)
	}
	if dir == "" {
		return ""
	}
	// The folder is used as it was recorded, resolved. One that is now a
	// link, or that resolves elsewhere, was replaced after it was recorded,
	// and is no folder: following it would carry the grant to wherever the
	// link points.
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() || realPath(dir) != dir {
		return ""
	}
	if tooWideFolder(dir) || !ownedByDaemonUser(dir) {
		return ""
	}
	return dir
}

// tooWideFolder reports whether dir holds the home folder: a file system
// root, /home or /Users, or the home folder itself. A grant held to one of
// those reaches the keys and every project, which is no folder at all. It
// is true when the home folder cannot be found. It takes a resolved path
// and keeps no result.
func tooWideFolder(dir string) bool {
	if filepath.Dir(dir) == dir {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// With no home folder to compare, nothing is known to be narrow
		// enough.
		return true
	}
	real := realPath(dir)
	return pathUnder(realPath(filepath.Clean(home)), real)
}

// noFolderReason is the refusal of a copy from a pane with no folder the
// files grant can reach.
const noFolderReason = "the pane has no folder the files grant can reach. It reaches the folder a pane started in, or its session's tuios worktree, " +
	"and never the home folder, a folder above it, or a folder another user owns. Start the pane in a project folder or a worktree, then give it files"

// childWindowBound holds a window that cs asks for to the caller's folder,
// when cs is a pane without admin. cwd is the folder the call names, "" for
// none. An explicit folder outside the caller's folder is refused. bounded
// says the window must be held (NewWindowOptions.Bounded), and bound is the
// folder ("" when the caller has none).
func (d *Daemon) childWindowBound(cs *connState, verb, cwd string) (bound string, bounded bool, verr *verbError) {
	pa := d.paneAuthority(cs)
	if pa == nil || pa.grants.Has(GrantAdmin) {
		return "", false, nil
	}
	bound = d.paneFolder(pa)
	if cwd != "" && (bound == "" || !pathUnder(realPath(cwd), bound)) {
		where := "this pane has no folder of its own"
		if bound != "" {
			where = "this pane's folder is " + echoName(bound)
		}
		return "", true, grantForbidden(verb, pa, echoName(cwd)+" is outside the folder of the pane that asks, and "+where+
			". A pane without admin starts windows only inside its own folder")
	}
	return bound, true, nil
}

// addWindowFor makes a window that cs asked for with verb, held to the
// caller's folder when cs is a pane without admin (childWindowBound). Every
// verb a pane may reach that starts a process in a folder of its choosing
// goes through it, so a pane cannot start a window, which may inherit its
// files grant, in a folder wider than its own.
func (d *Daemon) addWindowFor(cs *connState, verb string, sess *Session, opts NewWindowOptions, onExit func(ptyID string)) (WindowState, *verbError, error) {
	bound, bounded, verr := d.childWindowBound(cs, verb, opts.Cwd)
	if verr != nil {
		return WindowState{}, verr, nil
	}
	opts.Bounded, opts.Bound = bounded, bound
	win, err := sess.AddDaemonWindowWith(opts, onExit)
	return win, nil, err
}
