package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Gaurav-Gosain/tuios/internal/tape"
)

// EnterTerminalMode switches from window management to terminal mode.
// In terminal mode, raw input bypasses Bubbletea and goes directly to the PTY.
func (m *OS) EnterTerminalMode() tea.Cmd {
	// Record mode switch for tape recording
	if m.TapeRecorder != nil && m.TapeRecorder.IsRecording() {
		m.TapeRecorder.RecordModeSwitch(tape.CommandTypeTerminalMode)
	}

	m.Mode = TerminalMode
	m.windowModeChosen = false

	// The raw reader is disabled. Bubbletea handles all input correctly,
	// including:
	// - Bracketed paste for Cmd+V (via PasteMsg)
	// - OSC 52 clipboard reading for Ctrl+Shift+V (via ClipboardMsg)
	// - All key events properly parsed
	// The raw reader conflicts with Bubbletea in modern terminals using CSI u
	// encoding.
	return nil
}

// ExitTerminalMode switches from terminal to window management mode.
// In window management mode, Bubbletea handles input parsing.
//
// It is the user's own request for window mode, so the mode stays until they
// leave it (see settleHomeMode).
func (m *OS) ExitTerminalMode() tea.Cmd {
	// Record mode switch for tape recording
	if m.TapeRecorder != nil && m.TapeRecorder.IsRecording() {
		m.TapeRecorder.RecordModeSwitch(tape.CommandTypeWindowManagementMode)
	}

	m.Mode = WindowManagementMode
	m.windowModeChosen = true
	m.nest = false

	// The raw reader is disabled. Bubbletea handles all input correctly.
	return nil
}

// ChooseWindowMode puts the client in window management mode because the user
// asked for it, with nothing recorded to a tape: the action that asked is
// recorded by name. It stays until the user leaves it, whatever closes or
// opens in the meantime.
func (m *OS) ChooseWindowMode() {
	m.Mode = WindowManagementMode
	m.windowModeChosen = true
}

// WindowModeChosen reports whether the client is in a window management mode
// the user asked for, rather than one a closed pane or a gesture left it in.
func (m *OS) WindowModeChosen() bool {
	return m.Mode == WindowManagementMode && m.windowModeChosen
}

// ReturnsToWindowMode reports whether appearance.return_to_window_mode is on:
// the upstream behaviour, where renaming, restoring or dragging a pane, or
// having no pane to type into, leaves the client in window management mode
// until the user enters terminal mode again.
func (m *OS) ReturnsToWindowMode() bool {
	return m.Settings.ReturnToWindowMode
}

// AutoWindowMode is the window management mode an action falls into when the
// pane that had the keyboard is still there: a rename, a restore from the
// dock, a pointer press on a frame. With return_to_window_mode on it switches,
// as upstream does. Off (the default) it leaves the mode alone, because the
// action is not a request to stop typing.
func (m *OS) AutoWindowMode() {
	if m.ReturnsToWindowMode() {
		m.Mode = WindowManagementMode
	}
}

// settleHomeMode runs after every message. Terminal mode is the home mode:
// a window management mode the user did not choose goes back to terminal mode
// as soon as a pane has the focus. That covers every path that drops to
// window mode because nothing was left to type into (the last pane closed, a
// switch to an empty workspace, a session with no panes yet): the next pane
// that opens or takes the focus gets the keys, with no key press needed.
//
// A gesture that borrows window mode keeps it until it ends (modeBorrowed),
// and a window mode the user chose stays until they leave it.
// With appearance.return_to_window_mode on it does nothing.
func (m *OS) settleHomeMode() {
	if m.Mode == TerminalMode {
		m.windowModeChosen = false
		return
	}
	if m.ReturnsToWindowMode() || m.windowModeChosen || m.modeBorrowed() || !m.hasFocusedWindow() {
		return
	}
	m.EnterTerminalMode()
}
