package app

import (
	"github.com/Gaurav-Gosain/tuios/internal/session"
	"github.com/Gaurav-Gosain/tuios/internal/terminal"
)

// The narrow view: a client attached from a small terminal (a phone over SSH)
// shows the panes tiled and hides its sidebar, for itself only.
//
// Whether the session tiles is session state (SessionState.AutoTiling), and so
// is every floating pane's rectangle. A narrow client must not change either,
// or the desktop that attaches next finds its floating layout gone. So the
// narrow view is a local layer over the session, the way a scratch group's
// view is (see scratch.go):
//
//   - A floating session is tiled here with m.AutoTiling set locally, and the
//     session's own value is kept in narrow.sessionTiling. BuildSessionState
//     reports that value, so a push from this client never turns tiling on.
//   - Each pane's floating rectangle is kept in narrow.floatGeom, taken when
//     the view starts and updated from every state that arrives (a peer moving
//     a floating pane). BuildSessionState pushes those rectangles in place of
//     the tiled ones on screen, and updateWindowFromState declines the
//     floating rectangle a state carries rather than drawing it.
//   - The BSP trees laid out here are not sent: the session floats, and the
//     trees it keeps are the ones from when it last tiled.
//   - The rail is hidden with the local flag only. Nothing is sent to the
//     session, and a change to the session's rail while narrow is kept as the
//     state to go back to.
//
// A session that already tiles needs none of that: the client tiles it as
// every client does, and only the rail is hidden.
//
// The view is decided from this client's own terminal size (m.Width), not the
// session's: under daemon.window_size the session can be sized by another
// client, and what makes a client narrow is the screen in the person's hand.

// narrowRect is a pane's floating rectangle, kept while the narrow view tiles.
type narrowRect struct{ X, Y, W, H int }

// narrowView is the narrow view's state. The zero value is off.
type narrowView struct {
	on bool
	// overriding is set while the session floats and this client tiles it.
	overriding bool
	// sessionTiling is the session's own AutoTiling, while the view is on.
	sessionTiling bool
	// floatGeom is each pane's floating rectangle by window ID, while
	// overriding.
	floatGeom map[string]narrowRect
	// sidebarPrev is the rail state to go back to when the view ends.
	sidebarPrev bool
	// railShown is whether the person showed the rail again while narrow.
	railShown bool
}

// NarrowView reports whether this client shows the narrow view.
func (m *OS) NarrowView() bool { return m.narrow.on }

// narrowOverriding reports whether this client tiles a floating session for
// itself.
func (m *OS) narrowOverriding() bool { return m.narrow.on && m.narrow.overriding }

// narrowWanted reports whether this client's terminal is narrow: attached to a
// daemon session, with fewer columns than appearance.narrow_width.
func (m *OS) narrowWanted() bool {
	limit := m.Settings.NarrowWidth
	return limit > 0 && m.IsDaemonSession && m.Width > 0 && m.Width < limit
}

// updateNarrowView starts or ends the narrow view when the terminal size or
// appearance.narrow_width moved across the line. It runs on every resize and
// after every message (see settleChrome), and costs one comparison when
// nothing crossed.
func (m *OS) updateNarrowView() {
	switch want := m.narrowWanted(); {
	case want && !m.narrow.on:
		m.enterNarrowView()
	case !want && m.narrow.on:
		m.leaveNarrowView()
	}
}

// enterNarrowView starts the narrow view: the rail goes, and a floating
// session is tiled here.
func (m *OS) enterNarrowView() {
	sessionTiles := m.AutoTiling
	if m.InScratchView() {
		sessionTiles = m.scratchBaseTiling
	}
	// A rail shown only to hold the keyboard is not the rail to go back to.
	prev := m.Settings.SidebarEnabled && !m.SidebarRevealedForFocus
	m.ExitSidebarFocus()
	m.narrow = narrowView{on: true, sessionTiling: sessionTiles, sidebarPrev: prev}
	m.settleSizes(func() {
		m.applySidebarFlag(false)
		if !sessionTiles {
			m.startNarrowTiling()
			return
		}
		if m.AutoTiling && len(m.Windows) > 0 {
			m.tileAllWindows()
		}
	})
	m.MarkAllDirty()
	m.LogInfo("Narrow view: on at %d columns (narrow_width %d)", m.Width, m.Settings.NarrowWidth)
}

// leaveNarrowView ends the narrow view: the floating rectangles come back,
// and so does the rail state from before the view.
func (m *OS) leaveNarrowView() {
	prev := m.narrow.sidebarPrev
	m.settleSizes(func() {
		m.endNarrowTiling()
		m.applySidebarFlag(prev)
		if m.AutoTiling && len(m.Windows) > 0 {
			m.tileAllWindows()
		}
	})
	m.narrow = narrowView{}
	m.MarkAllDirty()
	m.LogInfo("Narrow view: off at %d columns", m.Width)
}

// restartNarrowView starts a narrow view in force again over a session that
// was just taken whole (a restore, a session switch, a reconnect), which
// replaced every pane and the session's mode. The rail state the view goes
// back to is kept.
func (m *OS) restartNarrowView() {
	if !m.narrow.on {
		return
	}
	prev := m.narrow.sidebarPrev
	m.narrow = narrowView{}
	m.enterNarrowView()
	m.narrow.sidebarPrev = prev
}

