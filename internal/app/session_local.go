package app

import (
	"github.com/Gaurav-Gosain/tuios/internal/config"
	"github.com/Gaurav-Gosain/tuios/internal/layout"
	"github.com/Gaurav-Gosain/tuios/internal/session"
	"github.com/Gaurav-Gosain/tuios/internal/sessiontree"
)

// Switching sessions and making panes and sessions: the paths every surface
// (the rail, the switcher, the palette, the keys) ends in.

// applyStartupToUnarranged applies [startup] tiled to the session the client
// just switched to, when nobody has arranged it yet. A session the new-session
// verb or tuios new --detach made is one: the daemon built it with tiling off
// and no client has placed its windows. A session somebody laid out keeps the
// layout it has. See stateUnarranged.
//
// It is the one place a client applies [startup] after a switch or a create.
// Every path that lands the client on a session, new or not, ends here, so a
// session made from one surface cannot come up laid out differently from the
// same session made from another.
func (m *OS) applyStartupToUnarranged() {
	if m.sessionUnarranged {
		m.applyStartupTiling()
	}
}

// OpenNewSessionPicker makes a new session and attaches to it.
func (m *OS) OpenNewSessionPicker() {
	if m.learnOff(learnNoteSessions) {
		return
	}
	if !m.CanCreateSession() {
		m.ShowNotification("Sessions need the daemon", "info", m.Settings.NotificationDuration)
		return
	}
	m.SidebarNewSession()
}

// CancelPendingSplit forgets a split that was recorded for a window that is
// not going to arrive.
func (m *OS) CancelPendingSplit() {
	m.pendingSplitDir = layout.PreselectionNone
	m.pendingSplitTarget = ""
}

// NewWindowHere is what every "make me a pane" gesture calls: the key, the
// prefix key, the rail's "+", and the palette.
func (m *OS) NewWindowHere() {
	m.AddWindow("")
}

// rebuildForSessionOn is rebuildForSession for a connection that is not the
// one the panes were subscribed on. The old connection is already closed, so
// the panes are closed without unsubscribing them from a daemon that no
// longer hears this client.
func (m *OS) rebuildForSessionOn(state *session.SessionState, savedWidth, savedHeight int) {
	for _, w := range m.Windows {
		w.Close()
	}
	m.Windows = nil
	m.rebuildForSession(state, savedWidth, savedHeight)
}

// clientCapabilities is this client's terminal, as the daemon wants it told.
func (m *OS) clientCapabilities() *session.ClientCapabilities {
	return ClientCapabilitiesOf(m.hostCaps())
}

// ClientCapabilitiesOf is a host terminal's capabilities in the form the hello
// hands them to the daemon. It returns nil for nil.
func ClientCapabilitiesOf(caps *HostCapabilities) *session.ClientCapabilities {
	if caps == nil {
		return nil
	}
	return &session.ClientCapabilities{
		PixelWidth:    caps.PixelWidth,
		PixelHeight:   caps.PixelHeight,
		CellWidth:     caps.CellWidth,
		CellHeight:    caps.CellHeight,
		KittyGraphics: caps.KittyGraphics,
		SixelGraphics: caps.SixelGraphics,
		TerminalName:  caps.TerminalName,
		// The daemon answers a frame edit a host cannot make, so the
		// refusal reaches the guest in order. See Session.SetKittyAnimation.
		KittyAnimation: caps.KittyAnimation,
		// Read from the process's settings: the hello is built before
		// any OS exists. See drawsSymbols.
		SymbolImages: drawsSymbols(config.Global.ImageSymbols, caps),
		Term:         caps.Term,
	}
}

// switchSession attaches the session name over the current connection.
func (m *OS) switchSession(name string) error {
	if m.sessionSwitchHook != nil {
		return m.sessionSwitchHook("", name)
	}
	return m.SwitchToSession(name)
}

// lastSession is the session this client left on its last switch.
type lastSession struct {
	name string
}

// leftSession records the session this client was on before a switch that
// landed, for last_session. It is called from SwitchToSession, where every
// switch ends, so a switch from the Inbox or into a session just made counts
// as well as one from the rail. The workspace history belongs to the session
// left, so it goes.
func (m *OS) leftSession(from lastSession) {
	m.lastWorkspace = 0
	if from.name == "" || from.name == m.SessionName {
		return
	}
	m.lastSession = from
}

// LastSession goes back to the session this client was on before the current
// one, as tmux's switch-client -l does.
func (m *OS) LastSession() {
	if m.lastSession.name == "" {
		m.ShowNotification("No last session", "info", m.Settings.NotificationDuration)
		return
	}
	m.openSession(m.lastSession.name)
}

// openSession is switchSession for a surface: it reports a failure itself and
// says whether the client is now on the session.
func (m *OS) openSession(name string) bool {
	err := m.switchSession(name)
	if err == nil {
		m.applyStartupToUnarranged()
		return true
	}
	m.reportSwitchFailure(err)
	return false
}

// OpenOrCreateSession switches to the session name. The daemon makes the
// session when no session has the name. It is the session switcher's Enter
// on a name that matches no row, and it runs openSession, so [startup]
// applies to the new session as it does to every other switch. It reports a
// failure itself.
func (m *OS) OpenOrCreateSession(name string) bool {
	return m.openSession(name)
}

// OpenSessionNode switches to the session a tree node names, and reports a
// failure itself. The switcher and the palette reach openSession through it.
// A node that is not a session opens nothing.
func (m *OS) OpenSessionNode(n sessiontree.Node) bool {
	if n.Kind != sessiontree.KindSession || n.ID == "" {
		return false
	}
	return m.openSession(n.ID)
}

// SetSessionSwitchHookForTest makes switchSession hand the name it resolved
// to fn instead of switching. Tests outside this package use it to see where a
// surface sends a switch. Production code never calls it.
func (m *OS) SetSessionSwitchHookForTest(fn func(host, name string) error) {
	m.sessionSwitchHook = fn
}

// reportSwitchFailure shows why a switch did not happen.
func (m *OS) reportSwitchFailure(err error) {
	m.ShowNotification("Switch failed: "+err.Error(), "error", m.Settings.NotificationDuration*2)
}

// focusSwitchTarget focuses the workspace or the pane a switch was asked to
// end on. Zero and empty focus nothing.
func (m *OS) focusSwitchTarget(workspace int, paneID string) {
	if workspace > 0 && workspace != m.CurrentWorkspace {
		m.SwitchToWorkspace(workspace)
	}
	if paneID != "" {
		m.focusWindowByID(paneID)
	}
}

// clientVersionForReconnect is the version string to introduce this client
// with. It comes from the connection that is going away, which is the same
// build, so a dead connection still answers it.
func (m *OS) clientVersionForReconnect() string {
	if m.DaemonClient == nil {
		return ""
	}
	return m.DaemonClient.ClientVersion()
}

// ownLayoutReserveForReconnect is the chrome this client keeps for itself, as
// the daemon was last told.
func (m *OS) ownLayoutReserveForReconnect() session.LayoutReserve {
	if m.DaemonClient == nil {
		return session.LayoutReserve{}
	}
	return m.DaemonClient.OwnLayoutReserve()
}
