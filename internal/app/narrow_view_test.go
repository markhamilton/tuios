package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Gaurav-Gosain/tuios/internal/config"
	"github.com/Gaurav-Gosain/tuios/internal/terminal"
)

// The desktop's floating layout the narrow tests start from: two panes that
// overlap, at rectangles no tiler would choose.
var narrowDesk = map[string]narrowRect{
	"narrow-a-0000000000000000000000000": {X: 5, Y: 3, W: 70, H: 20},
	"narrow-b-0000000000000000000000000": {X: 40, Y: 12, W: 90, H: 25},
}

// narrowOS is a client of a daemon session at the desktop's size, floating,
// with the two panes of narrowDesk and the rail shown.
func narrowOS(t *testing.T) *OS {
	t.Helper()
	prev := config.Global.Motion
	config.Global.Motion = config.MotionNone
	t.Cleanup(func() { config.Global.Motion = prev })

	m := NewOS(OSOptions{})
	m.IsDaemonSession = true
	m.Settings.SidebarEnabled = true
	m.Width, m.Height = 160, 48
	m.CurrentWorkspace = 1
	for _, id := range []string{"narrow-a-0000000000000000000000000", "narrow-b-0000000000000000000000000"} {
		g := narrowDesk[id]
		w := newTestWindow(t, id, g.W, g.H)
		w.Workspace = 1
		w.X, w.Y = g.X, g.Y
		m.Windows = append(m.Windows, w)
	}
	m.FocusedWindow = 0
	return m
}

func narrowRectOf(w *terminal.Window) narrowRect { return narrowRect{w.X, w.Y, w.Width, w.Height} }

// pushedRects is the rectangle of each pane in the state this client pushes.
func pushedRects(m *OS) map[string]narrowRect {
	out := map[string]narrowRect{}
	for _, w := range m.BuildSessionState().Windows {
		out[w.ID] = narrowRect{w.X, w.Y, w.Width, w.Height}
	}
	return out
}

func resize(m *OS, w, h int) { m.Update(tea.WindowSizeMsg{Width: w, Height: h}) }

// TestNarrowThreshold: narrow is fewer columns than appearance.narrow_width,
// on a client of a daemon session, and 0 turns it off.
func TestNarrowThreshold(t *testing.T) {
	m := narrowOS(t)
	for _, tc := range []struct {
		limit, width int
		daemon, want bool
	}{
		{100, 99, true, true},
		{100, 100, true, false},
		{100, 60, true, true},
		{100, 160, true, false},
		{0, 40, true, false},
		{100, 60, false, false},
		{config.NarrowWidthDefault, 80, true, true},
	} {
		m.Settings.NarrowWidth, m.Width, m.IsDaemonSession = tc.limit, tc.width, tc.daemon
		if got := m.narrowWanted(); got != tc.want {
			t.Errorf("narrow_width %d, %d columns, daemon %v: narrow = %v, want %v", tc.limit, tc.width, tc.daemon, got, tc.want)
		}
	}
	if config.NarrowWidthDefault != 100 {
		t.Errorf("NarrowWidthDefault = %d, want 100", config.NarrowWidthDefault)
	}
}

