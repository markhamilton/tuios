package app

import (
	"errors"
	"os"
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/Gaurav-Gosain/tuios/internal/session"
)

// The session order is the daemon's (set-session-order), not this client's.
// A move on the rail, by key or by drag, shows at once, as it always did, and
// goes to the daemon, which ranks the sessions, sorts its listing by the
// ranks and pushes the listing to every client. Once the daemon's listing
// carries an order, the order this client keeps in its sidebar state file
// is not applied any more: it would put the sessions back where this client
// last saw them, over a move made on another client.
//
// A daemon older than the verb keeps the client's own order, as before. The
// first move on a newer one sends the whole order the rail shows, the kept
// one included, so nobody's arrangement is lost to the upgrade.
//
// Until the daemon answers a move, the rail draws the order the move made,
// and a move made in that time is built on it. Without that, three quick
// presses of shift+down kept only the first, because each was built on the
// listing the daemon had not updated yet, and a drag drew the old order until
// the answer came. The moves go to the daemon one at a time: a move made
// while one is out waits, and only the newest waiting one is sent, since it
// already holds the ones before it.

// sessionOrderPush is the orders on their way to the daemon.
type sessionOrderPush struct {
	// sending is set while an order is out and not answered.
	sending bool
	// next is the newest order made while one was out, nil for none.
	next []string
	// dial reaches the daemon. It is kept from the move.
	dial agentMailDial
}

// SessionOrderAppliedMsg reports how a session order sent to the daemon
// landed. Order is what was sent.
type SessionOrderAppliedMsg struct {
	Order []string
	Err   error
}

// legacySessionOrder makes this client behave as a build from before the
// daemon's session order: it keeps its own order, applies it over the
// listing, and never sends one. It is TUIOS_SESSION_ORDER_LEGACY=1, so the
// mixed-version case can be tested without an old build.
var legacySessionOrder = sync.OnceValue(func() bool {
	return os.Getenv("TUIOS_SESSION_ORDER_LEGACY") == "1"
})

// commitSessionOrder shows order on the rail and hands it to the daemon that
// holds the attached session, now or after the move that is out.
func (m *OS) commitSessionOrder(order []string) {
	m.SidebarOrder = order
	m.saveSidebarState()
	if m.DaemonClient == nil || m.sessionOrderIsLocal() || legacySessionOrder() {
		return
	}
	push := &m.sessionOrderPush
	if push.sending {
		push.next = order
		return
	}
	push.sending = true
	push.dial = m.verbDialer()
	m.queueSidebarCmd(sessionOrderCmd(push.dial, m.DaemonClient, order))
}

// sessionOrderIsLocal reports whether the daemon turned the order down, and
// still runs the build that did. A daemon upgraded since gets the order
// again.
func (m *OS) sessionOrderIsLocal() bool {
	if !m.sessionOrderLocalSet {
		return false
	}
	if m.DaemonClient != nil && m.DaemonClient.DaemonVersion() != m.sessionOrderLocal {
		m.sessionOrderLocal, m.sessionOrderLocalSet = "", false
		return false
	}
	return true
}

// sessionOrderPending reports whether a move on the rail is on its way to the
// daemon, in which case the rail draws the order it made.
func (m *OS) sessionOrderPending() bool {
	return m.sessionOrderPush.sending
}

// sessionOrderCmd calls set-session-order off the Update goroutine, as
// labelVerbCmd does, then refreshes the listing, so the rail can drop its own
// copy of the order with the daemon's already in the cache.
func sessionOrderCmd(dial agentMailDial, client *session.TUIClient, order []string) tea.Cmd {
	return func() tea.Msg {
		c, err := dial()
		if err != nil {
			return SessionOrderAppliedMsg{Order: order, Err: err}
		}
		defer func() { _ = c.Close() }()
		if _, err := c.Call("set-session-order", map[string]any{"order": order, "skip_missing": true}); err != nil {
			return SessionOrderAppliedMsg{Order: order, Err: err}
		}
		_, _ = client.RefreshSessionList()
		return SessionOrderAppliedMsg{Order: order}
	}
}

// applySessionOrderResult finishes a session order the daemon was sent, and
// sends the move that waited behind it, if one did.
func (m *OS) applySessionOrderResult(msg SessionOrderAppliedMsg) tea.Cmd {
	push := &m.sessionOrderPush
	next := push.next
	push.sending, push.next = false, nil
	if msg.Err != nil {
		var call *session.VerbCallError
		if errors.As(msg.Err, &call) && call.Code == session.ErrVerbUnknownVerb {
			// An older daemon: the order stays this client's, as it was
			// before the daemon kept one.
			build := ""
			if m.DaemonClient != nil {
				build = m.DaemonClient.DaemonVersion()
			}
			m.sessionOrderLocal, m.sessionOrderLocalSet = build, true
			return nil
		}
		m.LogWarn("Session order: %v", msg.Err)
		m.ShowNotification("The session order did not reach the daemon.", "error", m.Settings.NotificationDuration*2)
	}
	if next != nil && push.dial != nil && m.DaemonClient != nil {
		push.sending = true
		return sessionOrderCmd(push.dial, m.DaemonClient, next)
	}
	// The daemon's order is in the listing now. The copy kept here goes,
	// unless a later move replaced it while this one was in flight.
	if msg.Err == nil && slices.Equal(m.SidebarOrder, msg.Order) {
		m.SidebarOrder = nil
		m.saveSidebarState()
	}
	m.refreshSwitcherItems()
	return nil
}

// sidebarSessionOrder is the user's drag order for the sessions, or nil when
// the daemon keeps the order every client shows and its listing is already
// in it, which is the case unless a move of this client's is on its way
// there.
func (m *OS) sidebarSessionOrder() []string {
	if m.sessionOrderFromDaemon() && !m.sessionOrderPending() {
		return nil
	}
	return m.SidebarOrder
}

// sessionOrderFromDaemon reports whether the daemon keeps an order its
// listing carries, in which case it is the order the rail shows once no move
// of this client's is on its way to it.
func (m *OS) sessionOrderFromDaemon() bool {
	return m.DaemonClient != nil && !m.sessionOrderIsLocal() && !legacySessionOrder() && m.DaemonClient.SessionOrderSet()
}
