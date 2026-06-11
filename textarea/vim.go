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

const vimMaxCount = 9999

// vimUpdate handles a key press in a non-Insert vim mode.
func (m *Model) vimUpdate(msg tea.KeyPressMsg) {
	if key.Matches(msg, vimEscBinding) {
		m.SetVimMode(ModeNormal)
		return
	}

	// Pending r{c} — next char replaces the char under cursor.
	if m.vim.pendingOp == 'r' {
		m.vim.pendingOp = 0
		if r, ok := singleRune(msg); ok {
			m.vimReplaceCharUnderCursor(r)
		}
		return
	}

	if m.vim.pendingFindKind != 0 {
		kind := m.vim.pendingFindKind
		m.vim.pendingFindKind = 0
		if r, ok := singleRune(msg); ok {
			m.vimFindChar(kind, r)
			m.vim.lastFind = vimFind{kind: kind, ch: r}
		}
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

	s := msg.String()

	// Digit input — counts. '0' is a motion only when no count is pending.
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		m.vim.pendingCount = m.vim.pendingCount*10 + int(s[0]-'0')
		if m.vim.pendingCount > vimMaxCount {
			m.vim.pendingCount = vimMaxCount
		}
		return
	}
	if s == "0" && m.vim.pendingCount > 0 {
		m.vim.pendingCount = m.vim.pendingCount * 10
		if m.vim.pendingCount > vimMaxCount {
			m.vim.pendingCount = vimMaxCount
		}
		return
	}

	count := m.vim.pendingCount
	if count == 0 {
		count = 1
	}
	m.vim.pendingCount = 0

	switch s {
	case "h":
		for i := 0; i < count; i++ {
			m.vimMotionCharLeft()
		}
	case "l":
		for i := 0; i < count; i++ {
			m.vimMotionCharRight()
		}
	case "j":
		for i := 0; i < count; i++ {
			m.CursorDown()
		}
	case "k":
		for i := 0; i < count; i++ {
			m.CursorUp()
		}
	case "w":
		for i := 0; i < count; i++ {
			m.vimMotionWordForward(vimWordClass)
		}
	case "W":
		for i := 0; i < count; i++ {
			m.vimMotionWordForward(vimWORDClass)
		}
	case "b":
		for i := 0; i < count; i++ {
			m.vimMotionWordBackward(vimWordClass)
		}
	case "B":
		for i := 0; i < count; i++ {
			m.vimMotionWordBackward(vimWORDClass)
		}
	case "e":
		for i := 0; i < count; i++ {
			m.vimMotionWordEndForward(vimWordClass)
		}
	case "E":
		for i := 0; i < count; i++ {
			m.vimMotionWordEndForward(vimWORDClass)
		}
	case "0":
		m.vimMotionLineStart()
	case "^":
		m.vimMotionFirstNonBlank()
	case "$":
		m.vimMotionLineEnd()
	case "g":
		m.vim.pendingOp = 'g'
	case "G":
		if count > 1 {
			m.row = clamp(count-1, 0, len(m.value)-1)
			m.vimMotionFirstNonBlank()
			m.repositionView()
		} else {
			m.vimMotionLastLine()
		}
	case "f":
		m.vim.pendingFindKind = 'f'
	case "F":
		m.vim.pendingFindKind = 'F'
	case "t":
		m.vim.pendingFindKind = 't'
	case "T":
		m.vim.pendingFindKind = 'T'
	case ";":
		m.vimFindRepeat(false)
	case ",":
		m.vimFindRepeat(true)
	case "i":
		m.vim.mode = ModeInsert
	case "I":
		m.vimMotionFirstNonBlank()
		m.vim.mode = ModeInsert
	case "a":
		// `a` moves cursor right by 1 (past cursor), then enters Insert. The
		// Normal-mode cap (n-1) doesn't apply once we're in Insert — we want
		// to be able to append past the last char.
		if m.col < len(m.value[m.row]) {
			m.SetCursorColumn(m.col + 1)
		}
		m.vim.mode = ModeInsert
	case "A":
		m.SetCursorColumn(len(m.value[m.row]))
		m.vim.mode = ModeInsert
	case "o":
		m.vimOpenLineBelow()
		m.vim.mode = ModeInsert
	case "O":
		m.vimOpenLineAbove()
		m.vim.mode = ModeInsert
	case "s":
		m.vimDeleteCharUnderCursor()
		m.vim.mode = ModeInsert
	case "x":
		for i := 0; i < count; i++ {
			m.vimDeleteCharUnderCursor()
		}
	case "X":
		for i := 0; i < count; i++ {
			if m.col > 0 {
				m.SetCursorColumn(m.col - 1)
				m.vimDeleteCharUnderCursor()
			}
		}
	case "r":
		m.vim.pendingOp = 'r'
	case "~":
		for i := 0; i < count; i++ {
			m.vimToggleCaseUnderCursor()
		}
	}
}

