package app

import (
	"strconv"

	"github.com/Gaurav-Gosain/tuios/internal/session"
)

// The workspace number prompt: tmux's prefix ' (select-window by index) and
// a move of the focused pane to a workspace by number. The keys 1-9 reach the
// first nine workspaces directly; the prompt reaches every one up to
// NumWorkspaces. It is the one-line rename editor with a digits-only buffer,
// so it opens, edits, commits and cancels with the keys and the dialog every
// other one-line edit uses.

// workspacePromptDigits is how many digits the prompt takes: enough for the
// highest workspace number and no more.
func (m *OS) workspacePromptDigits() int {
	return len(strconv.Itoa(max(m.NumWorkspaces, 1)))
}

// workspacePromptOpen reports whether the open editor is a workspace prompt.
func (m *OS) workspacePromptOpen() bool {
	return m.RenameKind == PromptGotoWorkspace || m.RenameKind == PromptMoveToWorkspace
}

// BeginGotoWorkspacePrompt opens the prompt that switches to a workspace by
// number.
func (m *OS) BeginGotoWorkspacePrompt() {
	m.RenameKind = PromptGotoWorkspace
	m.RenameTargetID = ""
	m.RenameBuffer = ""
}

// BeginMoveToWorkspacePrompt opens the prompt that moves the focused pane to
// a workspace by number and follows it. The pane is fixed when the prompt
// opens, so a focus change while it is up (a pane opening elsewhere) does not
// move a different pane.
func (m *OS) BeginMoveToWorkspacePrompt() {
	w := m.GetFocusedWindow()
	if w == nil {
		m.ShowNotification("No pane to move", "info", m.Settings.NotificationDuration)
		return
	}
	if isScratch(w) || session.IsScratchWorkspace(w.Workspace) {
		m.ShowNotification("A scratch pane stays in its scratch group.", "info", m.Settings.NotificationDuration)
		return
	}
	m.RenameKind = PromptMoveToWorkspace
	m.RenameTargetID = w.ID
	m.RenameBuffer = ""
}

// workspacePromptAppend keeps the ASCII digits of text, up to the prompt's
// width. A workspace number is all the prompt can take.
func (m *OS) workspacePromptAppend(text string) {
	limit := m.workspacePromptDigits()
	for _, r := range text {
		if r < '0' || r > '9' || len(m.RenameBuffer) >= limit {
			continue
		}
		m.RenameBuffer += string(r)
	}
}

// workspacePromptTitle names what the prompt does and the range it takes.
func (m *OS) workspacePromptTitle() string {
	rng := " (1-" + strconv.Itoa(m.NumWorkspaces) + ")"
	if m.RenameKind == PromptMoveToWorkspace {
		return "move pane to workspace" + rng
	}
	return "go to workspace" + rng
}

// commitWorkspacePrompt acts on the number typed. An empty buffer is a
// cancel. A number out of range says the range and changes nothing.
//
// A daemon session numbers its workspaces densely, so a number past the last
// workspace in use lands on the next free one, as the 1-9 keys already do
// (see newWorkspaceSlot).
func (m *OS) commitWorkspacePrompt(kind RenameKind, target, text string) {
	if text == "" {
		return
	}
	ws, err := strconv.Atoi(text)
	if err != nil || ws < 1 || ws > m.NumWorkspaces {
		m.ShowNotification("Workspace "+text+" does not exist. Use a number from 1 to "+strconv.Itoa(m.NumWorkspaces)+".",
			"warning", m.Settings.NotificationDuration)
		return
	}
	switch kind {
	case PromptGotoWorkspace:
		if ws != m.CurrentWorkspace {
			m.SwitchToWorkspace(ws)
		}
	case PromptMoveToWorkspace:
		idx := m.windowIndexByID(target)
		if idx < 0 {
			m.ShowNotification("The pane is gone", "info", m.Settings.NotificationDuration)
			return
		}
		m.MoveWindowToWorkspaceAndFollow(idx, ws)
	}
}
