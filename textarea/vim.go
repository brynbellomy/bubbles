package textarea

import (
	"unicode"

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
	pendingFindKind  rune    // active f/F/t/T prompt waiting for target char; 0 if none
	lastFind         vimFind // last completed find, for ; and , repeat
	selStartRow      int
	selStartCol      int
	yankBuf          string
	yankLinewise     bool
	savedCursorShape tea.CursorShape
}

// vimEscBinding matches Esc and Ctrl-[ in both Insert and non-Insert modes.
var vimEscBinding = key.NewBinding(key.WithKeys("esc", "ctrl+["))

// vimUpdate handles a key press in a non-Insert vim mode.
func (m *Model) vimUpdate(msg tea.KeyPressMsg) {
	// Esc / Ctrl-[ from any non-Insert mode returns to Normal and clears
	// pending state.
	if key.Matches(msg, vimEscBinding) {
		m.SetVimMode(ModeNormal)
		return
	}

	// Pending `g` prefix (for gg). gu/gU come in Task 17.
	if m.vim.pendingOp == 'g' {
		m.vim.pendingOp = 0
		if msg.String() == "g" {
			m.vimMotionFirstLine()
		}
		return
	}

	switch msg.String() {
	case "h":
		m.vimMotionCharLeft()
	case "l":
		m.vimMotionCharRight()
	case "j":
		m.CursorDown()
	case "k":
		m.CursorUp()
	case "w":
		m.vimMotionWordForward(vimWordClass)
	case "W":
		m.vimMotionWordForward(vimWORDClass)
	case "b":
		m.vimMotionWordBackward(vimWordClass)
	case "B":
		m.vimMotionWordBackward(vimWORDClass)
	case "e":
		m.vimMotionWordEndForward(vimWordClass)
	case "E":
		m.vimMotionWordEndForward(vimWORDClass)
	case "0":
		m.vimMotionLineStart()
	case "^":
		m.vimMotionFirstNonBlank()
	case "$":
		m.vimMotionLineEnd()
	case "g":
		m.vim.pendingOp = 'g'
	case "G":
		m.vimMotionLastLine()
	}
}

// vimMotionCharLeft is vim's h — char left without wrapping to previous line.
func (m *Model) vimMotionCharLeft() {
	if m.col > 0 {
		m.SetCursorColumn(m.col - 1)
	}
}

// vimMotionCharRight is vim's l — char right without wrapping to next line.
func (m *Model) vimMotionCharRight() {
	if m.col < len(m.value[m.row]) {
		m.SetCursorColumn(m.col + 1)
	}
}

// isVimWordChar reports whether r is part of a vim "word" (alnum or _).
func isVimWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// vimWordClass returns 0 for whitespace, 1 for word chars, 2 for non-word
// non-whitespace (punct). Used to detect word-boundary transitions.
func vimWordClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case isVimWordChar(r):
		return 1
	default:
		return 2
	}
}

// vimWORDClass returns 0 for whitespace, 1 for everything else.
func vimWORDClass(r rune) int {
	if unicode.IsSpace(r) {
		return 0
	}
	return 1
}

// vimMotionWordForward implements `w`. classFn determines word vs WORD.
func (m *Model) vimMotionWordForward(classFn func(rune) int) {
	for {
		line := m.value[m.row]
		if m.col >= len(line) {
			// End of line: descend to next line at col 0 if available, else stop.
			if m.row >= len(m.value)-1 {
				return
			}
			m.row++
			m.SetCursorColumn(0)
			// If next line starts with a non-whitespace char we are done.
			if len(m.value[m.row]) > 0 && classFn(m.value[m.row][0]) != 0 {
				return
			}
			continue
		}
		startClass := classFn(line[m.col])
		// Step forward through chars of the same class, then skip whitespace.
		for m.col < len(line) && classFn(line[m.col]) == startClass && startClass != 0 {
			m.SetCursorColumn(m.col + 1)
		}
		for m.col < len(line) && classFn(line[m.col]) == 0 {
			m.SetCursorColumn(m.col + 1)
		}
		if m.col < len(line) {
			return
		}
		// Wrap to next line.
	}
}

// vimMotionWordBackward implements `b`.
func (m *Model) vimMotionWordBackward(classFn func(rune) int) {
	for {
		if m.col == 0 {
			if m.row == 0 {
				return
			}
			m.row--
			m.SetCursorColumn(len(m.value[m.row]))
			continue
		}
		line := m.value[m.row]
		// Step back to the previous non-whitespace.
		m.SetCursorColumn(m.col - 1)
		for m.col > 0 && classFn(line[m.col]) == 0 {
			m.SetCursorColumn(m.col - 1)
		}
		if classFn(line[m.col]) == 0 {
			// Whole line was whitespace; loop again to previous line.
			continue
		}
		startClass := classFn(line[m.col])
		for m.col > 0 && classFn(line[m.col-1]) == startClass {
			m.SetCursorColumn(m.col - 1)
		}
		return
	}
}

// vimMotionLineStart is `0`.
func (m *Model) vimMotionLineStart() { m.SetCursorColumn(0) }

// vimMotionFirstNonBlank is `^`.
func (m *Model) vimMotionFirstNonBlank() {
	line := m.value[m.row]
	col := 0
	for col < len(line) && unicode.IsSpace(line[col]) {
		col++
	}
	m.SetCursorColumn(col)
}

// vimMotionLineEnd is `$` — cursor on last char of line, not past.
func (m *Model) vimMotionLineEnd() {
	if n := len(m.value[m.row]); n > 0 {
		m.SetCursorColumn(n - 1)
	} else {
		m.SetCursorColumn(0)
	}
}

// vimMotionFirstLine is `gg`.
func (m *Model) vimMotionFirstLine() {
	m.row = 0
	m.vimMotionFirstNonBlank()
	m.repositionView()
}

// vimMotionLastLine is `G` (no count).
func (m *Model) vimMotionLastLine() {
	m.row = len(m.value) - 1
	m.vimMotionFirstNonBlank()
	m.repositionView()
}

// vimMotionWordEndForward implements `e`.
func (m *Model) vimMotionWordEndForward(classFn func(rune) int) {
	for {
		line := m.value[m.row]
		if m.col >= len(line)-1 || len(line) == 0 {
			// At/past end: advance to next line if possible.
			if m.row >= len(m.value)-1 {
				if len(line) > 0 {
					m.SetCursorColumn(len(line) - 1)
				}
				return
			}
			m.row++
			m.SetCursorColumn(0)
			continue
		}
		// Advance once so 'e' from end-of-word goes to next word's end.
		m.SetCursorColumn(m.col + 1)
		line = m.value[m.row]
		// Skip whitespace.
		for m.col < len(line) && classFn(line[m.col]) == 0 {
			m.SetCursorColumn(m.col + 1)
		}
		if m.col >= len(line) {
			continue
		}
		startClass := classFn(line[m.col])
		for m.col < len(line)-1 && classFn(line[m.col+1]) == startClass {
			m.SetCursorColumn(m.col + 1)
		}
		return
	}
}
