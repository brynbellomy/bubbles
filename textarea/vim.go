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

// undoSnapshot captures a complete editor state for undo/redo.
type undoSnapshot struct {
	value    [][]rune
	row, col int
}

// undoStack is a two-stack undo/redo manager with bounded capacity.
// undoSnaps holds "before" states pushed before each mutation.
// redoSnaps holds "after" states saved during undo, popped on redo.
// The total combined capacity is bounded by defaultUndoCapacity.
type undoStack struct {
	undoSnaps []undoSnapshot
	redoSnaps []undoSnapshot
	capacity  int
}

const defaultUndoCapacity = 200

// cloneValue deep-copies a [][]rune value.
func cloneValue(v [][]rune) [][]rune {
	out := make([][]rune, len(v))
	for i, row := range v {
		out[i] = append([]rune(nil), row...)
	}
	return out
}

// pushUndo saves current state before a mutation and clears the redo stack.
// If at capacity, the oldest undo entry is dropped.
func (u *undoStack) pushUndo(s undoSnapshot) {
	if u.capacity == 0 {
		u.capacity = defaultUndoCapacity
	}
	u.redoSnaps = u.redoSnaps[:0]
	u.undoSnaps = append(u.undoSnaps, s)
	if len(u.undoSnaps) > u.capacity {
		drop := len(u.undoSnaps) - u.capacity
		u.undoSnaps = u.undoSnaps[drop:]
	}
}

// undo pops from undoSnaps and returns the state to restore, saving the
// provided current state onto redoSnaps. Returns nil if nothing to undo.
func (u *undoStack) undo(current undoSnapshot) *undoSnapshot {
	if len(u.undoSnaps) == 0 {
		return nil
	}
	u.redoSnaps = append(u.redoSnaps, current)
	s := u.undoSnaps[len(u.undoSnaps)-1]
	u.undoSnaps = u.undoSnaps[:len(u.undoSnaps)-1]
	return &s
}

// redo pops from redoSnaps and returns the state to restore, saving the
// provided current state onto undoSnaps. Returns nil if nothing to redo.
func (u *undoStack) redo(current undoSnapshot) *undoSnapshot {
	if len(u.redoSnaps) == 0 {
		return nil
	}
	u.undoSnaps = append(u.undoSnaps, current)
	s := u.redoSnaps[len(u.redoSnaps)-1]
	u.redoSnaps = u.redoSnaps[:len(u.redoSnaps)-1]
	return &s
}

// snapshotUndo saves the current state as an undo point before a mutation.
// It clears any pending redo history.
func (m *Model) snapshotUndo() {
	m.undo.pushUndo(undoSnapshot{
		value: cloneValue(m.value),
		row:   m.row,
		col:   m.col,
	})
}

// currentSnapshot returns the current state as a snapshot (without pushing).
func (m *Model) currentSnapshot() undoSnapshot {
	return undoSnapshot{
		value: cloneValue(m.value),
		row:   m.row,
		col:   m.col,
	}
}

