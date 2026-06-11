// Package textarea — vim modal keybindings (opt-in).
//
// This file implements opt-in vim modal keybindings for the textarea. The
// surface covers Normal / Insert / Visual / Replace modes, motions, operators
// over motions, text objects, counts, and a single unnamed yank register.
// Marks, macros, named registers, search, and block visual are out of scope.
// See docs/superpowers/specs/2026-06-10-textarea-vim-keybindings-design.md.
package textarea

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
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
	pendingObject    rune    // active text-object qualifier 'i' or 'a' after d/c/y; 0 if none
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
	defer m.applyVimCursorShape()
	if key.Matches(msg, vimEscBinding) {
		m.SetVimMode(ModeNormal)
		return
	}

	// Visual-mode operators consume the selection immediately.
	if m.vim.mode == ModeVisualChar || m.vim.mode == ModeVisualLine {
		switch msg.String() {
		case "d", "x":
			m.snapshotUndo()
			r1, c1, r2, c2 := m.vimVisualRange()
			m.vimYankRange(r1, c1, r2, c2)
			m.vim.yankLinewise = m.vim.mode == ModeVisualLine
			m.vimDeleteRange(r1, c1, r2, c2)
			m.vim.mode = ModeNormal
			return
		case "y":
			r1, c1, r2, c2 := m.vimVisualRange()
			m.vimYankRange(r1, c1, r2, c2)
			m.vim.yankLinewise = m.vim.mode == ModeVisualLine
			m.row = r1
			m.SetCursorColumn(c1)
			m.vim.mode = ModeNormal
			return
		case "c":
			m.snapshotUndo()
			r1, c1, r2, c2 := m.vimVisualRange()
			m.vimYankRange(r1, c1, r2, c2)
			m.vim.yankLinewise = m.vim.mode == ModeVisualLine
			m.vimDeleteRange(r1, c1, r2, c2)
			m.vim.mode = ModeInsert
			return
		case "~":
			m.snapshotUndo()
			r1, c1, r2, c2 := m.vimVisualRange()
			// Toggle case across range.
			for r := r1; r <= r2; r++ {
				line := m.value[r]
				start := 0
				end := len(line)
				if r == r1 {
					start = c1
				}
				if r == r2 {
					end = c2
				}
				if end > len(line) {
					end = len(line)
				}
				for i := start; i < end; i++ {
					switch {
					case unicode.IsUpper(line[i]):
						line[i] = unicode.ToLower(line[i])
					case unicode.IsLower(line[i]):
						line[i] = unicode.ToUpper(line[i])
					}
				}
			}
			m.vim.mode = ModeNormal
			return
		case "o":
			m.row, m.vim.selStartRow = m.vim.selStartRow, m.row
			c := m.col
			m.SetCursorColumn(m.vim.selStartCol)
			m.vim.selStartCol = c
			return
		}
	}

	// Pending r{c} — next char replaces the char under cursor.
	if m.vim.pendingOp == 'r' {
		m.vim.pendingOp = 0
		if r, ok := singleRune(msg); ok {
			m.snapshotUndo()
			m.vimReplaceCharUnderCursor(r)
		}
		m.vim.mode = ModeNormal
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

	// Pending `g` prefix (gg / gu / gU).
	if m.vim.pendingOp == 'g' {
		switch msg.String() {
		case "g":
			m.vim.pendingOp = 0
			m.vimMotionFirstLine()
		case "u":
			m.vim.pendingOp = 'L' // pending "lowercase over motion"
		case "U":
			m.vim.pendingOp = 'U' // pending "uppercase over motion"
		default:
			m.vim.pendingOp = 0
		}
		return
	}

	// Pending text-object qualifier ('i' or 'a' after d/c/y).
	if m.vim.pendingObject != 0 {
		qual := m.vim.pendingObject
		op := m.vim.pendingOp
		m.vim.pendingObject = 0
		m.vim.pendingOp = 0
		s := msg.String()
		r1, c1, r2, c2, ok := m.vimTextObjectRange(qual, s)
		if !ok {
			return
		}
		m.snapshotUndo()
		m.vimYankRange(r1, c1, r2, c2)
		if op != 'y' {
			m.vimDeleteRange(r1, c1, r2, c2)
		}
		if op == 'c' {
			m.vim.mode = ModeInsert
		}
		return
	}

	if m.vim.pendingOp == 'd' || m.vim.pendingOp == 'c' || m.vim.pendingOp == 'y' {
		op := m.vim.pendingOp
		m.vim.pendingOp = 0
		s := msg.String()
		// Digit after operator (e.g. d3w) — accumulate into count and keep
		// the operator pending.
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			m.vim.pendingOp = op
			m.vim.pendingCount = m.vim.pendingCount*10 + int(s[0]-'0')
			if m.vim.pendingCount > vimMaxCount {
				m.vim.pendingCount = vimMaxCount
			}
			return
		}
		if s == "0" && m.vim.pendingCount > 0 {
			m.vim.pendingOp = op
			m.vim.pendingCount = m.vim.pendingCount * 10
			if m.vim.pendingCount > vimMaxCount {
				m.vim.pendingCount = vimMaxCount
			}
			return
		}
		// Linewise: doubled letter (dd/cc/yy).
		if string(op) == s {
			count := m.vim.pendingCount
			if count == 0 {
				count = 1
			}
			m.vim.pendingCount = 0
			m.snapshotUndo()
			startRow := m.row
			endRow := m.row + count - 1
			if endRow >= len(m.value) {
				endRow = len(m.value) - 1
			}
			// Yank the affected lines.
			var parts []string
			for r := startRow; r <= endRow; r++ {
				parts = append(parts, string(m.value[r]))
			}
			m.vim.yankBuf = strings.Join(parts, "\n")
			m.vim.yankLinewise = true
			if op == 'c' {
				// cc: clear the current line and enter Insert regardless of count.
				m.vimClearCurrentLine()
				m.vim.mode = ModeInsert
			} else if op == 'd' {
				// Delete startRow..endRow inclusive.
				if startRow == 0 && endRow == len(m.value)-1 {
					m.value = [][]rune{nil}
					m.row = 0
					m.SetCursorColumn(0)
				} else {
					m.value = append(m.value[:startRow], m.value[endRow+1:]...)
					if m.row >= len(m.value) {
						m.row = len(m.value) - 1
					}
					m.SetCursorColumn(0)
				}
			}
			// yy with count yanks without mutation.
			return
		}
		// Text object qualifier ('i' or 'a') — wait for object key.
		if s == "i" || s == "a" {
			m.vim.pendingObject = rune(s[0])
			m.vim.pendingOp = op // keep op for the pendingObject branch
			return
		}
		// Motion-based op — consume any pending count for the motion.
		count := m.vim.pendingCount
		if count == 0 {
			count = 1
		}
		m.vim.pendingCount = 0
		r1, c1, r2, c2, linewise, ok := m.vimMotionRange(s, count)
		if !ok {
			return
		}
		m.snapshotUndo()
		if linewise {
			// Linewise motion — convert to row-range delete/yank.
			if r1 > r2 {
				r1, r2 = r2, r1
			}
			var parts []string
			for r := r1; r <= r2; r++ {
				parts = append(parts, string(m.value[r]))
			}
			m.vim.yankBuf = strings.Join(parts, "\n")
			m.vim.yankLinewise = true
			if op != 'y' {
				if r1 == 0 && r2 == len(m.value)-1 {
					m.value = [][]rune{nil}
					m.row = 0
					m.SetCursorColumn(0)
				} else {
					m.value = append(m.value[:r1], m.value[r2+1:]...)
					if m.row >= len(m.value) {
						m.row = len(m.value) - 1
					}
					m.SetCursorColumn(0)
				}
			}
		} else {
			m.vimYankRange(r1, c1, r2, c2)
			if op != 'y' {
				m.vimDeleteRange(r1, c1, r2, c2)
			}
		}
		if op == 'c' {
			m.vim.mode = ModeInsert
		}
		return
	}

	if m.vim.pendingOp == 'L' || m.vim.pendingOp == 'U' {
		op := m.vim.pendingOp
		m.vim.pendingOp = 0
		s := msg.String()
		// Use any pending count for the motion.
		count := m.vim.pendingCount
		if count == 0 {
			count = 1
		}
		m.vim.pendingCount = 0
		r1, c1, r2, c2, _, ok := m.vimMotionRange(s, count)
		if !ok {
			return
		}
		m.snapshotUndo()
		m.vimApplyCase(r1, c1, r2, c2, op == 'U')
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

	rawCount := m.vim.pendingCount // 0 means "no count typed yet"
	count := rawCount
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
		m.vim.pendingCount = rawCount
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
		var xBuf strings.Builder
		for i := 0; i < count; i++ {
			if m.col >= len(m.value[m.row]) {
				break
			}
			xBuf.WriteRune(m.value[m.row][m.col])
			m.vimDeleteCharUnderCursor()
		}
		if xBuf.Len() > 0 {
			m.vim.yankBuf = xBuf.String()
			m.vim.yankLinewise = false
		}
	case "X":
		m.snapshotUndo()
		var xBuf strings.Builder
		for i := 0; i < count; i++ {
			if m.col == 0 {
				break
			}
			m.SetCursorColumn(m.col - 1)
			xBuf.WriteRune(m.value[m.row][m.col])
			m.vimDeleteCharUnderCursor()
		}
		if xBuf.Len() > 0 {
			// X collects chars in reverse order of deletion; reverse to restore
			// original left-to-right order.
			runes := []rune(xBuf.String())
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			m.vim.yankBuf = string(runes)
			m.vim.yankLinewise = false
		}
	case "d":
		m.vim.pendingOp = 'd'
		// Carry any pre-operator count forward so the motion can use it
		// (e.g. 3dw). If no count was typed, leave pendingCount at 0 so
		// a count typed after the operator (d3w) accumulates normally.
		m.vim.pendingCount = rawCount
	case "c":
		m.vim.pendingOp = 'c'
		m.vim.pendingCount = rawCount
	case "y":
		m.vim.pendingOp = 'y'
		m.vim.pendingCount = rawCount
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
	case "p":
		m.snapshotUndo()
		m.vimPaste(true) // after
	case "P":
		m.snapshotUndo()
		m.vimPaste(false) // before
	case "r":
		m.vim.pendingOp = 'r'
		m.vim.mode = ModeReplace
	case "~":
		m.snapshotUndo()
		for i := 0; i < count; i++ {
			m.vimToggleCaseUnderCursor()
		}
	case "v":
		m.vim.mode = ModeVisualChar
		m.vim.selStartRow = m.row
		m.vim.selStartCol = m.col
	case "V":
		m.vim.mode = ModeVisualLine
		m.vim.selStartRow = m.row
		m.vim.selStartCol = 0
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

// vimPaste inserts yankBuf relative to the cursor. If linewise, the buffer
// becomes a new line below (after=true) or above (after=false) the current row.
// If charwise, chars are spliced in after (after=true) or at (after=false) the
// cursor column. The cursor lands on the last pasted character in both cases.
func (m *Model) vimPaste(after bool) {
	if m.vim.yankBuf == "" {
		return
	}
	if m.vim.yankLinewise {
		newRow := m.row
		if after {
			newRow = m.row + 1
		}
		newLine := []rune(m.vim.yankBuf)
		if newRow >= len(m.value) {
			m.value = append(m.value, newLine)
		} else {
			m.value = append(m.value[:newRow+1], m.value[newRow:]...)
			m.value[newRow] = newLine
		}
		m.row = newRow
		// Vim parks cursor on first non-blank of pasted line.
		m.vimMotionFirstNonBlank()
		return
	}
	// Charwise: determine insertion column.
	insertCol := m.col
	if after && m.col < len(m.value[m.row]) {
		insertCol = m.col + 1
	}
	runes := []rune(m.vim.yankBuf)
	line := m.value[m.row]
	newLine := make([]rune, 0, len(line)+len(runes))
	newLine = append(newLine, line[:insertCol]...)
	newLine = append(newLine, runes...)
	newLine = append(newLine, line[insertCol:]...)
	m.value[m.row] = newLine
	// Cursor on last pasted char.
	m.SetCursorColumn(insertCol + len(runes) - 1)
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

// vimMotionRange computes the range covered by a motion key starting from the
// current cursor. Returns the start (always cursor) and end position, and
// whether the motion is linewise. Returns ok=false if the key isn't a motion.
// Internally moves and restores cursor.
func (m *Model) vimMotionRange(s string, count int) (r1, c1, r2, c2 int, linewise bool, ok bool) {
	if count == 0 {
		count = 1
	}
	startRow, startCol := m.row, m.col
	saveRow, saveCol := m.row, m.col
	defer func() {
		m.row = saveRow
		m.SetCursorColumn(saveCol)
	}()
	switch s {
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
		// `e` motion is inclusive — bump end by one so operator consumes the
		// final char.
		m.SetCursorColumn(m.col + 1)
	case "E":
		for i := 0; i < count; i++ {
			m.vimMotionWordEndForward(vimWORDClass)
		}
		m.SetCursorColumn(m.col + 1)
	case "h":
		for i := 0; i < count; i++ {
			m.vimMotionCharLeft()
		}
	case "l":
		for i := 0; i < count; i++ {
			m.vimMotionCharRight()
		}
	case "0":
		m.vimMotionLineStart()
	case "^":
		m.vimMotionFirstNonBlank()
	case "$":
		m.vimMotionLineEnd()
		// `$` is inclusive — bump.
		m.SetCursorColumn(m.col + 1)
	case "j":
		for i := 0; i < count; i++ {
			m.CursorDown()
		}
		linewise = true
	case "k":
		for i := 0; i < count; i++ {
			m.CursorUp()
		}
		linewise = true
	case "G":
		if count > 1 {
			m.row = clamp(count-1, 0, len(m.value)-1)
			m.vimMotionFirstNonBlank()
		} else {
			m.vimMotionLastLine()
		}
		linewise = true
	default:
		return 0, 0, 0, 0, false, false
	}
	r2, c2 = m.row, m.col
	r1, c1 = startRow, startCol
	// Normalize so (r1,c1) <= (r2,c2).
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	ok = true
	return
}

// vimYankRange copies the text from (r1,c1) to (r2,c2) (exclusive end) into
// yankBuf as charwise.
func (m *Model) vimYankRange(r1, c1, r2, c2 int) {
	var b strings.Builder
	for r := r1; r <= r2; r++ {
		line := m.value[r]
		start := 0
		end := len(line)
		if r == r1 {
			start = c1
		}
		if r == r2 {
			end = c2
		}
		if end > len(line) {
			end = len(line)
		}
		if start > end {
			start = end
		}
		b.WriteString(string(line[start:end]))
		if r < r2 {
			b.WriteByte('\n')
		}
	}
	m.vim.yankBuf = b.String()
	m.vim.yankLinewise = false
}

// vimDeleteRange deletes [r1,c1 .. r2,c2) from value, leaving cursor at
// (r1,c1).
func (m *Model) vimDeleteRange(r1, c1, r2, c2 int) {
	if r1 == r2 {
		line := m.value[r1]
		if c2 > len(line) {
			c2 = len(line)
		}
		m.value[r1] = append(line[:c1], line[c2:]...)
	} else {
		head := append([]rune{}, m.value[r1][:c1]...)
		tail := append([]rune{}, m.value[r2][c2:]...)
		head = append(head, tail...)
		m.value[r1] = head
		m.value = append(m.value[:r1+1], m.value[r2+1:]...)
	}
	m.row = r1
	m.SetCursorColumn(c1)
}

// vimTextObjectRange resolves a text-object (i/a + obj key) to a range.
func (m *Model) vimTextObjectRange(qual rune, obj string) (r1, c1, r2, c2 int, ok bool) {
	inner := qual == 'i'
	switch obj {
	case "w":
		return m.vimObjectWord(inner)
	case "\"":
		return m.vimObjectQuote(inner, '"')
	case "'":
		return m.vimObjectQuote(inner, '\'')
	case "`":
		return m.vimObjectQuote(inner, '`')
	case "(", ")":
		return m.vimObjectBracket(inner, '(', ')')
	case "{", "}":
		return m.vimObjectBracket(inner, '{', '}')
	case "[", "]":
		return m.vimObjectBracket(inner, '[', ']')
	case "p":
		return m.vimObjectParagraph(inner)
	}
	return 0, 0, 0, 0, false
}

// vimObjectParagraph returns row-range covering the paragraph at cursor.
// Paragraph = run of non-blank lines, blank-line-delimited. ip is the run; ap
// also includes the trailing blank line if any.
func (m *Model) vimObjectParagraph(inner bool) (r1, c1, r2, c2 int, ok bool) {
	isBlank := func(r int) bool {
		return r < 0 || r >= len(m.value) || len(strings.TrimSpace(string(m.value[r]))) == 0
	}
	// If the cursor is on a blank line, the paragraph object collapses on that
	// blank run.
	if isBlank(m.row) {
		top := m.row
		for top > 0 && isBlank(top-1) {
			top--
		}
		bot := m.row
		for bot < len(m.value)-1 && isBlank(bot+1) {
			bot++
		}
		return top, 0, bot + 1, 0, true
	}
	top := m.row
	for top > 0 && !isBlank(top-1) {
		top--
	}
	bot := m.row
	for bot < len(m.value)-1 && !isBlank(bot+1) {
		bot++
	}
	if !inner && bot+1 < len(m.value) && isBlank(bot+1) {
		bot++
	}
	if bot+1 < len(m.value) {
		return top, 0, bot + 1, 0, true
	}
	// Last paragraph: range to end of last line.
	return top, 0, bot, len(m.value[bot]), true
}

// vimObjectQuote computes i{q}/a{q} for a quote char q. Returns range on the
// current logical line only; multi-line quoted strings are out of scope.
func (m *Model) vimObjectQuote(inner bool, q rune) (r1, c1, r2, c2 int, ok bool) {
	line := m.value[m.row]
	// Find the nearest quote at or before cursor.
	left := -1
	for i := m.col; i >= 0; i-- {
		if i < len(line) && line[i] == q {
			left = i
			break
		}
	}
	if left == -1 {
		// Try forward.
		for i := m.col; i < len(line); i++ {
			if line[i] == q {
				left = i
				break
			}
		}
	}
	if left == -1 {
		return 0, 0, 0, 0, false
	}
	// Find the closing quote after left.
	right := -1
	for i := left + 1; i < len(line); i++ {
		if line[i] == q {
			right = i
			break
		}
	}
	if right == -1 {
		return 0, 0, 0, 0, false
	}
	if inner {
		return m.row, left + 1, m.row, right, true
	}
	return m.row, left, m.row, right + 1, true
}

// vimObjectBracket finds the matching open/closeR pair on the current line
// around or after the cursor. Single-line only.
func (m *Model) vimObjectBracket(inner bool, open, closeR rune) (r1, c1, r2, c2 int, ok bool) {
	line := m.value[m.row]
	// Walk back to the most recent unmatched open before/at cursor.
	depth := 0
	left := -1
	for i := m.col; i >= 0 && i < len(line); i-- {
		switch line[i] {
		case closeR:
			depth++
		case open:
			if depth == 0 {
				left = i
			} else {
				depth--
			}
		}
		if left != -1 {
			break
		}
	}
	if left == -1 {
		// Try forward.
		for i := m.col; i < len(line); i++ {
			if line[i] == open {
				left = i
				break
			}
		}
	}
	if left == -1 {
		return 0, 0, 0, 0, false
	}
	// Walk forward for matching close.
	depth = 0
	right := -1
	for i := left + 1; i < len(line); i++ {
		switch line[i] {
		case open:
			depth++
		case closeR:
			if depth == 0 {
				right = i
				break
			}
			depth--
		}
		if right != -1 {
			break
		}
	}
	if right == -1 {
		return 0, 0, 0, 0, false
	}
	if inner {
		return m.row, left + 1, m.row, right, true
	}
	return m.row, left, m.row, right + 1, true
}

// vimObjectWord computes iw/aw bounds. iw = word at cursor (no surrounding
// whitespace). aw = word + trailing whitespace (or leading if at end).
func (m *Model) vimObjectWord(inner bool) (r1, c1, r2, c2 int, ok bool) {
	line := m.value[m.row]
	if len(line) == 0 || m.col >= len(line) {
		return 0, 0, 0, 0, false
	}
	cls := vimWordClass(line[m.col])
	start := m.col
	for start > 0 && vimWordClass(line[start-1]) == cls {
		start--
	}
	end := m.col
	for end < len(line) && vimWordClass(line[end]) == cls {
		end++
	}
	if !inner {
		// `aw`: include trailing whitespace (or leading if no trailing).
		extEnd := end
		for extEnd < len(line) && unicode.IsSpace(line[extEnd]) {
			extEnd++
		}
		if extEnd > end {
			end = extEnd
		} else {
			// Extend leading whitespace.
			for start > 0 && unicode.IsSpace(line[start-1]) {
				start--
			}
		}
	}
	return m.row, start, m.row, end, true
}

// vimVisualRange returns the normalized range covered by the current visual
// selection. For ModeVisualChar the range is end-exclusive at c2 (includes the
// char under cursor). For ModeVisualLine the range covers whole rows including
// the trailing newline implicitly.
func (m *Model) vimVisualRange() (r1, c1, r2, c2 int) {
	r1, c1 = m.vim.selStartRow, m.vim.selStartCol
	r2, c2 = m.row, m.col
	if m.vim.mode == ModeVisualLine {
		if r1 > r2 {
			r1, r2 = r2, r1
		}
		c1 = 0
		// To delete whole rows including the trailing newline, target the
		// start of the row after r2 — vimDeleteRange handles that.
		if r2+1 < len(m.value) {
			c2 = 0
			r2 = r2 + 1
		} else {
			c2 = len(m.value[r2])
		}
		return
	}
	// Normalize char range and make end inclusive (vim selection includes the
	// char under cursor), so bump c2 by 1.
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	c2++
	return
}

// vimApplyCase transforms case across a range.
func (m *Model) vimApplyCase(r1, c1, r2, c2 int, upper bool) {
	transform := unicode.ToLower
	if upper {
		transform = unicode.ToUpper
	}
	for r := r1; r <= r2; r++ {
		line := m.value[r]
		start := 0
		end := len(line)
		if r == r1 {
			start = c1
		}
		if r == r2 {
			end = c2
		}
		if end > len(line) {
			end = len(line)
		}
		for i := start; i < end; i++ {
			line[i] = transform(line[i])
		}
	}
}

// vimVisualRangeForRender returns the inclusive selection range for rendering.
// For VisualChar, c2 is exclusive of one past the cursor. For VisualLine,
// c1=0 and c2=len(value[r2]). ok=false if not in visual mode.
func (m *Model) vimVisualRangeForRender() (r1, c1, r2, c2 int, ok bool) {
	if !m.vimEnabled {
		return
	}
	if m.vim.mode != ModeVisualChar && m.vim.mode != ModeVisualLine {
		return
	}
	r1, c1 = m.vim.selStartRow, m.vim.selStartCol
	r2, c2 = m.row, m.col
	if m.vim.mode == ModeVisualLine {
		if r1 > r2 {
			r1, r2 = r2, r1
		}
		c1 = 0
		c2 = len(m.value[r2])
		return r1, c1, r2, c2, true
	}
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	// Make c2 inclusive of cursor char.
	c2++
	ok = true
	return
}

// renderWrappedLineWithSelection renders a wrapped line, splicing selection
// styling where the visual selection intersects.
func (m *Model) renderWrappedLineWithSelection(
	wrappedLine []rune,
	logicalRow int,
	wrappedStart int,
	baseStyle lipgloss.Style,
) string {
	selR1, selC1, selR2, selC2, ok := m.vimVisualRangeForRender()
	if !ok || logicalRow < selR1 || logicalRow > selR2 {
		return baseStyle.Render(string(wrappedLine))
	}
	styles := m.activeStyle()
	selStyle := styles.computedSelectedText().Inherit(baseStyle)

	start := 0
	end := len(wrappedLine)
	if logicalRow == selR1 {
		start = max(0, selC1-wrappedStart)
	}
	if logicalRow == selR2 {
		end = min(len(wrappedLine), selC2-wrappedStart)
		if end < 0 {
			end = 0
		}
	}
	if start >= end {
		return baseStyle.Render(string(wrappedLine))
	}

	var b strings.Builder
	if start > 0 {
		b.WriteString(baseStyle.Render(string(wrappedLine[:start])))
	}
	b.WriteString(selStyle.Render(string(wrappedLine[start:end])))
	if end < len(wrappedLine) {
		b.WriteString(baseStyle.Render(string(wrappedLine[end:])))
	}
	return b.String()
}