// startNarrowTiling keeps every pane's floating rectangle and tiles the
// panes here.
func (m *OS) startNarrowTiling() {
	m.narrow.overriding = true
	m.narrow.floatGeom = make(map[string]narrowRect, len(m.Windows))
	for _, w := range m.Windows {
		m.narrow.floatGeom[w.ID] = m.floatingRect(w)
	}
	if m.InScratchView() {
		// The group on screen tiles already. The workspace under it tiles
		// when the group is hidden.
		m.scratchBaseTiling = true
		return
	}
	m.requireRealLayout()
	m.AutoTiling = true
	if len(m.Windows) > 0 {
		m.tileAllWindows()
	}
	for _, w := range m.Windows {
		if w.Workspace == m.CurrentWorkspace {
			w.InvalidateCache()
		}
	}
}

// endNarrowTiling puts the session's floating layout back: tiling off, and
// each pane at the floating rectangle kept for it. A session that started
// tiling while narrow has nothing to put back.
func (m *OS) endNarrowTiling() {
	if !m.narrow.overriding {
		return
	}
	geom := m.narrow.floatGeom
	m.narrow.overriding = false
	m.narrow.floatGeom = nil
	if m.InScratchView() {
		m.scratchBaseTiling = m.narrow.sessionTiling
	} else {
		m.AutoTiling = m.narrow.sessionTiling
	}
	if m.AutoTiling {
		return
	}
	for _, w := range m.Windows {
		w.SetTiled(false)
		if g, ok := geom[w.ID]; ok && !w.Zoomed && !w.IsPopup {
			m.CancelSnapAnimation(w)
			w.X, w.Y = g.X, g.Y
			w.Resize(g.W, g.H)
		}
		w.InvalidateCache()
		w.MarkPositionDirty()
	}
}

// floatingRect is the rectangle a pane is going to: where its animation ends
// when it is sliding, else where it is.
func (m *OS) floatingRect(w *terminal.Window) narrowRect {
	if anim := m.snapInFlight(w); anim != nil {
		return narrowRect{anim.EndX, anim.EndY, anim.EndWidth, anim.EndHeight}
	}
	return narrowRect{w.X, w.Y, w.Width, w.Height}
}

// narrowSessionTiling is the AutoTiling a push reports: the session's own
// while this client tiles a floating session, else autoTiling as given.
func (m *OS) narrowSessionTiling(autoTiling bool) bool {
	if m.narrowOverriding() {
		return m.narrow.sessionTiling
	}
	return autoTiling
}

// narrowPushRect is the rectangle a push carries for w: its floating one
// while this client tiles a floating session, else ok is false.
func (m *OS) narrowPushRect(id string) (narrowRect, bool) {
	if !m.narrowOverriding() {
		return narrowRect{}, false
	}
	g, ok := m.narrow.floatGeom[id]
	return g, ok
}

// adoptNarrowTiling runs in ApplyStateSync right after the session's
// AutoTiling was taken. It records the session's value and, while narrow,
// puts the local tiling back on: a floating session that arrives starts the
// override, and a session that now tiles ends it.
func (m *OS) adoptNarrowTiling(state *session.SessionState) {
	if !m.narrow.on {
		return
	}
	m.narrow.sessionTiling = m.AutoTiling
	if m.AutoTiling {
		// The session tiles now. Its rectangles are tiled ones, and there is
		// no floating layout left to keep.
		m.narrow.overriding = false
		m.narrow.floatGeom = nil
		return
	}
	if !m.narrow.overriding {
		// The session stopped tiling. The panes took its floating
		// rectangles in this sync (nothing was declining them), so those
		// are the ones to keep.
		m.narrow.overriding = true
		m.narrow.floatGeom = make(map[string]narrowRect, len(m.Windows))
		for _, w := range m.Windows {
			m.narrow.floatGeom[w.ID] = narrowRect{w.X, w.Y, w.Width, w.Height}
		}
	}
	// A pane the state no longer holds has no rectangle to keep, and a pane
	// it brought for the first time keeps the rectangle it arrived with.
	live := make(map[string]bool, len(state.Windows))
	for i := range state.Windows {
		ws := &state.Windows[i]
		live[ws.ID] = true
		if _, ok := m.narrow.floatGeom[ws.ID]; !ok && !ws.Unplaced {
			m.narrow.floatGeom[ws.ID] = narrowRect{ws.X, ws.Y, ws.Width, ws.Height}
		}
	}
	for id := range m.narrow.floatGeom {
		if !live[id] {
			delete(m.narrow.floatGeom, id)
		}
	}
	m.AutoTiling = true
}

// narrowKeepsRect reports whether a state's rectangle for a pane is a
// floating one this client keeps rather than draws, and keeps it.
func (m *OS) narrowKeepsRect(ws *session.WindowState) bool {
	if !m.narrowOverriding() {
		return false
	}
	m.narrow.floatGeom[ws.ID] = narrowRect{ws.X, ws.Y, ws.Width, ws.Height}
	return true
}
