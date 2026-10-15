// Package textarea — vim modal keybindings (opt-in).
//
// This file implements opt-in vim modal keybindings for the textarea. The
// surface covers Normal / Insert / Visual / Replace modes, motions, operators
// over motions, text objects, counts, joins, shifts, search, and a single
// unnamed yank register. Marks, macros, named registers, and block visual are
// out of scope.
// See docs/superpowers/specs/2026-06-10-textarea-vim-keybindings-design.md.
package textarea

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/brynbellomy/bubbles/key"
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
	// ModeReplace is vim's Replace mode, entered with `R`: typed characters
	// overwrite the line.
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
	search           vimSearch
	replace          vimReplaceSession
	// onYank, when set, is invoked with the text yanked by a visual-mode
	// `y` operation. Consumers use it to mirror the selection into the OS
	// clipboard. nil means no callback (the default).
	onYank func(text string, linewise bool)
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
	if m.vim.search.prompting {
		m.vimSearchPromptUpdate(msg)
		return
	}
	if m.vim.mode == ModeReplace {
		m.vimReplaceUpdate(msg)
		return
	}
	if key.Matches(msg, vimEscBinding) {
		m.SetVimMode(ModeNormal)
		return
	}

	// Visual-mode operators consume the selection immediately.
	if (m.vim.mode == ModeVisualChar || m.vim.mode == ModeVisualLine) &&
		m.vim.pendingOp == 0 && m.vim.pendingFindKind == 0 {
		switch msg.String() {
		case "J":
			m.vimVisualJoin(true)
			return
		case ">", "<":
			amount := max(m.vim.pendingCount, 1)
			m.vim.pendingCount = 0
			r1, r2 := m.vimVisualRows()
			m.vimVisualOpStart()
			m.snapshotUndo()
			m.vimShiftLines(r1, r2, msg.String() == "<", amount)
			m.vim.mode = ModeNormal
			return
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
			if m.vim.onYank != nil {
				m.vim.onYank(m.vim.yankBuf, m.vim.yankLinewise)
			}
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
		case "J":
			m.vim.pendingOp = 0
			if m.vim.mode == ModeVisualChar || m.vim.mode == ModeVisualLine {
				m.vimVisualJoin(false)
			} else {
				count := m.vim.pendingCount
				m.vim.pendingCount = 0
				m.vimJoinCount(count, false)
			}
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
		if op == '>' || op == '<' {
			m.vimShiftLines(r1, vimExclusiveEndRow(r1, r2, c2), op == '<', 1)
			return
		}
		m.vimYankRange(r1, c1, r2, c2)
		if op != 'y' {
			m.vimDeleteRange(r1, c1, r2, c2)
		}
		if op == 'c' {
			m.vim.mode = ModeInsert
		}
		return
	}

	if op := m.vim.pendingOp; op == 'd' || op == 'c' || op == 'y' || op == '>' || op == '<' {
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
			if op == '>' || op == '<' {
				if r1, r2, ok := m.vimLineSpan(count); ok {
					m.snapshotUndo()
					m.vimShiftLines(r1, r2, op == '<', 1)
				}
				return
			}
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
		if op == '>' || op == '<' {
			if !linewise {
				r2 = vimExclusiveEndRow(r1, r2, c2)
			}
			m.vimShiftLines(min(r1, r2), max(r1, r2), op == '<', 1)
			return
		}
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
	case "enter":
		// <CR> in normal/visual mode: move down by count, then to first
		// non-blank of the resulting line. Mapped equivalent of `+`.
		for i := 0; i < count; i++ {
			m.CursorDown()
		}
		m.vimMotionFirstNonBlank()
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
	case "R":
		if m.vim.mode != ModeNormal {
			break
		}
		m.snapshotUndo()
		m.vim.mode = ModeReplace
		m.vim.replace = vimReplaceSession{count: count}
	case "J":
		m.vimJoinCount(rawCount, true)
	case ">", "<":
		m.vim.pendingOp = rune(s[0])
		m.vim.pendingCount = rawCount
	case "/", "?":
		m.vim.search.prompting = true
		m.vim.search.forward = s == "/"
		m.vim.search.input = nil
		m.vim.search.count = count
	case "n", "N":
		if m.vim.search.last != nil {
			m.vimSearchJump(m.vim.search.last, m.vim.search.lastForward == (s == "n"), count)
		}
	case "*", "#":
		m.vimSearchWordUnderCursor(s == "*", count)
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
	case "j", "enter":
		// <CR> in operator-pending mode is an alias for `j` (linewise down).
		// Real vim treats `d<CR>`/`c<CR>`/`y<CR>` as line-down operations,
		// matching the behavior of `dj`/`cj`/`yj`.
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

// vimLineSpan resolves the rows of a linewise command over count lines from
// the cursor, following vim's cursor_down: a count that runs past the last
// line clamps to it, but a count above 1 on the last line fails.
func (m *Model) vimLineSpan(count int) (r1, r2 int, ok bool) {
	last := len(m.value) - 1
	if count > 1 && m.row >= last {
		return 0, 0, false
	}
	return m.row, min(m.row+count-1, last), true
}

// vimVisualRows returns the first and last row touched by the visual
// selection.
func (m *Model) vimVisualRows() (r1, r2 int) {
	return min(m.vim.selStartRow, m.row), max(m.vim.selStartRow, m.row)
}

// vimVisualOpStart moves the cursor to the start of the visual selection, the
// position an operator on it starts from and its undo returns to.
func (m *Model) vimVisualOpStart() {
	r1, c1, _, _ := m.vimVisualRange()
	m.row = r1
	m.SetCursorColumn(c1)
}

// vimExclusiveEndRow returns the last row an exclusive range ending at
// (r2, c2) covers: an end at column 0 of a later row excludes that row.
func vimExclusiveEndRow(r1, r2, c2 int) int {
	if c2 == 0 && r2 > r1 {
		return r2 - 1
	}
	return r2
}

// vimJoinCount implements [count]J and [count]gJ: join count lines, at least
// two, starting at the cursor.
func (m *Model) vimJoinCount(count int, insertSpace bool) {
	r1, r2, ok := m.vimLineSpan(max(count, 2))
	if !ok {
		return
	}
	m.snapshotUndo()
	m.vimJoinLines(r1, r2, insertSpace)
}

// vimVisualJoin implements visual J and gJ: join the selected lines, at least
// two, and return to Normal mode.
func (m *Model) vimVisualJoin(insertSpace bool) {
	r1, r2 := m.vimVisualRows()
	m.vimVisualOpStart()
	m.vim.mode = ModeNormal
	m.vim.pendingCount = 0
	if r1 == r2 {
		r2++
	}
	if r2 >= len(m.value) {
		return
	}
	m.snapshotUndo()
	m.vimJoinLines(r1, r2, insertSpace)
}

// vimJoinLines joins rows r1..r2 into r1 the way vim's do_join does. With
// insertSpace (J), each joined line loses its leading whitespace and one space
// separates it from the text before, unless the joined text is empty or
// starts with ')', the text before is empty, or the previous line's text ends
// in whitespace ('joinspaces' off). Without insertSpace (gJ) the lines are
// concatenated unchanged. The cursor lands where the last line was joined.
func (m *Model) vimJoinLines(r1, r2 int, insertSpace bool) {
	joined := append([]rune(nil), m.value[r1]...)
	prevEnd := vimLastRune(joined)
	col := 0
	for r := r1 + 1; r <= r2; r++ {
		curr := m.value[r]
		col = len(joined)
		if insertSpace {
			curr = vimSkipWhite(curr)
			if len(curr) > 0 && curr[0] != ')' && len(joined) > 0 &&
				prevEnd != ' ' && prevEnd != '\t' {
				joined = append(joined, ' ')
			}
		}
		joined = append(joined, curr...)
		prevEnd = vimLastRune(curr)
	}
	m.value[r1] = joined
	m.value = append(m.value[:r1+1], m.value[r2+1:]...)
	m.row = r1
	m.SetCursorColumn(min(col, max(len(joined)-1, 0)))
	m.repositionView()
}

// vimSkipWhite returns line without its leading spaces and tabs.
func vimSkipWhite(line []rune) []rune {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[i:]
}

// vimLastRune returns the last rune of line, or 0 when line is empty.
func vimLastRune(line []rune) rune {
	if len(line) == 0 {
		return 0
	}
	return line[len(line)-1]
}

// vimShiftLines implements > and < over rows r1..r2: each non-empty line's
// indent moves by amount shiftwidths (tabWidth columns), never below zero,
// and is rewritten as spaces. The cursor moves to r1 and keeps its column,
// clamped to the shifted line, as vim does.
func (m *Model) vimShiftLines(r1, r2 int, left bool, amount int) {
	for r := r1; r <= r2; r++ {
		line := m.value[r]
		if len(line) == 0 {
			continue
		}
		indent, body := 0, 0
		for ; body < len(line) && (line[body] == ' ' || line[body] == '\t'); body++ {
			if line[body] == '\t' {
				indent += tabWidth - indent%tabWidth
			} else {
				indent++
			}
		}
		if left {
			indent = max(indent-amount*tabWidth, 0)
		} else {
			indent += amount * tabWidth
		}
		shifted := make([]rune, 0, indent+len(line)-body)
		shifted = append(shifted, []rune(strings.Repeat(" ", indent))...)
		m.value[r] = append(shifted, line[body:]...)
	}
	m.row = r1
	m.SetCursorColumn(min(m.col, max(len(m.value[r1])-1, 0)))
	m.repositionView()
}

// vimMatcher returns the rune offsets at which a search pattern matches line,
// in ascending order.
type vimMatcher func(line []rune) []int

// vimSearch holds the `/` `?` prompt and the last search for n and N.
type vimSearch struct {
	prompting   bool
	forward     bool // direction of the open prompt
	input       []rune
	count       int
	last        vimMatcher
	lastForward bool
}

// vimSearchPromptUpdate edits the open search prompt: Enter runs it, Esc
// cancels it, and Backspace on an empty prompt cancels it as vim does.
func (m *Model) vimSearchPromptUpdate(msg tea.KeyPressMsg) {
	p := &m.vim.search
	switch {
	case key.Matches(msg, vimEscBinding):
		p.prompting, p.input = false, nil
	case msg.String() == "enter":
		p.prompting = false
		m.vimSearchRun(string(p.input), p.forward, p.count)
		p.input = nil
	case msg.String() == "backspace":
		if len(p.input) == 0 {
			p.prompting = false
			return
		}
		p.input = p.input[:len(p.input)-1]
	default:
		if msg.Text != "" {
			p.input = append(p.input, []rune(msg.Text)...)
		} else if r, ok := singleRune(msg); ok {
			p.input = append(p.input, r)
		}
	}
}

// vimSearchRun compiles pattern and jumps to its count-th match. An empty
// pattern reuses the last one in the new direction; an invalid pattern
// leaves the cursor and the last search unchanged.
func (m *Model) vimSearchRun(pattern string, forward bool, count int) {
	match := m.vim.search.last
	if pattern != "" {
		re, err := vimCompilePattern(pattern)
		if err != nil {
			return
		}
		match = vimRegexpMatcher(re)
	}
	if match == nil {
		return
	}
	m.vim.search.last = match
	m.vim.search.lastForward = forward
	m.vimSearchJump(match, forward, count)
}

// vimSearchWordUnderCursor implements * and #: search for the keyword under
// or after the cursor as a whole word (or, on a line with no keyword there,
// the non-blank string), starting from its first character.
func (m *Model) vimSearchWordUnderCursor(forward bool, count int) {
	line := m.value[m.row]
	start, end, keyword := vimIdentAt(line, m.col)
	if start == end {
		return
	}
	word := append([]rune(nil), line[start:end]...)
	match := vimWordMatcher(word, keyword)
	m.vim.search.last = match
	m.vim.search.lastForward = forward
	m.SetCursorColumn(start)
	m.vimSearchJump(match, forward, count)
}

// vimIdentAt finds the text * and # search for, per vim's
// find_ident_under_cursor: the keyword at or after col, else the non-blank
// run at or after col. keyword reports which was found; start == end means
// neither was.
func vimIdentAt(line []rune, col int) (start, end int, keyword bool) {
	find := func(in func(rune) bool) (int, int, bool) {
		i := col
		for i < len(line) && !in(line[i]) {
			i++
		}
		if i == len(line) {
			return 0, 0, false
		}
		s, e := i, i
		for s > 0 && in(line[s-1]) {
			s--
		}
		for e < len(line) && in(line[e]) {
			e++
		}
		return s, e, true
	}
	if s, e, ok := find(isVimWordChar); ok {
		return s, e, true
	}
	nonBlank := func(r rune) bool { return !unicode.IsSpace(r) && !isVimWordChar(r) }
	s, e, _ := find(nonBlank)
	return s, e, false
}

// vimWordMatcher matches word literally; with whole, only where it is not
// adjacent to another keyword character (vim's \< \>).
func vimWordMatcher(word []rune, whole bool) vimMatcher {
	return func(line []rune) []int {
		var out []int
		for i := 0; i+len(word) <= len(line); i++ {
			if string(line[i:i+len(word)]) != string(word) {
				continue
			}
			end := i + len(word)
			if whole && ((i > 0 && isVimWordChar(line[i-1])) || (end < len(line) && isVimWordChar(line[end]))) {
				continue
			}
			out = append(out, i)
		}
		return out
	}
}

// vimRegexpMatcher matches re against each line. Matches do not overlap, as
// in vim.
func vimRegexpMatcher(re *regexp.Regexp) vimMatcher {
	return func(line []rune) []int {
		s := string(line)
		locs := re.FindAllStringIndex(s, -1)
		out := make([]int, 0, len(locs))
		for _, loc := range locs {
			out = append(out, utf8.RuneCountInString(s[:loc[0]]))
		}
		return out
	}
}

// vimSearchJump moves the cursor to the count-th match of match after (or,
// backward, before) the cursor, wrapping around the buffer ends as vim's
// 'wrapscan' does. It reports whether any match exists.
func (m *Model) vimSearchJump(match vimMatcher, forward bool, count int) bool {
	type pos struct{ row, col int }
	var hits []pos
	for r, line := range m.value {
		lastCol := -1
		for _, c := range match(line) {
			// A match at end of line sits on the last character, as the
			// Normal-mode cursor does.
			c = min(c, max(len(line)-1, 0))
			if c != lastCol {
				hits = append(hits, pos{r, c})
				lastCol = c
			}
		}
	}
	if len(hits) == 0 {
		return false
	}
	before := func(a, b pos) bool { return a.row < b.row || (a.row == b.row && a.col < b.col) }
	cur := pos{m.row, m.col}
	for range max(count, 1) {
		next := -1
		if forward {
			for i, h := range hits {
				if before(cur, h) {
					next = i
					break
				}
			}
			if next < 0 {
				next = 0
			}
		} else {
			for i := len(hits) - 1; i >= 0; i-- {
				if before(hits[i], cur) {
					next = i
					break
				}
			}
			if next < 0 {
				next = len(hits) - 1
			}
		}
		cur = hits[next]
	}
	m.row = cur.row
	m.SetCursorColumn(cur.col)
	m.repositionView()
	return true
}

// vimCompilePattern translates a vim pattern in the default 'magic' syntax to
// a Go regexp. `. * [] ^ $` are special as in vim; `( ) | + ? = {` are
// literal unless backslashed; `\< \>` become ASCII word boundaries; `\c`
// ignores case. Matching is case-sensitive by default ('ignorecase' off).
// Each line is matched on its own, so unlike vim a pattern never matches
// across a line break: `\n` matches nothing.
func vimCompilePattern(p string) (*regexp.Regexp, error) {
	rs := []rune(p)
	var b strings.Builder
	fold := false
	// atStart is true where vim treats ^ as an anchor and * as a literal:
	// the start of the pattern, of a group, or of an alternative.
	atStart := true
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		wasStart := atStart
		atStart = false
		if r == '\\' && i+1 < len(rs) {
			i++
			switch n := rs[i]; n {
			case '(':
				b.WriteString("(")
				atStart = true
			case '|':
				b.WriteString("|")
				atStart = true
			case ')', '+', '?':
				b.WriteRune(n)
			case '=':
				b.WriteString("?")
			case '{':
				j := i + 1
				for j < len(rs) && rs[j] != '}' {
					j++
				}
				if j == len(rs) {
					b.WriteString(`\{`)
					break
				}
				body := strings.TrimSuffix(string(rs[i+1:j]), `\`)
				lazy := strings.HasPrefix(body, "-")
				body = strings.TrimPrefix(body, "-")
				switch {
				case body == "":
					b.WriteString("*")
				case strings.HasPrefix(body, ","):
					b.WriteString("{0" + body + "}")
				default:
					b.WriteString("{" + body + "}")
				}
				if lazy {
					b.WriteString("?")
				}
				i = j
			case '<', '>':
				b.WriteString(`\b`)
			case 's', 'S', 'd', 'D', 'w', 'W', 'n', 't':
				b.WriteRune('\\')
				b.WriteRune(n)
			case 'c':
				fold = true
				atStart = wasStart
			case 'C':
				atStart = wasStart
			default:
				b.WriteString(regexp.QuoteMeta(string(n)))
			}
			continue
		}
		switch r {
		case '^':
			if wasStart {
				b.WriteString("^")
				atStart = true
			} else {
				b.WriteString(`\^`)
			}
		case '$':
			if i == len(rs)-1 || strings.HasPrefix(string(rs[i+1:]), `\|`) || strings.HasPrefix(string(rs[i+1:]), `\)`) {
				b.WriteString("$")
			} else {
				b.WriteString(`\$`)
			}
		case '*':
			if wasStart {
				b.WriteString(`\*`)
			} else {
				b.WriteString("*")
			}
		case '.':
			b.WriteString(".")
		case '[':
			end := vimCollectionEnd(rs, i)
			if end < 0 {
				b.WriteString(`\[`)
				break
			}
			b.WriteString(string(rs[i : end+1]))
			i = end
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	expr := b.String()
	if fold {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

// vimCollectionEnd returns the index of the ']' closing the collection that
// opens at rs[open], or -1 when it is unclosed (vim then matches '[' as a
// literal). A ']' right after '[' or '[^' is a member, not the close.
func vimCollectionEnd(rs []rune, open int) int {
	i := open + 1
	if i < len(rs) && rs[i] == '^' {
		i++
	}
	if i < len(rs) && rs[i] == ']' {
		i++
	}
	for ; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			i++
		case ']':
			return i
		}
	}
	return -1
}

// vimReplaceEdit records one character typed in Replace mode so Backspace
// can undo it: the rune it overwrote, an append past end of line, or a line
// break.
type vimReplaceEdit struct {
	orig     rune
	replaced bool
	newline  bool
}

// vimReplaceSession is the state of one `R` session: the edits Backspace can
// undo, the net typed text, and the count that repeats it on Esc.
type vimReplaceSession struct {
	edits []vimReplaceEdit
	typed []rune
	count int
}

// vimReplaceUpdate handles a key in Replace mode.
func (m *Model) vimReplaceUpdate(msg tea.KeyPressMsg) {
	switch {
	case key.Matches(msg, vimEscBinding):
		m.vimReplaceFinish()
	case msg.String() == "enter":
		m.vimReplaceInput([]rune{'\n'})
	case msg.String() == "backspace":
		m.vimReplaceBackspace()
	default:
		if msg.Text != "" {
			m.vimReplaceInput([]rune(msg.Text))
		} else if r, ok := singleRune(msg); ok {
			m.vimReplaceInput([]rune{r})
		}
	}
}

// vimReplaceInput types runes in Replace mode and records them for
// Backspace and the count repeat.
func (m *Model) vimReplaceInput(runes []rune) {
	for _, r := range m.san().Sanitize(runes) {
		if m.vimReplaceRune(r) {
			m.vim.replace.typed = append(m.vim.replace.typed, r)
		}
	}
}

// vimReplaceRune overwrites the character under the cursor with r, appends
// r at end of line, or breaks the line for '\n' (deleting nothing), as vim's
// Replace mode does. It reports whether r was applied.
func (m *Model) vimReplaceRune(r rune) bool {
	s := &m.vim.replace
	if r == '\n' {
		if m.atContentLimit() {
			return false
		}
		m.splitLine(m.row, m.col)
		s.edits = append(s.edits, vimReplaceEdit{newline: true})
		return true
	}
	line := m.value[m.row]
	if m.col < len(line) {
		s.edits = append(s.edits, vimReplaceEdit{orig: line[m.col], replaced: true})
		line[m.col] = r
	} else {
		if m.CharLimit > 0 && m.Length() >= m.CharLimit {
			return false
		}
		m.value[m.row] = append(line, r)
		s.edits = append(s.edits, vimReplaceEdit{})
	}
	m.SetCursorColumn(m.col + 1)
	return true
}

// vimReplaceBackspace undoes the last Replace-mode edit: an overwritten
// character is restored, an appended one or a line break is removed. Before
// the start of the session it only moves the cursor left.
func (m *Model) vimReplaceBackspace() {
	s := &m.vim.replace
	if len(s.edits) == 0 {
		m.vimMotionCharLeft()
		return
	}
	e := s.edits[len(s.edits)-1]
	s.edits = s.edits[:len(s.edits)-1]
	if len(s.typed) > 0 {
		s.typed = s.typed[:len(s.typed)-1]
	}
	if e.newline {
		m.mergeLineAbove(m.row)
		return
	}
	m.SetCursorColumn(m.col - 1)
	line := m.value[m.row]
	if e.replaced {
		line[m.col] = e.orig
		return
	}
	m.value[m.row] = append(line[:m.col], line[m.col+1:]...)
}

// vimReplaceFinish leaves Replace mode: the typed text is replayed count-1
// more times, still overwriting, and the cursor steps back onto the last
// typed character.
func (m *Model) vimReplaceFinish() {
	s := m.vim.replace
	for i := 1; i < s.count; i++ {
		for _, r := range s.typed {
			m.vimReplaceRune(r)
		}
	}
	m.vim.replace = vimReplaceSession{}
	m.vim.mode = ModeNormal
	m.vimMotionCharLeft()
}

// vimPasteIntoPrompt routes pasted text to the open search prompt or to
// Replace mode. It reports whether it consumed the paste.
func (m *Model) vimPasteIntoPrompt(runes []rune) bool {
	if !m.vimEnabled {
		return false
	}
	switch {
	case m.vim.search.prompting:
		for _, r := range runes {
			if r != '\n' && r != '\r' {
				m.vim.search.input = append(m.vim.search.input, r)
			}
		}
		return true
	case m.vim.mode == ModeReplace:
		m.vimReplaceInput(runes)
		return true
	}
	return false
}