// restoreSnapshot applies a snapshot to the model.
func (m *Model) restoreSnapshot(s *undoSnapshot) {
	m.value = cloneValue(s.value)
	m.row = s.row
	m.SetCursorColumn(s.col)
}

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
			m.snapshotUndo()
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

	// Pending d/c/y operators waiting for doubled letter (dd/cc/yy).
	// Task 12 will extend this to handle motions; for now ONLY the doubled
	// letter case fires; any other key clears the pending op.
	if m.vim.pendingOp == 'd' || m.vim.pendingOp == 'c' || m.vim.pendingOp == 'y' {
		op := m.vim.pendingOp
		m.vim.pendingOp = 0
		s := msg.String()
		if string(op) == s {
			m.snapshotUndo()
			m.vimYankCurrentLine()
			if op == 'c' {
				m.vimClearCurrentLine()
				m.vim.mode = ModeInsert
			} else if op == 'd' {
				m.vimDeleteCurrentLine()
			}
			// yy is a pure yank; no mutation.
			return
		}
		// Other keys (motions) — deferred to Task 12. For now, swallow.
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
		m.snapshotUndo() // capture pre-insert state
		m.vim.mode = ModeInsert
	case "I":
		m.snapshotUndo() // capture pre-insert state
		m.vimMotionFirstNonBlank()
		m.vim.mode = ModeInsert
	case "a":
		// `a` moves cursor right by 1 (past cursor), then enters Insert. The
		// Normal-mode cap (n-1) doesn't apply once we're in Insert — we want
		// to be able to append past the last char.
		m.snapshotUndo() // capture pre-insert state
		if m.col < len(m.value[m.row]) {
			m.SetCursorColumn(m.col + 1)
		}
		m.vim.mode = ModeInsert
	case "A":
		m.snapshotUndo() // capture pre-insert state
		m.SetCursorColumn(len(m.value[m.row]))
		m.vim.mode = ModeInsert
	case "o":
		m.snapshotUndo() // capture pre-insert state (before line mutation)
		m.vimOpenLineBelow()
		m.vim.mode = ModeInsert
	case "O":
		m.snapshotUndo() // capture pre-insert state (before line mutation)
		m.vimOpenLineAbove()
		m.vim.mode = ModeInsert
	case "s":
		m.snapshotUndo() // capture pre-insert state (before deletion)
		m.vimDeleteCharUnderCursor()
		m.vim.mode = ModeInsert
	case "x":
		m.snapshotUndo()
		for i := 0; i < count; i++ {
			m.vimDeleteCharUnderCursor()
		}
	case "X":
		m.snapshotUndo()
		for i := 0; i < count; i++ {
			if m.col > 0 {
				m.SetCursorColumn(m.col - 1)
				m.vimDeleteCharUnderCursor()
			}
		}
	case "d":
		m.vim.pendingOp = 'd'
	case "c":
		m.vim.pendingOp = 'c'
	case "y":
		m.vim.pendingOp = 'y'
	case "D":
		m.snapshotUndo()
		m.vimYankToEOL()
		m.vimDeleteToEOL()
	case "C":
		m.snapshotUndo()
		m.vimYankToEOL()
		m.vimDeleteToEOL()
		m.vim.mode = ModeInsert
	case "Y":
		m.vimYankCurrentLine()
	case "r":
		m.vim.pendingOp = 'r'
	case "~":
		m.snapshotUndo()
		for i := 0; i < count; i++ {
			m.vimToggleCaseUnderCursor()
		}
	case "u":
		if s := m.undo.undo(m.currentSnapshot()); s != nil {
			m.restoreSnapshot(s)
		}
	case "ctrl+r":
		if s := m.undo.redo(m.currentSnapshot()); s != nil {
			m.restoreSnapshot(s)
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

// vimYankCurrentLine stores the current row's text in yankBuf linewise.
func (m *Model) vimYankCurrentLine() {
	m.vim.yankBuf = string(m.value[m.row])
	m.vim.yankLinewise = true
}

// vimYankToEOL stores value[row][col:] charwise.
func (m *Model) vimYankToEOL() {
	m.vim.yankBuf = string(m.value[m.row][m.col:])
	m.vim.yankLinewise = false
}

// vimDeleteCurrentLine removes m.row from value, parking the cursor on the
// line that takes its place (or the previous line if it was the last).
func (m *Model) vimDeleteCurrentLine() {
	if len(m.value) == 1 {
		m.value[0] = m.value[0][:0]
		m.SetCursorColumn(0)
		return
	}
	m.value = append(m.value[:m.row], m.value[m.row+1:]...)
	if m.row >= len(m.value) {
		m.row = len(m.value) - 1
	}
	m.SetCursorColumn(0)
}

// vimDeleteToEOL removes value[row][col:].
func (m *Model) vimDeleteToEOL() {
	m.value[m.row] = m.value[m.row][:m.col]
	if m.col > 0 {
		m.SetCursorColumn(m.col - 1)
	}
}

// vimClearCurrentLine empties the current row without removing it.
func (m *Model) vimClearCurrentLine() {
	m.value[m.row] = m.value[m.row][:0]
	m.SetCursorColumn(0)
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
