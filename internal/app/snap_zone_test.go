package app

import (
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/config"
)

// A snap zone belongs on the edge of the region panes occupy, which a sidebar
// shortens exactly as the dock shortens the height. Reading the raw screen put
// the right-hand zone under the rail: releasing at the visible right edge
// snapped nothing, and reaching the zone meant dragging into the sidebar.
func TestSnapZonesFollowTheContentRegion(t *testing.T) {
	const cols, rows, rail = 120, 40, 24

	for _, side := range []string{"right", "left"} {
		t.Run(side, func(t *testing.T) {
			swapSidebar(t, side, rail)
			m := &OS{Settings: config.Global, Width: cols, Height: rows}

			left := m.GetLeftMargin()
			right := m.GetRenderWidth() - m.GetRightMargin()
			midY := m.GetTopMargin() + m.GetUsableHeight()/2

			if got := m.SnapZoneAt(right-1, midY); got != SnapRight {
				t.Errorf("released on the content's right edge (x=%d): got %v, want SnapRight", right-1, got)
			}
			if got := m.SnapZoneAt(left, midY); got != SnapLeft {
				t.Errorf("released on the content's left edge (x=%d): got %v, want SnapLeft", left, got)
			}

			// Releasing over the rail still snaps toward it. The zone is open-ended
			// past the content edge on purpose: a pointer dragged that way is heading
			// right, and the rail is not a drop target of its own, so refusing here
			// would make an obvious gesture do nothing.
			var overRail int
			want := SnapRight
			if side == "right" {
				overRail = right + rail/2
			} else {
				overRail = rail / 2
				want = SnapLeft
			}
			if got := m.SnapZoneAt(overRail, midY); got != want {
				t.Errorf("released over the %s rail (x=%d): got %v, want %v", side, overRail, got, want)
			}

			// The middle of the pane region is not a zone at either edge.
			if got := m.SnapZoneAt(left+(right-left)/2, midY); got != NoSnap {
				t.Errorf("mid-content: got %v, want NoSnap", got)
			}
		})
	}
}

// TestSnapZoneDepthFollowsSnapEdge pins the zone math to appearance.snap_edge.
// A depth of n covers exactly the outer n rows or columns, so a pane released
// a cell or two below the top no longer maximizes at the default of 1, and 0
// turns edge snapping off everywhere, the corners included.
//
// Negative control: put the old `const edge = 5` back and the depth-1 rows
// at the second row and column fail.
func TestSnapZoneDepthFollowsSnapEdge(t *testing.T) {
	// Big enough that the deepest zones do not reach the middle.
	const cols, rows = 160, 60
	for _, edge := range []int{0, 1, 3, config.SnapEdgeMax} {
		m := &OS{Settings: config.Global, Width: cols, Height: rows}
		m.Settings.SidebarEnabled = false
		m.Settings.SnapEdge = edge

		top := m.GetTopMargin()
		bottom := top + m.GetUsableHeight() - 1
		left := m.GetLeftMargin()
		right := m.GetLayoutWidth() - m.GetRightMargin() - 1
		midX, midY := left+(right-left)/2, top+(bottom-top)/2

		cases := []struct {
			name string
			x, y int
			want SnapQuarter
		}{
			{"top row", midX, top, SnapFullScreen},
			{"last top row in the zone", midX, top + edge - 1, SnapFullScreen},
			{"first row below the zone", midX, top + edge, NoSnap},
			{"left column", left, midY, SnapLeft},
			{"last left column in the zone", left + edge - 1, midY, SnapLeft},
			{"first column right of the left zone", left + edge, midY, NoSnap},
			{"right column", right, midY, SnapRight},
			{"first column left of the right zone", right - edge, midY, NoSnap},
			{"bottom row", midX, bottom, NoSnap}, // a bottom edge alone is no zone
			{"top-left corner", left, top, SnapTopLeft},
			{"top-right corner", right, top, SnapTopRight},
			{"bottom-left corner", left, bottom, SnapBottomLeft},
			{"bottom-right corner", right, bottom, SnapBottomRight},
			{"middle", midX, midY, NoSnap},
		}
		for _, c := range cases {
			want := c.want
			if edge == 0 {
				want = NoSnap
			}
			if got := m.SnapZoneAt(c.x, c.y); got != want {
				t.Errorf("snap_edge %d, %s (x=%d, y=%d): got %v, want %v", edge, c.name, c.x, c.y, got, want)
			}
		}
	}
}

// TestSnapEdgeConfig: the default is 1, an explicit 0 survives the load, and a
// value out of range is clamped.
func TestSnapEdgeConfig(t *testing.T) {
	cases := []struct {
		toml string
		want int
	}{
		{"", config.SnapEdgeDefault},
		{"[appearance]\nsnap_edge = 0\n", 0},
		{"[appearance]\nsnap_edge = 4\n", 4},
		{"[appearance]\nsnap_edge = -3\n", 0},
		{"[appearance]\nsnap_edge = 999\n", config.SnapEdgeMax},
	}
	if config.SnapEdgeDefault != 1 {
		t.Errorf("SnapEdgeDefault = %d, want 1", config.SnapEdgeDefault)
	}
	for _, c := range cases {
		cfg, err := config.ParseUserConfig([]byte(c.toml))
		if err != nil {
			t.Fatalf("%q: %v", c.toml, err)
		}
		s := config.DefaultSettings()
		config.ApplyAppearanceConfig(cfg, &s)
		if s.SnapEdge != c.want {
			t.Errorf("%q: Settings.SnapEdge = %d, want %d", c.toml, s.SnapEdge, c.want)
		}
	}
}

func swapSidebar(t *testing.T, pos string, width int) {
	t.Helper()
	oe, op, ow := config.Global.SidebarEnabled, config.Global.SidebarPosition, config.Global.SidebarWidth
	config.Global.SidebarEnabled, config.Global.SidebarPosition, config.Global.SidebarWidth = true, pos, width
	t.Cleanup(func() {
		config.Global.SidebarEnabled, config.Global.SidebarPosition, config.Global.SidebarWidth = oe, op, ow
	})
}
