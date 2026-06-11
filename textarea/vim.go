package textarea

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// VimMode identifies which vim mode the textarea is currently in.
type VimMode int

const (
	// ModeInsert is the default. When vim is disabled, the textarea is
	// always in ModeInsert.
	ModeInsert VimMode = iota
	// ModeNormal is vim's command mode.
	ModeNormal
	// ModeVisualChar is character-wise visual selection.
	ModeVisualChar
	// ModeVisualLine is line-wise visual selection.
	ModeVisualLine
	// ModeReplace is the single-shot replace mode entered with `r{c}`.
	ModeReplace
)

// vimFind records the last f/F/t/T target for ; and ,.
type vimFind struct {
	kind rune
	ch   rune
}

// vimState holds all vim-mode-only state that lives on Model.
type vimState struct {
	mode             VimMode
	pendingOp        rune
	pendingCount     int
	pendingFindKind  rune
	lastFind         vimFind
	selStartRow      int
	selStartCol      int
	yankBuf          string
	yankLinewise     bool
	savedCursorShape tea.CursorShape
}

// vimEscBinding matches Esc and Ctrl-[ in both Insert and non-Insert modes.
var vimEscBinding = key.NewBinding(key.WithKeys("esc", "ctrl+["))

// vimUpdate handles a key press in a non-Insert vim mode. Returns true if the
// key was consumed by the vim dispatcher.
func (m *Model) vimUpdate(msg tea.KeyPressMsg) bool {
	// Task 1 stub: swallow everything in non-Insert modes. Later tasks fill
	// this in with motion, operator, and text-object handling.
	_ = msg
	return true
}