// TestNarrowViewTilesAFloatingSessionForItselfOnly: a resize below the line
// tiles the panes on this client and hides its rail, while every push still
// says the session floats, at the desktop's rectangles. A resize back above
// the line puts the floating rectangles and the rail back exactly.
//
// Negative control: drop the narrowPushRect substitution in BuildSessionState
// and the pushed rectangles are the phone's tiles.
func TestNarrowViewTilesAFloatingSessionForItselfOnly(t *testing.T) {
	m := narrowOS(t)
	defer closeWindows(m)

	resize(m, 60, 30)
	if !m.NarrowView() {
		t.Fatal("60 columns did not start the narrow view")
	}
	if !m.AutoTiling {
		t.Fatal("the narrow view does not tile")
	}
	if m.Settings.SidebarEnabled {
		t.Error("the narrow view left the rail shown")
	}
	a, b := m.Windows[0], m.Windows[1]
	if narrowRectOf(a) == narrowDesk[a.ID] || narrowRectOf(b) == narrowDesk[b.ID] {
		t.Fatalf("the panes kept their floating rectangles on the narrow screen: %+v %+v", narrowRectOf(a), narrowRectOf(b))
	}
	for _, w := range m.Windows {
		if w.X < 0 || w.X+w.Width > 60 {
			t.Errorf("pane %s at x %d width %d is off a 60-column screen", w.ID[:8], w.X, w.Width)
		}
	}

	st := m.BuildSessionState()
	if st.AutoTiling {
		t.Error("a push from the narrow client turned the session's tiling on")
	}
	if st.WorkspaceTrees != nil {
		t.Error("a push from the narrow client carried its BSP trees")
	}
	for id, got := range pushedRects(m) {
		if got != narrowDesk[id] {
			t.Errorf("pane %s pushed %+v, want the desktop's %+v", id[:8], got, narrowDesk[id])
		}
	}

	resize(m, 160, 48)
	if m.NarrowView() {
		t.Fatal("160 columns did not end the narrow view")
	}
	if m.AutoTiling {
		t.Error("the session's floating mode did not come back")
	}
	if !m.Settings.SidebarEnabled {
		t.Error("the rail did not come back")
	}
	for _, w := range m.Windows {
		if got := narrowRectOf(w); got != narrowDesk[w.ID] {
			t.Errorf("pane %s is at %+v after the narrow view, want %+v", w.ID[:8], got, narrowDesk[w.ID])
		}
	}
}

// TestNarrowViewCrossesBothWays: the view follows every resize across the
// line, and a resize that stays on one side does nothing new.
func TestNarrowViewCrossesBothWays(t *testing.T) {
	m := narrowOS(t)
	defer closeWindows(m)
	for _, step := range []struct {
		width int
		want  bool
	}{{120, false}, {99, true}, {70, true}, {100, false}, {50, true}, {101, false}} {
		resize(m, step.width, 30)
		if m.NarrowView() != step.want {
			t.Fatalf("at %d columns narrow = %v, want %v", step.width, m.NarrowView(), step.want)
		}
		if !step.want && m.AutoTiling {
			t.Fatalf("at %d columns the session's floating mode is not back", step.width)
		}
	}
	for _, w := range m.Windows {
		if got := narrowRectOf(w); got != narrowDesk[w.ID] {
			t.Errorf("pane %s drifted to %+v over the crossings, want %+v", w.ID[:8], got, narrowDesk[w.ID])
		}
	}
}

// TestNarrowRailToggleIsLocal: the rail can be shown again while narrow, the
// change reaches neither the config nor the session, and the view's end puts
// back the rail from before it, hidden or shown.
func TestNarrowRailToggleIsLocal(t *testing.T) {
	for _, before := range []bool{true, false} {
		m := narrowOS(t)
		m.Settings.SidebarEnabled = before
		cfg := config.DefaultConfig()
		m.UserConfig = cfg
		saved := cfg.Appearance.Sidebar.Enabled

		resize(m, 60, 30)
		if m.Settings.SidebarEnabled {
			t.Fatalf("before=%v: the narrow view shows the rail", before)
		}
		m.ToggleSidebar()
		if !m.Settings.SidebarEnabled {
			t.Fatalf("before=%v: the toggle did not show the rail while narrow", before)
		}
		if cfg.Appearance.Sidebar.Enabled != saved {
			t.Errorf("before=%v: the narrow toggle wrote the config", before)
		}
		resize(m, 160, 48)
		if m.Settings.SidebarEnabled != before {
			t.Errorf("before=%v: after the narrow view the rail is %v", before, m.Settings.SidebarEnabled)
		}
		closeWindows(m)
	}
}

// TestNarrowViewOnATiledSession: a session that already tiles is tiled as it
// is, pushes say it tiles, and only the rail is hidden.
func TestNarrowViewOnATiledSession(t *testing.T) {
	m := narrowOS(t)
	defer closeWindows(m)
	m.SetAutoTiling(true)
	resize(m, 60, 30)
	if !m.NarrowView() || m.narrowOverriding() {
		t.Fatalf("narrow = %v, overriding = %v, want narrow and no override", m.NarrowView(), m.narrowOverriding())
	}
	if !m.BuildSessionState().AutoTiling {
		t.Error("a tiled session was pushed as floating")
	}
	resize(m, 160, 48)
	if !m.AutoTiling {
		t.Error("leaving the narrow view turned a tiled session's tiling off")
	}
}