// vimMotionCharLeft is vim's h — char left without wrapping to previous line.
func (m *Model) vimMotionCharLeft() {
	if m.col > 0 {
		m.SetCursorColumn(m.col - 1)
	}
}

// vimMotionCharRight is vim's l — char right, stopping at the last character
// of the line (Normal mode does not place cursor past the last char).
func (m *Model) vimMotionCharRight() {
	if n := len(m.value[m.row]); n > 0 && m.col < n-1 {
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

// vimOpenLineBelow inserts a blank line after m.row and parks the cursor on it.
func (m *Model) vimOpenLineBelow() {
	m.SetCursorColumn(len(m.value[m.row]))
	m.splitLine(m.row, m.col)
}

// vimOpenLineAbove inserts a blank line before m.row and parks the cursor on it.
func (m *Model) vimOpenLineAbove() {
	m.SetCursorColumn(0)
	m.splitLine(m.row, 0)
	m.row--
}

// vimDeleteCharUnderCursor removes value[row][col] (no-op on empty line).
func (m *Model) vimDeleteCharUnderCursor() {
	if len(m.value[m.row]) == 0 || m.col >= len(m.value[m.row]) {
		return
	}
	m.value[m.row] = append(m.value[m.row][:m.col], m.value[m.row][m.col+1:]...)
	if m.col > len(m.value[m.row]) {
		m.SetCursorColumn(len(m.value[m.row]))
	}
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

// singleRune extracts the single literal rune from a KeyPressMsg, if any.
// Returns ok=false for special keys (Esc, arrows, etc.).
func singleRune(msg tea.KeyPressMsg) (rune, bool) {
	if len(msg.Text) == 1 {
		return rune(msg.Text[0]), true
	}
	// Text may be empty for synthesized rune key events from tests.
	if msg.Code != 0 && msg.Code < 0x110000 {
		r := rune(msg.Code)
		if r >= 0x20 && r <= 0x7e {
			return r, true
		}
	}
	return 0, false
}

// vimFindChar searches the current line for ch in the given direction. For
// 'f'/'F', cursor lands on the match. For 't'/'T', cursor lands one before
// (forward) or one after (backward).
func (m *Model) vimFindChar(kind rune, ch rune) {
	line := m.value[m.row]
	switch kind {
	case 'f':
		for i := m.col + 1; i < len(line); i++ {
			if line[i] == ch {
				m.SetCursorColumn(i)
				return
			}
		}
	case 't':
		for i := m.col + 1; i < len(line); i++ {
			if line[i] == ch {
				m.SetCursorColumn(i - 1)
				return
			}
		}
	case 'F':
		for i := m.col - 1; i >= 0; i-- {
			if line[i] == ch {
				m.SetCursorColumn(i)
				return
			}
		}
	case 'T':
		for i := m.col - 1; i >= 0; i-- {
			if line[i] == ch {
				m.SetCursorColumn(i + 1)
				return
			}
		}
	}
}

// vimReplaceCharUnderCursor overwrites value[row][col] with r (no-op on empty line).
func (m *Model) vimReplaceCharUnderCursor(r rune) {
	if len(m.value[m.row]) == 0 || m.col >= len(m.value[m.row]) {
		return
	}
	m.value[m.row][m.col] = r
}

// vimToggleCaseUnderCursor flips the case of value[row][col] and advances the cursor.
func (m *Model) vimToggleCaseUnderCursor() {
	if len(m.value[m.row]) == 0 || m.col >= len(m.value[m.row]) {
		return
	}
	r := m.value[m.row][m.col]
	switch {
	case unicode.IsUpper(r):
		m.value[m.row][m.col] = unicode.ToLower(r)
	case unicode.IsLower(r):
		m.value[m.row][m.col] = unicode.ToUpper(r)
	}
	if m.col < len(m.value[m.row])-1 {
		m.SetCursorColumn(m.col + 1)
	}
}

// vimFindRepeat repeats the last find. reverse swaps direction.
func (m *Model) vimFindRepeat(reverse bool) {
	if m.vim.lastFind.kind == 0 {
		return
	}
	kind := m.vim.lastFind.kind
	if reverse {
		switch kind {
		case 'f':
			kind = 'F'
		case 'F':
			kind = 'f'
		case 't':
			kind = 'T'
		case 'T':
			kind = 't'
		}
	}
	m.vimFindChar(kind, m.vim.lastFind.ch)
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
