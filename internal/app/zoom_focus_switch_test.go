package app

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuios/internal/config"
	"github.com/Gaurav-Gosain/tuios/internal/layout"
)

// TestZoomFollowsFocusKeepsEveryPaneATile is the report's setup driven from the
// keyboard: a spiral zoomed to part of the screen, with the focus moved back
// and forth between the left pane and the right ones at random points in the
// slides. The left pane came out of it zero columns wide; the cause was the
// mouse (see TestAResizeUnderTheCameraLeavesTheTree), and this holds the
// focus path to the same result.
func TestZoomFollowsFocusKeepsEveryPaneATile(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			m, wins := tiledZoomOS(t, config.LayoutModeBSP)
			m.Settings.ZoomSize = 95
			m.Settings.ZoomAnimation = true
			m.Settings.ZoomFollowsFocus = true
			m.TileAllWindows()
			m.CompleteAllAnimations()

			plain := map[int]layout.Rect{}
			for i, w := range wins {
				plain[i] = layout.Rect{X: w.X, Y: w.Y, W: w.Width, H: w.Height}
			}

			r := rand.New(rand.NewSource(seed))
			clock := time.Now()
			m.msgClock = clock
			m.FocusedWindow = 0
			m.ToggleZoom()
			dur := m.Settings.GetAnimationDuration()
			for step := range 40 {
				clock = clock.Add(time.Duration(r.Int63n(int64(dur))))
				m.msgClock = clock
				stepAnimationsAt(m, clock)
				target := 0
				if m.FocusedWindow == 0 {
					target = 1 + r.Intn(len(wins)-1)
				}
				m.FocusWindow(target)
				for i, w := range wins {
					if w.Width < 3 {
						t.Fatalf("step %d: pane %d is %d columns wide", step, i, w.Width)
					}
				}
			}
			m.CompleteAllAnimations()
			if zw := m.zoomedWindow(); zw != nil {
				m.FocusedWindow = m.windowIndex(zw)
				m.ToggleZoom()
			}
			m.CompleteAllAnimations()
			for i, w := range wins {
				got := layout.Rect{X: w.X, Y: w.Y, W: w.Width, H: w.Height}
				if got != plain[i] {
					t.Errorf("pane %d is %+v after the zoom, want its tile %+v", i, got, plain[i])
				}
			}
		})
	}
}

// TestAResizeUnderTheCameraLeavesTheTree covers the paths that read the BSP
// tree's ratios back from the pane rectangles. Under a camera zoom those
// rectangles are panned over a box larger than the screen, so reading them
// against the screen put the left pane's divider a few columns from the edge.
func TestAResizeUnderTheCameraLeavesTheTree(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(m *OS)
	}{
		{"sync from geometry", func(m *OS) { m.SyncBSPTreeFromGeometry() }},
		{"grow width", func(m *OS) { m.ResizeFocusedWindowWidth(2) }},
		{"grow width left", func(m *OS) { m.ResizeFocusedWindowWidthLeft(-2) }},
		{"grow height", func(m *OS) { m.ResizeFocusedWindowHeight(2) }},
		{"grow height top", func(m *OS) { m.ResizeFocusedWindowHeightTop(-2) }},
	} {
		for zoomed := range 4 {
			t.Run(fmt.Sprintf("%s/pane %d", tc.name, zoomed), func(t *testing.T) {
				m, wins := tiledZoomOS(t, config.LayoutModeBSP)
				m.Settings.ZoomSize = 95
				m.WorkspaceHasCustom = map[int]bool{}
				m.TileAllWindows()
				m.CompleteAllAnimations()
				plain := map[int]layout.Rect{}
				for i, w := range wins {
					plain[i] = layout.Rect{X: w.X, Y: w.Y, W: w.Width, H: w.Height}
				}

				zoomAndSettle(m, zoomed)
				tc.do(m)
				m.CompleteAllAnimations()
				m.ToggleZoom()
				m.CompleteAllAnimations()

				for i, w := range wins {
					got := layout.Rect{X: w.X, Y: w.Y, W: w.Width, H: w.Height}
					if got != plain[i] {
						t.Errorf("pane %d is %+v after the zoom, want its tile %+v", i, got, plain[i])
					}
				}
			})
		}
	}
}

// stepAnimationsAt is UpdateAnimations against a given clock.
func stepAnimationsAt(m *OS, now time.Time) {
	for i := len(m.Animations) - 1; i >= 0; i-- {
		if m.Animations[i].UpdateAt(now) {
			m.Animations = append(m.Animations[:i], m.Animations[i+1:]...)
		}
	}
}
