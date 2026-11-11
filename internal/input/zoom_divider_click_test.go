package input

import (
	"testing"

	"github.com/Gaurav-Gosain/tuios/internal/terminal"
)

// TestAClickOnTheDividerUnderAZoomKeepsTheLayout is the report: with a pane
// on the right zoomed to part of the screen, the left pane shows as a strip at
// the edge, mostly border and divider. A click there armed a divider resize,
// and the release read the camera's rectangles back into the BSP tree, so the
// left pane came out of the zoom almost no columns wide.
func TestAClickOnTheDividerUnderAZoomKeepsTheLayout(t *testing.T) {
	o, wa, wb := twoPaneBSP(t)
	prevSize, prevFollow := o.Settings.ZoomSize, o.Settings.ZoomFollowsFocus
	t.Cleanup(func() { o.Settings.ZoomSize, o.Settings.ZoomFollowsFocus = prevSize, prevFollow })
	o.Settings.ZoomSize = 95
	o.Settings.ZoomFollowsFocus = true
	left, right := leftPaneOf(wa, wb)
	plainLeft, plainRight := rectOf(left), rectOf(right)

	o.FocusWindow(indexOf(o, right))
	o.ToggleZoom()
	o.CompleteAllAnimations()
	if !right.Zoomed {
		t.Fatal("the right pane did not zoom")
	}

	x, y := right.X-1, right.Y+right.Height/2
	handleMouseClick(clickMsg(x, y), o)
	if o.Resizing {
		t.Error("a press on the divider armed a resize while a pane is zoomed")
	}
	handleMouseRelease(releaseMsg(x, y), o)
	o.CompleteAllAnimations()

	if z := zoomedPane(o); z != nil {
		o.FocusWindow(indexOf(o, z))
		o.ToggleZoom()
	}
	o.CompleteAllAnimations()

	if got := rectOf(left); got != plainLeft {
		t.Errorf("left pane is %v after the zoom, want its tile %v", got, plainLeft)
	}
	if got := rectOf(right); got != plainRight {
		t.Errorf("right pane is %v after the zoom, want its tile %v", got, plainRight)
	}
}

func rectOf(w *terminal.Window) [4]int { return [4]int{w.X, w.Y, w.Width, w.Height} }

func zoomedPane(o *OS2) *terminal.Window {
	for _, w := range o.Windows {
		if w.Zoomed {
			return w
		}
	}
	return nil
}