// TestNarrowViewRefusesFloating: tiling off is refused while narrow; tiling
// on tiles the session for real.
func TestNarrowViewRefusesFloating(t *testing.T) {
	m := narrowOS(t)
	defer closeWindows(m)
	resize(m, 60, 30)
	m.SetAutoTiling(false)
	if !m.AutoTiling {
		t.Fatal("tiling off took effect on the narrow screen")
	}
	if m.BuildSessionState().AutoTiling {
		t.Fatal("a refused tiling off changed the session")
	}
	// The tiling key toggles the session's mode, which floats: so it tiles
	// the session. [startup] tiled goes the same way (applyStartupTiling).
	m.ToggleAutoTiling()
	if !m.BuildSessionState().AutoTiling {
		t.Fatal("the tiling key on the narrow client did not tile the session")
	}
	m.ToggleAutoTiling()
	if !m.BuildSessionState().AutoTiling || !m.AutoTiling {
		t.Fatal("the tiling key turned a tiled session's tiling off on the narrow client")
	}
	resize(m, 160, 48)
	if !m.AutoTiling {
		t.Error("the session tiled from the narrow client stopped tiling when the view ended")
	}
}

// TestNarrowClientKeepsAPeersFloatingMove: with the desktop still attached
// (attach -k), a floating move it makes reaches the narrow client's pushes and
// is not drawn there, and the narrow client's tiles never reach the desktop.
// The desktop that attaches after the narrow client left finds every pane
// where it put it.
func TestNarrowClientKeepsAPeersFloatingMove(t *testing.T) {
	desk := narrowOS(t)
	defer closeWindows(desk)
	phone := narrowOS(t)
	defer closeWindows(phone)
	resize(phone, 60, 30)
	tiles := map[string]narrowRect{}
	for _, w := range phone.Windows {
		tiles[w.ID] = narrowRectOf(w)
	}

	// The desktop drags pane a.
	a := desk.Windows[0]
	a.X, a.Y = 20, 6
	moved := narrowDesk[a.ID]
	moved.X, moved.Y = 20, 6
	if err := phone.ApplyStateSyncFrom(desk.BuildSessionState(), "desk"); err != nil {
		t.Fatal(err)
	}
	if !phone.NarrowView() || !phone.AutoTiling {
		t.Fatal("a peer's floating state ended the narrow view")
	}
	for _, w := range phone.Windows {
		if narrowRectOf(w) != tiles[w.ID] {
			t.Errorf("the narrow client drew pane %s at the peer's %+v, want its tile %+v", w.ID[:8], narrowRectOf(w), tiles[w.ID])
		}
	}
	pushed := pushedRects(phone)
	if pushed[a.ID] != moved {
		t.Errorf("the narrow client pushes %+v for the moved pane, want the peer's %+v", pushed[a.ID], moved)
	}

	// The phone's push reaches the desktop: nothing moves there.
	if err := desk.ApplyStateSyncFrom(phone.BuildSessionState(), "phone"); err != nil {
		t.Fatal(err)
	}
	if desk.AutoTiling {
		t.Fatal("the narrow client's push turned tiling on at the desktop")
	}
	want := map[string]narrowRect{a.ID: moved, desk.Windows[1].ID: narrowDesk[desk.Windows[1].ID]}
	for _, w := range desk.Windows {
		if narrowRectOf(w) != want[w.ID] {
			t.Errorf("desktop pane %s is at %+v after the narrow push, want %+v", w.ID[:8], narrowRectOf(w), want[w.ID])
		}
	}

	// The narrow client is the last to push before it detaches, and a desktop
	// that attaches then restores that state.
	last := phone.BuildSessionState()
	for i := range last.Windows {
		w := &last.Windows[i]
		if got := (narrowRect{w.X, w.Y, w.Width, w.Height}); got != want[w.ID] {
			t.Errorf("the narrow client's last push has pane %s at %+v, want %+v", w.ID[:8], got, want[w.ID])
		}
	}
	if last.AutoTiling {
		t.Error("the narrow client's last push says the session tiles")
	}
	if err := desk.RestoreFromState(last); err != nil {
		t.Fatal(err)
	}
	for _, w := range desk.Windows {
		if narrowRectOf(w) != want[w.ID] {
			t.Errorf("desktop pane %s restores at %+v, want %+v", w.ID[:8], narrowRectOf(w), want[w.ID])
		}
	}
}
