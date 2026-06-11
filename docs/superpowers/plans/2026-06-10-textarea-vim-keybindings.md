# Textarea Vim Keybindings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opt-in modal vim keybindings (Normal / Insert / Visual / Replace) to the `charm.land/bubbles/v2/textarea` component with motions, operators, text objects, counts, undo/redo, and a single unnamed yank register.

**Architecture:** A new in-package `vim.go` module owns mode state, operator-pending sequencing, and motion/operator implementations. `textarea.go` gets four new public methods (`SetVimEnabled`, `VimEnabled`, `VimMode`, `SetVimMode`), an embedded `vimState`, an `undoStack`, and a dispatcher hook at the top of `Update()` that routes non-Insert keypresses through `vimUpdate` while leaving Insert-mode dispatch untouched. Selection rendering composes inside the existing `view()` wrapped-line render loop. Vim is byte-identical-to-current when `VimEnabled() == false`.

**Tech Stack:** Go, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2/key`. No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-06-10-textarea-vim-keybindings-design.md`

**Test helpers already in `textarea_test.go`:** `newTextArea()`, `keyPress(rune)`, `sendString(m, str)`. We add: `keyCtrl(rune)`, `keyEsc()`, `vimSetup(t)`.

---

### Task 1: Add VimMode type, vimState, opt-in toggle, and dispatcher hook (no-op)

**Files:**
- Create: `textarea/vim.go`
- Create: `textarea/vim_test.go`
- Modify: `textarea/textarea.go` (add fields to `Model`, add 4 methods, add dispatcher hook in `Update`)

- [ ] **Step 1: Write the failing test**

Create `textarea/vim_test.go` with:

```go
package textarea

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyEsc returns a synthesized Esc KeyPressMsg.
func keyEsc() tea.Msg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}

// keyCtrl returns a synthesized Ctrl+<r> KeyPressMsg.
func keyCtrl(r rune) tea.Msg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl, Text: ""}
}

// vimSetup returns a focused textarea with vim enabled (Normal mode).
func vimSetup(t *testing.T) Model {
	t.Helper()
	m := newTextArea()
	m.SetVimEnabled(true)
	m, _ = m.Update(nil)
	return m
}

func TestVim_DefaultDisabled(t *testing.T) {
	m := newTextArea()
	if m.VimEnabled() {
		t.Fatalf("VimEnabled should be false by default")
	}
	if m.VimMode() != ModeInsert {
		t.Fatalf("VimMode should be ModeInsert by default, got %v", m.VimMode())
	}
}

func TestVim_EnableEntersNormal(t *testing.T) {
	m := vimSetup(t)
	if !m.VimEnabled() {
		t.Fatalf("VimEnabled should be true after SetVimEnabled(true)")
	}
	if m.VimMode() != ModeNormal {
		t.Fatalf("expected ModeNormal after enable, got %v", m.VimMode())
	}
}

func TestVim_DisableRestoresInsert(t *testing.T) {
	m := vimSetup(t)
	m.SetVimEnabled(false)
	if m.VimEnabled() {
		t.Fatalf("VimEnabled should be false after disable")
	}
	if m.VimMode() != ModeInsert {
		t.Fatalf("expected ModeInsert after disable, got %v", m.VimMode())
	}
}

func TestVim_NormalSwallowsRegularChars(t *testing.T) {
	m := vimSetup(t)
	// In Normal mode, typing 'x' should NOT insert 'x' as text
	// (Task 1 just swallows; Task 8 will give 'x' its delete meaning.)
	m, _ = m.Update(keyPress('x'))
	if got := m.Value(); got != "" {
		t.Fatalf("Normal mode should not insert chars, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_ -v`
Expected: FAIL with "undefined: ModeInsert" / "Model.SetVimEnabled undefined" / etc.

- [ ] **Step 3: Write minimal implementation**

Create `textarea/vim.go`:

```go
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
```

Now modify `textarea/textarea.go`. Add to the `Model` struct (after the `rsan` field):

```go
	// Vim-mode state. Only meaningful when vimEnabled is true.
	vimEnabled bool
	vim        vimState
```

Add the four public methods (anywhere sensible — e.g., after `SetVirtualCursor`):

```go
// VimEnabled reports whether vim modal keybindings are active.
func (m Model) VimEnabled() bool {
	return m.vimEnabled
}

// SetVimEnabled toggles vim modal keybindings. When enabled, the textarea
// starts in [ModeNormal]; when disabled, it returns to [ModeInsert] and all
// vim state (selection, pending operator, undo stack) is cleared.
func (m *Model) SetVimEnabled(enabled bool) {
	if enabled == m.vimEnabled {
		return
	}
	m.vimEnabled = enabled
	if enabled {
		m.vim = vimState{mode: ModeNormal}
	} else {
		m.vim = vimState{mode: ModeInsert}
	}
}

// VimMode returns the current vim mode. Always [ModeInsert] when vim is
// disabled.
func (m Model) VimMode() VimMode {
	if !m.vimEnabled {
		return ModeInsert
	}
	return m.vim.mode
}

// SetVimMode forces an immediate transition to the given mode. Clears any
// pending operator, count, or find prompt. No-op if vim is not enabled.
func (m *Model) SetVimMode(mode VimMode) {
	if !m.vimEnabled {
		return
	}
	m.vim.mode = mode
	m.vim.pendingOp = 0
	m.vim.pendingCount = 0
	m.vim.pendingFindKind = 0
}
```

Add the dispatcher hook inside the `case tea.KeyPressMsg:` branch of `Update()`, as the very first line of that case:

```go
		case tea.KeyPressMsg:
			if m.vimEnabled && m.vim.mode != ModeInsert {
				m.vimUpdate(msg)
				break
			}
			switch {
			// ... existing cases unchanged ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_ -v`
Expected: all four tests PASS.

Also run the full suite to ensure no regression:
Run: `go test ./textarea/ -v`
Expected: all existing tests PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go textarea/textarea.go
git commit -m "feat(textarea): add opt-in vim mode scaffold

Introduces VimMode, vimState, four public methods (SetVimEnabled,
VimEnabled, VimMode, SetVimMode), and a dispatcher hook in Update()
that swallows non-Insert keys via vimUpdate. No motions or edits yet."
```

---

### Task 2: Implement h/j/k/l motions and Esc Insert→Normal

**Files:**
- Modify: `textarea/vim.go` (flesh out `vimUpdate` to handle hjkl, add Esc-from-Insert support)
- Modify: `textarea/textarea.go` (add Esc check at top of Insert-mode switch)
- Modify: `textarea/vim_test.go` (add motion tests)

- [ ] **Step 1: Write the failing test**

Append to `textarea/vim_test.go`:

```go
func TestVim_BasicMotions_hjkl(t *testing.T) {
	cases := []struct {
		name    string
		seed    string
		row, col int
		key     rune
		wantRow, wantCol int
	}{
		{"l moves right", "hello", 0, 0, 'l', 0, 1},
		{"l at end of line stays put", "hi", 0, 2, 'l', 0, 2},
		{"h moves left", "hello", 0, 3, 'h', 0, 2},
		{"h at col 0 stays put", "hello", 0, 0, 'h', 0, 0},
		{"j moves down", "a\nb\nc", 0, 0, 'j', 1, 0},
		{"j at last row stays put", "a\nb", 1, 0, 'j', 1, 0},
		{"k moves up", "a\nb\nc", 2, 0, 'k', 1, 0},
		{"k at row 0 stays put", "a\nb", 0, 0, 'k', 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTextArea()
			m.SetValue(tc.seed)
			m.row = tc.row
			m.SetCursorColumn(tc.col)
			m.SetVimEnabled(true)
			m, _ = m.Update(keyPress(tc.key))
			if m.Line() != tc.wantRow || m.Column() != tc.wantCol {
				t.Fatalf("got row=%d col=%d, want row=%d col=%d",
					m.Line(), m.Column(), tc.wantRow, tc.wantCol)
			}
		})
	}
}

func TestVim_EscFromInsertEntersNormal(t *testing.T) {
	m := newTextArea()
	m.SetValue("hello")
	m.SetCursorColumn(3)
	m.SetVimEnabled(true)
	m.SetVimMode(ModeInsert)
	m, _ = m.Update(keyEsc())
	if m.VimMode() != ModeNormal {
		t.Fatalf("expected ModeNormal after Esc, got %v", m.VimMode())
	}
	// Vim convention: cursor moves left by 1 on Insert→Normal if col > 0.
	if m.Column() != 2 {
		t.Fatalf("expected col 2 after Esc, got %d", m.Column())
	}
}

func TestVim_EscFromInsertAtCol0StaysPut(t *testing.T) {
	m := newTextArea()
	m.SetValue("hello")
	m.SetCursorColumn(0)
	m.SetVimEnabled(true)
	m.SetVimMode(ModeInsert)
	m, _ = m.Update(keyEsc())
	if m.Column() != 0 {
		t.Fatalf("expected col 0 after Esc at col 0, got %d", m.Column())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_BasicMotions_hjkl -v`
Expected: FAIL — motions are no-ops (h on "hello" col 0 → col 0 expected 0, l on "hello" col 0 → col 0 actual but expected 1).

Run: `go test ./textarea/ -run TestVim_EscFromInsert -v`
Expected: FAIL — Esc not handled in Insert mode.

- [ ] **Step 3: Write minimal implementation**

Replace the stub `vimUpdate` in `textarea/vim.go`:

```go
func (m *Model) vimUpdate(msg tea.KeyPressMsg) bool {
	// Esc / Ctrl-[ from any non-Insert mode returns to Normal and clears
	// pending state.
	if key.Matches(msg, vimEscBinding) {
		m.SetVimMode(ModeNormal)
		return true
	}

	// Single-char dispatch for Task 2: hjkl only. Later tasks expand.
	switch msg.String() {
	case "h":
		m.vimMotionCharLeft()
	case "l":
		m.vimMotionCharRight()
	case "j":
		m.CursorDown()
	case "k":
		m.CursorUp()
	}
	return true
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
```

In `textarea/textarea.go`, add an Esc handler as the FIRST case in the Insert-mode `switch` block (inside `case tea.KeyPressMsg:` after the vim hook):

```go
			switch {
			case m.vimEnabled && key.Matches(msg, vimEscBinding):
				m.vim.mode = ModeNormal
				if m.col > 0 {
					m.SetCursorColumn(m.col - 1)
				}
			case key.Matches(msg, m.KeyMap.DeleteAfterCursor):
				// ... existing cases unchanged
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_ -v`
Expected: all TestVim_* PASS.

Run: `go test ./textarea/ -v`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go textarea/textarea.go
git commit -m "feat(textarea): add h/j/k/l motions and Insert→Normal on Esc"
```

---

### Task 3: Word motions w/b/e and W/B/E

**Files:**
- Modify: `textarea/vim.go` (add word/WORD motion implementations)
- Modify: `textarea/vim_test.go` (add tests)

- [ ] **Step 1: Write the failing test**

Append to `textarea/vim_test.go`:

```go
func TestVim_WordMotions(t *testing.T) {
	cases := []struct {
		name, seed string
		col        int
		key        rune
		wantCol    int
	}{
		// `w` — start of next word (alnum+_; punct is its own word).
		{"w over alnum", "foo bar", 0, 'w', 4},
		{"w skips punct boundary", "foo,bar", 0, 'w', 3},
		{"w from punct to alnum", "foo,bar", 3, 'w', 4},
		// `W` — start of next WORD (whitespace-delimited).
		{"W skips punct", "foo,bar baz", 0, 'W', 8},
		// `b` — start of previous word.
		{"b from word start", "foo bar", 4, 'b', 0},
		{"b across punct", "foo,bar", 4, 'b', 3},
		// `B` — start of previous WORD.
		{"B across punct", "foo,bar baz", 8, 'B', 0},
		// `e` — end of current/next word.
		{"e to end of word", "foo bar", 0, 'e', 2},
		{"e from end advances", "foo bar", 2, 'e', 6},
		// `E` — end of current/next WORD.
		{"E spans punct", "foo,bar baz", 0, 'E', 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTextArea()
			m.SetValue(tc.seed)
			m.SetCursorColumn(tc.col)
			m.SetVimEnabled(true)
			m, _ = m.Update(keyPress(tc.key))
			if m.Column() != tc.wantCol {
				t.Fatalf("got col=%d, want col=%d", m.Column(), tc.wantCol)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_WordMotions -v`
Expected: FAIL — w/b/e/W/B/E unhandled.

- [ ] **Step 3: Write minimal implementation**

Add to `textarea/vim.go`:

```go
import "unicode"

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
```

Extend the switch in `vimUpdate`:

```go
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
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_WordMotions -v`
Expected: all PASS.

Run: `go test ./textarea/ -v` — full suite PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add w/b/e and W/B/E word motions"
```

---

### Task 4: Line motions 0, ^, $, gg, G

**Files:**
- Modify: `textarea/vim.go` (add line motions and the `g` pending-prefix)
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_LineMotions(t *testing.T) {
	t.Run("0 goes to col 0", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("  hello")
		m.SetCursorColumn(5)
		m, _ = m.Update(keyPress('0'))
		if m.Column() != 0 {
			t.Fatalf("expected col 0, got %d", m.Column())
		}
	})
	t.Run("^ goes to first non-blank", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("   hi")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('^'))
		if m.Column() != 3 {
			t.Fatalf("expected col 3, got %d", m.Column())
		}
	})
	t.Run("$ goes to end of line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('$'))
		if m.Column() != 4 {
			t.Fatalf("expected col 4, got %d", m.Column())
		}
	})
	t.Run("gg goes to first line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 2
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('g'))
		m, _ = m.Update(keyPress('g'))
		if m.Line() != 0 {
			t.Fatalf("expected row 0, got %d", m.Line())
		}
	})
	t.Run("G goes to last line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m, _ = m.Update(keyPress('G'))
		if m.Line() != 2 {
			t.Fatalf("expected row 2, got %d", m.Line())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_LineMotions -v`
Expected: FAIL — none of `0 ^ $ gg G` handled.

- [ ] **Step 3: Write minimal implementation**

In `textarea/vim.go`:

```go
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
```

Extend `vimUpdate`. The `g` key needs pending-prefix handling: if `pendingOp == 'g'`, the next key forms a `gg`/`gu`/`gU` sequence. Add at the top of `vimUpdate` (after the Esc check):

```go
	// Pending `g` prefix (for gg). gu/gU come in Task 17.
	if m.vim.pendingOp == 'g' {
		m.vim.pendingOp = 0
		if msg.String() == "g" {
			m.vimMotionFirstLine()
		}
		return true
	}
```

Add the new cases to the main switch:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_LineMotions -v`
Expected: all PASS.
Run full suite: `go test ./textarea/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add 0/^/\$/gg/G line motions"
```

---

### Task 5: Inline find/till f/F/t/T and ; ,

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_FindMotions(t *testing.T) {
	cases := []struct {
		name string
		seq  []rune
		col  int
		want int
	}{
		{"f finds char forward", []rune{'f', 'r'}, 0, 8}, // "hello world" → 'r' at 8
		{"F finds char backward", []rune{'F', 'l'}, 9, 3},
		{"t lands one before forward", []rune{'t', 'r'}, 0, 7},
		{"T lands one after backward", []rune{'T', 'l'}, 9, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := vimSetup(t)
			m.SetValue("hello world")
			m.SetCursorColumn(tc.col)
			for _, r := range tc.seq {
				m, _ = m.Update(keyPress(r))
			}
			if m.Column() != tc.want {
				t.Fatalf("got col=%d, want col=%d", m.Column(), tc.want)
			}
		})
	}
}

func TestVim_FindRepeat(t *testing.T) {
	m := vimSetup(t)
	m.SetValue("a.b.c.d")
	m.SetCursorColumn(0)
	m, _ = m.Update(keyPress('f'))
	m, _ = m.Update(keyPress('.'))
	if m.Column() != 1 {
		t.Fatalf("first f: got col=%d, want 1", m.Column())
	}
	m, _ = m.Update(keyPress(';'))
	if m.Column() != 3 {
		t.Fatalf("; got col=%d, want 3", m.Column())
	}
	m, _ = m.Update(keyPress(','))
	if m.Column() != 1 {
		t.Fatalf(", got col=%d, want 1", m.Column())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_Find -v`
Expected: FAIL — find unhandled.

- [ ] **Step 3: Write minimal implementation**

Add to `textarea/vim.go`:

```go
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
```

In `vimUpdate`, add pending-find handling at the top (after Esc, before `g` prefix):

```go
	// Pending find target (f/F/t/T waiting for next char).
	if m.vim.pendingFindKind != 0 {
		kind := m.vim.pendingFindKind
		m.vim.pendingFindKind = 0
		if r, ok := singleRune(msg); ok {
			m.vimFindChar(kind, r)
			m.vim.lastFind = vimFind{kind: kind, ch: r}
		}
		return true
	}
```

Add to the main switch:

```go
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
```

Helper `singleRune` (add to `vim.go`):

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_Find -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add f/F/t/T find motions and ; , repeat"
```

---

### Task 6: Counts prefix on motions

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_Counts(t *testing.T) {
	t.Run("3l moves right 3", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("abcdef")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('l'))
		if m.Column() != 3 {
			t.Fatalf("got col=%d, want 3", m.Column())
		}
	})
	t.Run("3w skips 3 words", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("one two three four")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('w'))
		if m.Column() != 14 { // start of "four"
			t.Fatalf("got col=%d, want 14", m.Column())
		}
	})
	t.Run("count caps at 9999", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("abc")
		for _, d := range "99999" {
			m, _ = m.Update(keyPress(d))
		}
		// Verify count was clamped — sending `l` shouldn't loop forever.
		m, _ = m.Update(keyPress('l'))
		if m.Column() != 2 { // end of "abc"
			t.Fatalf("got col=%d, want 2", m.Column())
		}
	})
	t.Run("Esc clears pending count", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("abcdef")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('5'))
		m, _ = m.Update(keyEsc())
		m, _ = m.Update(keyPress('l'))
		if m.Column() != 1 {
			t.Fatalf("got col=%d, want 1 (count should have been cleared)", m.Column())
		}
	})
	t.Run("0 is motion when no count pending", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(3)
		m, _ = m.Update(keyPress('0'))
		if m.Column() != 0 {
			t.Fatalf("got col=%d, want 0", m.Column())
		}
	})
	t.Run("0 is digit when count pending", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue(strings.Repeat("a", 30))
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('1'))
		m, _ = m.Update(keyPress('0'))
		m, _ = m.Update(keyPress('l'))
		if m.Column() != 10 {
			t.Fatalf("got col=%d, want 10", m.Column())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_Counts -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `textarea/vim.go`, restructure `vimUpdate` to accumulate counts and apply them. Replace the body of `vimUpdate`:

```go
const vimMaxCount = 9999

func (m *Model) vimUpdate(msg tea.KeyPressMsg) bool {
	if key.Matches(msg, vimEscBinding) {
		m.SetVimMode(ModeNormal)
		return true
	}

	if m.vim.pendingFindKind != 0 {
		kind := m.vim.pendingFindKind
		m.vim.pendingFindKind = 0
		if r, ok := singleRune(msg); ok {
			m.vimFindChar(kind, r)
			m.vim.lastFind = vimFind{kind: kind, ch: r}
		}
		return true
	}

	if m.vim.pendingOp == 'g' {
		m.vim.pendingOp = 0
		if msg.String() == "g" {
			m.vimMotionFirstLine()
		}
		return true
	}

	s := msg.String()

	// Digit input — counts. '0' is a motion only when no count is pending.
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		m.vim.pendingCount = m.vim.pendingCount*10 + int(s[0]-'0')
		if m.vim.pendingCount > vimMaxCount {
			m.vim.pendingCount = vimMaxCount
		}
		return true
	}
	if s == "0" && m.vim.pendingCount > 0 {
		m.vim.pendingCount = m.vim.pendingCount * 10
		if m.vim.pendingCount > vimMaxCount {
			m.vim.pendingCount = vimMaxCount
		}
		return true
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
	}
	return true
}
```

Also update `SetVimMode` to clear `pendingFindKind` (already in spec):

```go
func (m *Model) SetVimMode(mode VimMode) {
	if !m.vimEnabled {
		return
	}
	m.vim.mode = mode
	m.vim.pendingOp = 0
	m.vim.pendingCount = 0
	m.vim.pendingFindKind = 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_Counts -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add count prefixes for vim motions"
```

---

### Task 7: Insert-mode entries i, I, a, A, o, O, s

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_InsertEntries(t *testing.T) {
	type tc struct {
		name      string
		seed      string
		row, col  int
		key       rune
		afterMode VimMode
		wantRow   int
		wantCol   int
		wantValue string // "" means unchanged
	}
	cases := []tc{
		{"i enters insert at cursor", "hello", 0, 2, 'i', ModeInsert, 0, 2, ""},
		{"a moves right then insert", "hello", 0, 2, 'a', ModeInsert, 0, 3, ""},
		{"I goes to first non-blank", "  hi", 0, 3, 'I', ModeInsert, 0, 2, ""},
		{"A goes to end of line", "hi", 0, 0, 'A', ModeInsert, 0, 2, ""},
		{"o opens line below", "a\nb", 0, 0, 'o', ModeInsert, 1, 0, "a\n\nb"},
		{"O opens line above", "a\nb", 1, 0, 'O', ModeInsert, 1, 0, "a\n\nb"},
		{"s deletes char and inserts", "hello", 0, 1, 's', ModeInsert, 0, 1, "hllo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newTextArea()
			m.SetValue(c.seed)
			m.row = c.row
			m.SetCursorColumn(c.col)
			m.SetVimEnabled(true)
			m, _ = m.Update(keyPress(c.key))
			if m.VimMode() != c.afterMode {
				t.Fatalf("mode: got %v want %v", m.VimMode(), c.afterMode)
			}
			if m.Line() != c.wantRow || m.Column() != c.wantCol {
				t.Fatalf("pos: got row=%d col=%d want row=%d col=%d",
					m.Line(), m.Column(), c.wantRow, c.wantCol)
			}
			if c.wantValue != "" && m.Value() != c.wantValue {
				t.Fatalf("value: got %q want %q", m.Value(), c.wantValue)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_InsertEntries -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Add cases to the switch in `vimUpdate` (in `textarea/vim.go`):

```go
	case "i":
		m.vim.mode = ModeInsert
	case "I":
		m.vimMotionFirstNonBlank()
		m.vim.mode = ModeInsert
	case "a":
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
```

Add helpers:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_InsertEntries -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add i/I/a/A/o/O/s insert-mode entries"
```

---

### Task 8: x, X, r{c}, ~ — single-char edits

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_CharEdits(t *testing.T) {
	t.Run("x deletes char under cursor", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('x'))
		if m.Value() != "hllo" {
			t.Fatalf("got %q want %q", m.Value(), "hllo")
		}
	})
	t.Run("X deletes char before cursor", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(2)
		m, _ = m.Update(keyPress('X'))
		if m.Value() != "hllo" {
			t.Fatalf("got %q", m.Value())
		}
		if m.Column() != 1 {
			t.Fatalf("col: got %d want 1", m.Column())
		}
	})
	t.Run("r replaces char", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('r'))
		m, _ = m.Update(keyPress('a'))
		if m.Value() != "hallo" {
			t.Fatalf("got %q", m.Value())
		}
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode: got %v want Normal", m.VimMode())
		}
	})
	t.Run("~ toggles case and advances", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('~'))
		if m.Value() != "Hello" {
			t.Fatalf("got %q", m.Value())
		}
		if m.Column() != 1 {
			t.Fatalf("col: got %d want 1", m.Column())
		}
	})
	t.Run("3x deletes 3 chars", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('x'))
		if m.Value() != "ho" {
			t.Fatalf("got %q", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_CharEdits -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Add a pending-replace branch at the top of `vimUpdate` after the Esc check:

```go
	// Pending r{c} — next char replaces the char under cursor.
	if m.vim.pendingOp == 'r' {
		m.vim.pendingOp = 0
		if r, ok := singleRune(msg); ok {
			m.vimReplaceCharUnderCursor(r)
		}
		return true
	}
```

Add to the switch:

```go
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
```

Add helpers:

```go
func (m *Model) vimReplaceCharUnderCursor(r rune) {
	if len(m.value[m.row]) == 0 || m.col >= len(m.value[m.row]) {
		return
	}
	m.value[m.row][m.col] = r
}

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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_CharEdits -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add x/X/r/~ single-char edits"
```

---

### Task 9: Undo/redo stack with u and Ctrl-r

**Files:**
- Modify: `textarea/vim.go` (add `undoStack`, snapshot helpers, u/Ctrl-r dispatch)
- Modify: `textarea/textarea.go` (add `undo undoStack` field; snapshot on Insert→Normal)
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_Undo(t *testing.T) {
	t.Run("u reverts x", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('x'))
		if m.Value() != "ello" {
			t.Fatalf("post-x: got %q", m.Value())
		}
		m, _ = m.Update(keyPress('u'))
		if m.Value() != "hello" {
			t.Fatalf("post-u: got %q", m.Value())
		}
	})
	t.Run("Ctrl-r redoes", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('x'))
		m, _ = m.Update(keyPress('u'))
		m, _ = m.Update(keyCtrl('r'))
		if m.Value() != "ello" {
			t.Fatalf("post-redo: got %q", m.Value())
		}
	})
	t.Run("edit after undo truncates redo", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("abcdef")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('x')) // bcdef
		m, _ = m.Update(keyPress('u')) // abcdef
		m, _ = m.Update(keyPress('x')) // bcdef (new edit)
		m, _ = m.Update(keyCtrl('r'))  // no-op
		if m.Value() != "bcdef" {
			t.Fatalf("got %q want bcdef", m.Value())
		}
	})
	t.Run("Insert session is one undo step", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a")
		m.SetCursorColumn(1)
		// Enter insert, type "bc", Esc.
		m, _ = m.Update(keyPress('a')) // append → Insert at col 1
		m, _ = m.Update(keyPress('b'))
		m, _ = m.Update(keyPress('c'))
		m, _ = m.Update(keyEsc())
		if m.Value() != "abc" {
			t.Fatalf("post-insert: got %q want abc", m.Value())
		}
		m, _ = m.Update(keyPress('u'))
		if m.Value() != "a" {
			t.Fatalf("post-undo: got %q want a", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_Undo -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Add to `textarea/vim.go`:

```go
type undoSnapshot struct {
	value    [][]rune
	row, col int
}

type undoStack struct {
	snaps    []undoSnapshot
	head     int // index of current state
	capacity int
}

const defaultUndoCapacity = 200

func cloneValue(v [][]rune) [][]rune {
	out := make([][]rune, len(v))
	for i, row := range v {
		out[i] = append([]rune(nil), row...)
	}
	return out
}

// push truncates the redo tail, appends a new snapshot, and bumps head.
// If the stack is at capacity, oldest is dropped.
func (u *undoStack) push(s undoSnapshot) {
	if u.capacity == 0 {
		u.capacity = defaultUndoCapacity
	}
	// Truncate redo tail.
	if u.head+1 < len(u.snaps) {
		u.snaps = u.snaps[:u.head+1]
	}
	u.snaps = append(u.snaps, s)
	if len(u.snaps) > u.capacity {
		drop := len(u.snaps) - u.capacity
		u.snaps = u.snaps[drop:]
	}
	u.head = len(u.snaps) - 1
}

// undo returns the previous snapshot, or nil if at the bottom.
func (u *undoStack) undo() *undoSnapshot {
	if u.head <= 0 {
		return nil
	}
	u.head--
	return &u.snaps[u.head]
}

// redo returns the next snapshot, or nil if at the top.
func (u *undoStack) redo() *undoSnapshot {
	if u.head+1 >= len(u.snaps) {
		return nil
	}
	u.head++
	return &u.snaps[u.head]
}

// snapshotUndo captures the current value+cursor on m.undo.
func (m *Model) snapshotUndo() {
	m.undo.push(undoSnapshot{
		value: cloneValue(m.value),
		row:   m.row,
		col:   m.col,
	})
}

// restoreSnapshot applies a snapshot to m.
func (m *Model) restoreSnapshot(s *undoSnapshot) {
	m.value = cloneValue(s.value)
	m.row = s.row
	m.SetCursorColumn(s.col)
}
```

In `textarea/textarea.go`, add `undo undoStack` to `Model`. In `SetVimEnabled(true)`, push an initial snapshot:

```go
func (m *Model) SetVimEnabled(enabled bool) {
	if enabled == m.vimEnabled {
		return
	}
	m.vimEnabled = enabled
	if enabled {
		m.vim = vimState{mode: ModeNormal}
		m.undo = undoStack{capacity: defaultUndoCapacity}
		m.snapshotUndo()
	} else {
		m.vim = vimState{mode: ModeInsert}
		m.undo = undoStack{}
	}
}
```

The Esc-from-Insert case in `Update` snapshots before mode change:

```go
			case m.vimEnabled && key.Matches(msg, vimEscBinding):
				m.snapshotUndo()
				m.vim.mode = ModeNormal
				if m.col > 0 {
					m.SetCursorColumn(m.col - 1)
				}
```

In `vimUpdate` switch, add:

```go
	case "u":
		if s := m.undo.undo(); s != nil {
			m.restoreSnapshot(s)
		}
```

Handle Ctrl-r. It comes in as `msg.String() == "ctrl+r"`:

```go
	case "ctrl+r":
		if s := m.undo.redo(); s != nil {
			m.restoreSnapshot(s)
		}
```

Snapshot before x/X/r/~ — wrap each at the top:

```go
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
	case "~":
		m.snapshotUndo()
		for i := 0; i < count; i++ {
			m.vimToggleCaseUnderCursor()
		}
```

In the pending-`r` branch, snapshot before replace:

```go
	if m.vim.pendingOp == 'r' {
		m.vim.pendingOp = 0
		if r, ok := singleRune(msg); ok {
			m.snapshotUndo()
			m.vimReplaceCharUnderCursor(r)
		}
		return true
	}
```

In the `o`/`O`/`s` cases (Task 7), prepend snapshots:

```go
	case "o":
		m.snapshotUndo()
		m.vimOpenLineBelow()
		m.vim.mode = ModeInsert
	case "O":
		m.snapshotUndo()
		m.vimOpenLineAbove()
		m.vim.mode = ModeInsert
	case "s":
		m.snapshotUndo()
		m.vimDeleteCharUnderCursor()
		m.vim.mode = ModeInsert
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_Undo -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go textarea/textarea.go
git commit -m "feat(textarea): add undo stack with u and Ctrl-r"
```

---

### Task 10: Linewise current-line ops dd, D, cc, C, yy, Y

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_LinewiseOps(t *testing.T) {
	t.Run("dd deletes current line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 1
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('d'))
		if m.Value() != "a\nc" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("dd of last line drops it", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 2
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('d'))
		if m.Value() != "a\nb" {
			t.Fatalf("got %q", m.Value())
		}
		if m.Line() != 1 {
			t.Fatalf("row: got %d want 1", m.Line())
		}
	})
	t.Run("D deletes to end of line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello world")
		m.SetCursorColumn(5)
		m, _ = m.Update(keyPress('D'))
		if m.Value() != "hello" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("C deletes to end and enters insert", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello world")
		m.SetCursorColumn(5)
		m, _ = m.Update(keyPress('C'))
		if m.Value() != "hello" {
			t.Fatalf("got %q", m.Value())
		}
		if m.VimMode() != ModeInsert {
			t.Fatalf("mode: got %v", m.VimMode())
		}
	})
	t.Run("cc clears line and enters insert", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nhello\nb")
		m.row = 1
		m, _ = m.Update(keyPress('c'))
		m, _ = m.Update(keyPress('c'))
		if m.Value() != "a\n\nb" {
			t.Fatalf("got %q", m.Value())
		}
		if m.VimMode() != ModeInsert {
			t.Fatalf("mode: got %v", m.VimMode())
		}
	})
	t.Run("yy yanks line linewise", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello\nworld")
		m.row = 0
		m, _ = m.Update(keyPress('y'))
		m, _ = m.Update(keyPress('y'))
		if m.vim.yankBuf != "hello" {
			t.Fatalf("yankBuf: got %q want %q", m.vim.yankBuf, "hello")
		}
		if !m.vim.yankLinewise {
			t.Fatalf("expected yankLinewise=true")
		}
	})
	t.Run("Y is alias for yy", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m, _ = m.Update(keyPress('Y'))
		if m.vim.yankBuf != "hello" || !m.vim.yankLinewise {
			t.Fatalf("Y didn't yank linewise: buf=%q line=%v", m.vim.yankBuf, m.vim.yankLinewise)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_LinewiseOps -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `vimUpdate`, intercept linewise operator doubles (`dd`, `cc`, `yy`) and the single-key Capital variants. Add pending-op handling for `d`/`c`/`y` (Task 12 will extend this for motions; for now, only the double-letter case matters):

```go
	// Handle pending d/c/y operators waiting for doubled letter (dd/cc/yy)
	// or a motion (added in Task 12).
	if m.vim.pendingOp == 'd' || m.vim.pendingOp == 'c' || m.vim.pendingOp == 'y' {
		op := m.vim.pendingOp
		m.vim.pendingOp = 0
		if string(op) == s {
			// Linewise op on current line.
			m.snapshotUndo()
			m.vimYankCurrentLine()
			m.vimDeleteCurrentLine()
			if op == 'c' {
				m.vimOpenInsertOnCurrentLine()
			}
			return true
		}
		// Other motions: deferred to Task 12. For now, just clear.
		return true
	}
```

Add the cases:

```go
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
```

Add helpers to `textarea/vim.go`:

```go
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

// vimOpenInsertOnCurrentLine clears the current row and enters insert mode.
func (m *Model) vimOpenInsertOnCurrentLine() {
	// The line was already deleted by vimDeleteCurrentLine which removed the
	// row. Re-insert a blank row at the same position.
	if m.row >= len(m.value) {
		m.value = append(m.value, []rune{})
	} else {
		m.value = append(m.value[:m.row+1], m.value[m.row:]...)
		m.value[m.row] = []rune{}
	}
	m.SetCursorColumn(0)
	m.vim.mode = ModeInsert
}
```

Wait — for `cc` we deleted the line and then need to re-add a blank one and enter insert. Simpler: `cc` should clear the line in place rather than delete+insert. Refactor:

```go
// vimClearCurrentLine empties the current row without removing it.
func (m *Model) vimClearCurrentLine() {
	m.value[m.row] = m.value[m.row][:0]
	m.SetCursorColumn(0)
}
```

And in the linewise-op handler:

```go
		if string(op) == s {
			m.snapshotUndo()
			m.vimYankCurrentLine()
			if op == 'c' {
				m.vimClearCurrentLine()
				m.vim.mode = ModeInsert
			} else {
				m.vimDeleteCurrentLine()
			}
			return true
		}
```

Remove the now-unused `vimOpenInsertOnCurrentLine` from the diff.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_LinewiseOps -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add dd/cc/yy/D/C/Y linewise ops"
```

---

### Task 11: Paste p and P (linewise + charwise)

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_Paste(t *testing.T) {
	t.Run("p pastes linewise after current row", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 0
		m, _ = m.Update(keyPress('y'))
		m, _ = m.Update(keyPress('y')) // yank "a" linewise
		m.row = 2                       // cursor on "c"
		m, _ = m.Update(keyPress('p'))
		if m.Value() != "a\nb\nc\na" {
			t.Fatalf("got %q", m.Value())
		}
		if m.Line() != 3 {
			t.Fatalf("row: got %d want 3", m.Line())
		}
	})
	t.Run("P pastes linewise before current row", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb")
		m.row = 0
		m.vim.yankBuf = "X"
		m.vim.yankLinewise = true
		m, _ = m.Update(keyPress('P'))
		if m.Value() != "X\na\nb" {
			t.Fatalf("got %q", m.Value())
		}
		if m.Line() != 0 {
			t.Fatalf("row: got %d want 0", m.Line())
		}
	})
	t.Run("p pastes charwise after cursor", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("ac")
		m.SetCursorColumn(0)
		m.vim.yankBuf = "b"
		m.vim.yankLinewise = false
		m, _ = m.Update(keyPress('p'))
		if m.Value() != "abc" {
			t.Fatalf("got %q", m.Value())
		}
		if m.Column() != 1 {
			t.Fatalf("col: got %d want 1", m.Column())
		}
	})
	t.Run("P pastes charwise before cursor", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("ac")
		m.SetCursorColumn(1)
		m.vim.yankBuf = "b"
		m.vim.yankLinewise = false
		m, _ = m.Update(keyPress('P'))
		if m.Value() != "abc" {
			t.Fatalf("got %q", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_Paste -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Add cases to the switch:

```go
	case "p":
		m.snapshotUndo()
		m.vimPaste(true) // after
	case "P":
		m.snapshotUndo()
		m.vimPaste(false) // before
```

Add helper:

```go
// vimPaste inserts yankBuf relative to the cursor. If linewise, the buffer
// becomes a new line above (before=true) or below (before=false). If charwise,
// chars are spliced in after (before=false) or at (before=true) the cursor.
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
	// Charwise.
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_Paste -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add p/P linewise and charwise paste"
```

---

### Task 12: Operator-over-motion (d{motion}, c{motion}, y{motion})

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_OperatorOverMotion(t *testing.T) {
	cases := []struct {
		name, seed string
		col        int
		keys       []rune
		wantValue  string
		wantYank   string
	}{
		{"dw deletes word", "foo bar", 0, []rune{'d', 'w'}, "bar", "foo "},
		{"d$ deletes to end of line", "hello world", 6, []rune{'d', '$'}, "hello ", "world"},
		{"d0 deletes to start", "hello", 3, []rune{'d', '0'}, "lo", "hel"},
		{"yw yanks word", "foo bar", 0, []rune{'y', 'w'}, "foo bar", "foo "},
		{"cw deletes word and enters insert", "foo bar", 0, []rune{'c', 'w'}, "bar", "foo "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := vimSetup(t)
			m.SetValue(tc.seed)
			m.SetCursorColumn(tc.col)
			for _, k := range tc.keys {
				m, _ = m.Update(keyPress(k))
			}
			if m.Value() != tc.wantValue {
				t.Fatalf("value: got %q want %q", m.Value(), tc.wantValue)
			}
			if m.vim.yankBuf != tc.wantYank {
				t.Fatalf("yank: got %q want %q", m.vim.yankBuf, tc.wantYank)
			}
		})
	}
}

func TestVim_OperatorEntersInsert_cw(t *testing.T) {
	m := vimSetup(t)
	m.SetValue("foo bar")
	m.SetCursorColumn(0)
	m, _ = m.Update(keyPress('c'))
	m, _ = m.Update(keyPress('w'))
	if m.VimMode() != ModeInsert {
		t.Fatalf("expected ModeInsert after cw, got %v", m.VimMode())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_OperatorOverMotion -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Refactor: motions need to return a range so operators can consume them. Add a motion-range helper that maps a single key to `(row1, col1, row2, col2, linewise)`:

```go
// vimMotionRange computes the range covered by a motion key starting from the
// current cursor. Returns the start (always cursor) and end position, and
// whether the motion is linewise. Returns ok=false if the key isn't a motion.
func (m *Model) vimMotionRange(s string, count int) (r1, c1, r2, c2 int, linewise bool, ok bool) {
	if count == 0 {
		count = 1
	}
	startRow, startCol := m.row, m.col
	// Save cursor, move with the motion, capture end, restore.
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
	default:
		return 0, 0, 0, 0, false, false
	}
	r2, c2 = m.row, m.col
	r1, c1 = startRow, startCol
	// Normalize so (r1,c1) <= (r2,c2).
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, c1, r2, c2 = r2, c2, r1, c1
	}
	return r1, c1, r2, c2, false, true
}
```

Replace the pending-op handler:

```go
	if m.vim.pendingOp == 'd' || m.vim.pendingOp == 'c' || m.vim.pendingOp == 'y' {
		op := m.vim.pendingOp
		m.vim.pendingOp = 0
		// Linewise doubled letter (dd/cc/yy).
		if string(op) == s {
			m.snapshotUndo()
			m.vimYankCurrentLine()
			if op == 'c' {
				m.vimClearCurrentLine()
				m.vim.mode = ModeInsert
			} else {
				m.vimDeleteCurrentLine()
			}
			return true
		}
		// Motion-based op.
		r1, c1, r2, c2, _, ok := m.vimMotionRange(s, count)
		if !ok {
			return true
		}
		m.snapshotUndo()
		m.vimYankRange(r1, c1, r2, c2)
		if op != 'y' {
			m.vimDeleteRange(r1, c1, r2, c2)
		}
		if op == 'c' {
			m.vim.mode = ModeInsert
		}
		return true
	}
```

Add range helpers:

```go
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
```

Add `import "strings"` to `vim.go` if not already present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_OperatorOverMotion -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add operator-over-motion (d/c/y + motion)"
```

---

### Task 13: Word text objects iw, aw

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_TextObjectsWord(t *testing.T) {
	t.Run("diw deletes inner word", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("foo bar baz")
		m.SetCursorColumn(5) // inside "bar"
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('i'))
		m, _ = m.Update(keyPress('w'))
		if m.Value() != "foo  baz" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("daw deletes a word including trailing space", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("foo bar baz")
		m.SetCursorColumn(5)
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('a'))
		m, _ = m.Update(keyPress('w'))
		if m.Value() != "foo baz" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("ciw enters insert", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("foo bar")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('c'))
		m, _ = m.Update(keyPress('i'))
		m, _ = m.Update(keyPress('w'))
		if m.VimMode() != ModeInsert {
			t.Fatalf("mode: got %v", m.VimMode())
		}
		if m.Value() != " bar" {
			t.Fatalf("value: got %q", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_TextObjectsWord -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Text objects are 2-char sequences after an operator (`iw`, `aw`, `i"`, etc.). Add a pending-text-object phase: when the operator is pending and the next key is `i` or `a`, we don't dispatch a motion — we wait for the object key.

Extend `vimState`:

```go
type vimState struct {
	mode             VimMode
	pendingOp        rune
	pendingCount     int
	pendingFindKind  rune
	pendingObject    rune // 'i' or 'a' (or 0)
	lastFind         vimFind
	selStartRow      int
	selStartCol      int
	yankBuf          string
	yankLinewise     bool
	savedCursorShape tea.CursorShape
}
```

In `vimUpdate`, after the Esc / pendingFindKind / pendingOp=='g' / pendingOp=='r' branches and BEFORE the `if m.vim.pendingOp == 'd' || ...` block, add:

```go
	// Pending text-object qualifier ('i' or 'a' after d/c/y).
	if m.vim.pendingObject != 0 {
		qual := m.vim.pendingObject
		op := m.vim.pendingOp
		m.vim.pendingObject = 0
		m.vim.pendingOp = 0
		r1, c1, r2, c2, ok := m.vimTextObjectRange(qual, s)
		if !ok {
			return true
		}
		m.snapshotUndo()
		m.vimYankRange(r1, c1, r2, c2)
		if op != 'y' {
			m.vimDeleteRange(r1, c1, r2, c2)
		}
		if op == 'c' {
			m.vim.mode = ModeInsert
		}
		return true
	}
```

In the existing pendingOp d/c/y branch, intercept `i` / `a` before falling through to motion:

```go
		if s == "i" || s == "a" {
			m.vim.pendingObject = rune(s[0])
			m.vim.pendingOp = op // keep the op set; the pending-object branch above will consume next key
			return true
		}
```

Add text-object resolver:

```go
// vimTextObjectRange resolves a text-object (i/a + obj key) to a range.
// inner=true means the 'i' variant; otherwise 'a' variant.
func (m *Model) vimTextObjectRange(qual rune, obj string) (r1, c1, r2, c2 int, ok bool) {
	inner := qual == 'i'
	switch obj {
	case "w":
		return m.vimObjectWord(inner)
	}
	return 0, 0, 0, 0, false
}

// vimObjectWord computes iw/aw bounds. iw = word at cursor (no surrounding
// whitespace). aw = word + trailing whitespace (or leading if at end).
func (m *Model) vimObjectWord(inner bool) (r1, c1, r2, c2 int, ok bool) {
	line := m.value[m.row]
	if len(line) == 0 || m.col >= len(line) {
		return 0, 0, 0, 0, false
	}
	cls := vimWordClass(line[m.col])
	if cls == 0 {
		// On whitespace — vim's iw on whitespace selects the whitespace run.
	}
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_TextObjectsWord -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add iw/aw word text objects"
```

---

### Task 14: Quote text objects i" a" i' a' i` a`

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_TextObjectsQuotes(t *testing.T) {
	t.Run(`di" deletes inside double quotes`, func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue(`foo "bar" baz`)
		m.SetCursorColumn(6) // inside "bar"
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('i'))
		m, _ = m.Update(keyPress('"'))
		if m.Value() != `foo "" baz` {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run(`da" deletes around double quotes`, func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue(`foo "bar" baz`)
		m.SetCursorColumn(6)
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('a'))
		m, _ = m.Update(keyPress('"'))
		if m.Value() != `foo  baz` {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("di' deletes inside single quotes", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue(`foo 'bar' baz`)
		m.SetCursorColumn(6)
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('i'))
		m, _ = m.Update(keyPress('\''))
		if m.Value() != `foo '' baz` {
			t.Fatalf("got %q", m.Value())
		}
	})
}
```

- [ ] **Step 3: Write minimal implementation**

(Step 2: Run test to verify it fails — `go test ./textarea/ -run TestVim_TextObjectsQuotes -v` → FAIL.)

Extend `vimTextObjectRange`:

```go
	case "\"":
		return m.vimObjectQuote(inner, '"')
	case "'":
		return m.vimObjectQuote(inner, '\'')
	case "`":
		return m.vimObjectQuote(inner, '`')
```

Add helper:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_TextObjectsQuotes -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add quote text objects (i\" a\" i' a' i\` a\`)"
```

---

### Task 15: Bracket text objects i( a( i{ a{ i[ a[ (single-line)

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_TextObjectsBrackets(t *testing.T) {
	cases := []struct {
		name, seed string
		col        int
		keys       []rune
		want       string
	}{
		{"di(", "f(bar)g", 3, []rune{'d', 'i', '('}, "f()g"},
		{"da(", "f(bar)g", 3, []rune{'d', 'a', '('}, "fg"},
		{"di{", "f{bar}g", 3, []rune{'d', 'i', '{'}, "f{}g"},
		{"di[", "f[bar]g", 3, []rune{'d', 'i', '['}, "f[]g"},
		{"di)", "f(bar)g", 3, []rune{'d', 'i', ')'}, "f()g"}, // closing bracket key works too
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := vimSetup(t)
			m.SetValue(tc.seed)
			m.SetCursorColumn(tc.col)
			for _, k := range tc.keys {
				m, _ = m.Update(keyPress(k))
			}
			if m.Value() != tc.want {
				t.Fatalf("got %q want %q", m.Value(), tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_TextObjectsBrackets -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Extend `vimTextObjectRange`:

```go
	case "(", ")":
		return m.vimObjectBracket(inner, '(', ')')
	case "{", "}":
		return m.vimObjectBracket(inner, '{', '}')
	case "[", "]":
		return m.vimObjectBracket(inner, '[', ']')
```

Add helper:

```go
// vimObjectBracket finds the matching open/close pair on the current line
// around or after the cursor. Single-line only.
func (m *Model) vimObjectBracket(inner bool, open, close rune) (r1, c1, r2, c2 int, ok bool) {
	line := m.value[m.row]
	// Walk back to the most recent unmatched open before/at cursor.
	depth := 0
	left := -1
	for i := m.col; i >= 0 && i < len(line); i-- {
		switch line[i] {
		case close:
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
		case close:
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_TextObjectsBrackets -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add bracket text objects (i( a( i{ a{ i[ a[)"
```

---

### Task 16: Paragraph text objects ip, ap

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_TextObjectsParagraph(t *testing.T) {
	t.Run("dip deletes paragraph at cursor", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\n\nc\nd\n\ne")
		m.row = 3 // on "c"
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('i'))
		m, _ = m.Update(keyPress('p'))
		if m.Value() != "a\nb\n\n\ne" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("dap deletes paragraph + trailing blank", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\n\nc\nd\n\ne")
		m.row = 3
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('a'))
		m, _ = m.Update(keyPress('p'))
		if m.Value() != "a\nb\n\ne" {
			t.Fatalf("got %q", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_TextObjectsParagraph -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Extend `vimTextObjectRange`:

```go
	case "p":
		return m.vimObjectParagraph(inner)
```

Add helper:

```go
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
	// Last paragraph: range to end of last line, then deletion truncates value.
	return top, 0, bot, len(m.value[bot]), true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_TextObjectsParagraph -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add ip/ap paragraph text objects"
```

---

### Task 17: Case operators gu{motion}, gU{motion}

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_CaseOperators(t *testing.T) {
	t.Run("guw lowercases word forward", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("FOO BAR")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('g'))
		m, _ = m.Update(keyPress('u'))
		m, _ = m.Update(keyPress('w'))
		if m.Value() != "foo BAR" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("gUw uppercases word forward", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("foo bar")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('g'))
		m, _ = m.Update(keyPress('U'))
		m, _ = m.Update(keyPress('w'))
		if m.Value() != "FOO bar" {
			t.Fatalf("got %q", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_CaseOperators -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In the `g`-prefix branch of `vimUpdate`, add `gu`/`gU` handling. Extend `vimState` with a `pendingCaseOp rune` (or reuse `pendingOp` with sentinel values 'U' / 'L'):

For simplicity, reuse `pendingOp`. Update the `g` prefix:

```go
	if m.vim.pendingOp == 'g' {
		switch msg.String() {
		case "g":
			m.vim.pendingOp = 0
			m.vimMotionFirstLine()
		case "u":
			m.vim.pendingOp = 'L' // pending "make lower over motion"
		case "U":
			m.vim.pendingOp = 'U' // pending "make upper over motion"
		default:
			m.vim.pendingOp = 0
		}
		return true
	}
```

Extend the d/c/y pending-op block to handle 'L' / 'U' similarly:

```go
	if m.vim.pendingOp == 'L' || m.vim.pendingOp == 'U' {
		op := m.vim.pendingOp
		m.vim.pendingOp = 0
		r1, c1, r2, c2, _, ok := m.vimMotionRange(s, count)
		if !ok {
			return true
		}
		m.snapshotUndo()
		m.vimApplyCase(r1, c1, r2, c2, op == 'U')
		return true
	}
```

Add helper:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_CaseOperators -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add gu/gU case operators over motion"
```

---

### Task 18: Visual mode entries (v, V) and motion extension

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_VisualEntry(t *testing.T) {
	t.Run("v enters visual char mode", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('v'))
		if m.VimMode() != ModeVisualChar {
			t.Fatalf("mode: got %v", m.VimMode())
		}
		if m.vim.selStartCol != 1 {
			t.Fatalf("selStartCol: got %d want 1", m.vim.selStartCol)
		}
	})
	t.Run("V enters visual line mode", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 1
		m, _ = m.Update(keyPress('V'))
		if m.VimMode() != ModeVisualLine {
			t.Fatalf("mode: got %v", m.VimMode())
		}
	})
	t.Run("motion extends visual selection", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('l'))
		if m.Column() != 2 {
			t.Fatalf("col: got %d", m.Column())
		}
		if m.vim.selStartCol != 0 {
			t.Fatalf("selStart unchanged: got %d", m.vim.selStartCol)
		}
	})
	t.Run("Esc exits visual", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyEsc())
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode: got %v", m.VimMode())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_VisualEntry -v`
Expected: FAIL — `v`/`V` unhandled.

- [ ] **Step 3: Write minimal implementation**

In `vimUpdate`, add cases:

```go
	case "v":
		m.vim.mode = ModeVisualChar
		m.vim.selStartRow = m.row
		m.vim.selStartCol = m.col
	case "V":
		m.vim.mode = ModeVisualLine
		m.vim.selStartRow = m.row
		m.vim.selStartCol = 0
```

Motion keys naturally work in Visual mode because `vimUpdate` runs the same switch regardless of mode. The selection range is implicit: `(selStartRow, selStartCol) .. (row, col)`.

The Esc case at the very top already returns to Normal — verify it does NOT clear `selStart*` (we want them retained for potential future re-selection `gv`, out of scope, but harmless to keep). Actually `SetVimMode` clears `pendingOp/Count/FindKind`, but leaves `selStart*` — fine.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_VisualEntry -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add v/V visual mode entries"
```

---

### Task 19: Visual mode consume (d, c, y, o, ~)

**Files:**
- Modify: `textarea/vim.go`
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_VisualConsume(t *testing.T) {
	t.Run("d on visual char selection deletes", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('l'))
		// Selection: cols 1..3 inclusive of cursor pos
		m, _ = m.Update(keyPress('d'))
		if m.Value() != "ho" {
			t.Fatalf("got %q want ho", m.Value())
		}
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode: got %v", m.VimMode())
		}
	})
	t.Run("y on visual yanks", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('y'))
		if m.vim.yankBuf != "el" {
			t.Fatalf("yank: got %q", m.vim.yankBuf)
		}
	})
	t.Run("c on visual deletes and enters insert", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('c'))
		if m.VimMode() != ModeInsert {
			t.Fatalf("mode: got %v", m.VimMode())
		}
		if m.Value() != "hllo" {
			t.Fatalf("got %q", m.Value())
		}
	})
	t.Run("o swaps anchor and cursor in visual", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(1)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('l'))
		// Anchor at col 1, cursor at col 3.
		m, _ = m.Update(keyPress('o'))
		if m.Column() != 1 {
			t.Fatalf("cursor: got %d want 1", m.Column())
		}
		if m.vim.selStartCol != 3 {
			t.Fatalf("anchor: got %d want 3", m.vim.selStartCol)
		}
	})
	t.Run("~ toggles case across visual", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('l'))
		m, _ = m.Update(keyPress('~'))
		if m.Value() != "HELlo" {
			t.Fatalf("got %q", m.Value())
		}
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode: got %v", m.VimMode())
		}
	})
	t.Run("V d on visual line deletes whole line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 1
		m, _ = m.Update(keyPress('V'))
		m, _ = m.Update(keyPress('d'))
		if m.Value() != "a\nc" {
			t.Fatalf("got %q", m.Value())
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_VisualConsume -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `vimUpdate`, at the very top of the main switch (BEFORE the operator-pending blocks), intercept the visual-mode operators:

```go
	// Visual-mode operators consume the selection immediately.
	if m.vim.mode == ModeVisualChar || m.vim.mode == ModeVisualLine {
		switch s {
		case "d", "x":
			m.snapshotUndo()
			r1, c1, r2, c2 := m.vimVisualRange()
			m.vimYankRange(r1, c1, r2, c2)
			m.vim.yankLinewise = m.vim.mode == ModeVisualLine
			m.vimDeleteRange(r1, c1, r2, c2)
			m.vim.mode = ModeNormal
			return true
		case "y":
			r1, c1, r2, c2 := m.vimVisualRange()
			m.vimYankRange(r1, c1, r2, c2)
			m.vim.yankLinewise = m.vim.mode == ModeVisualLine
			m.row = r1
			m.SetCursorColumn(c1)
			m.vim.mode = ModeNormal
			return true
		case "c":
			m.snapshotUndo()
			r1, c1, r2, c2 := m.vimVisualRange()
			m.vimYankRange(r1, c1, r2, c2)
			m.vim.yankLinewise = m.vim.mode == ModeVisualLine
			m.vimDeleteRange(r1, c1, r2, c2)
			m.vim.mode = ModeInsert
			return true
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
			return true
		case "o":
			m.row, m.vim.selStartRow = m.vim.selStartRow, m.row
			c := m.col
			m.SetCursorColumn(m.vim.selStartCol)
			m.vim.selStartCol = c
			return true
		}
	}
```

Add `vimVisualRange` helper:

```go
// vimVisualRange returns the normalized inclusive-exclusive range covered by
// the current visual selection. For ModeVisualChar the range is end-exclusive
// at c2; for ModeVisualLine, c1=0 and c2=length-of-last-row PLUS the trailing
// newline is treated implicitly by vimDeleteRange when r2 < len(value).
func (m *Model) vimVisualRange() (r1, c1, r2, c2 int) {
	r1, c1 = m.vim.selStartRow, m.vim.selStartCol
	r2, c2 = m.row, m.col
	if m.vim.mode == ModeVisualLine {
		if r1 > r2 {
			r1, r2 = r2, r1
		}
		c1 = 0
		// To delete whole rows including the trailing newline, target the
		// start of the row AFTER r2 — vimDeleteRange handles that already.
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_VisualConsume -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/vim_test.go
git commit -m "feat(textarea): add d/c/y/o/~ visual-mode consumers"
```

---

### Task 20: Render the visual selection in `view()`

**Files:**
- Modify: `textarea/textarea.go` (add `SelectedText` to `StyleState`, integrate selection rendering)
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_VisualRendering(t *testing.T) {
	m := vimSetup(t)
	m.Prompt = ""
	m.ShowLineNumbers = false
	m.SetWidth(20)
	// Apply a recognizable background to the selection.
	st := m.Styles()
	st.Focused.SelectedText = lipgloss.NewStyle().Background(lipgloss.Color("99"))
	m.SetStyles(st)

	m.SetValue("hello world")
	m.SetCursorColumn(0)
	m, _ = m.Update(keyPress('v'))
	m, _ = m.Update(keyPress('l'))
	m, _ = m.Update(keyPress('l'))
	// Selection covers "hel" (cols 0..2 inclusive).

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "hello world") {
		t.Fatalf("expected text in view, got %q", view)
	}
	// Sanity: the styled output should include the selected portion with the
	// selection style escape sequence. We check the raw (non-stripped) view
	// includes the bg color escape near 'h'.
	raw := m.View()
	if !strings.Contains(raw, "48;5;99") {
		t.Fatalf("selection style not present in view; raw=%q", raw)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_VisualRendering -v`
Expected: FAIL — selection not rendered.

- [ ] **Step 3: Write minimal implementation**

In `textarea/textarea.go`, add to `StyleState`:

```go
type StyleState struct {
	Base             lipgloss.Style
	Text             lipgloss.Style
	LineNumber       lipgloss.Style
	CursorLineNumber lipgloss.Style
	CursorLine       lipgloss.Style
	EndOfBuffer      lipgloss.Style
	Placeholder      lipgloss.Style
	Prompt           lipgloss.Style
	SelectedText     lipgloss.Style // NEW: vim visual-mode selection style
}
```

Add a computed accessor:

```go
func (s StyleState) computedSelectedText() lipgloss.Style {
	return s.SelectedText.Inherit(s.Base).Inline(true)
}
```

In `view()`, modify the per-wrapped-line rendering. Where it currently writes the cursor-line text:

```go
				if m.row == l && lineInfo.RowOffset == wl {
					s.WriteString(style.Render(string(wrappedLine[:lineInfo.ColumnOffset])))
					...
```

For Task 20, we ONLY render selection when `m.vimEnabled && (mode == ModeVisualChar || ModeVisualLine)`. Wrap the existing per-line render with a selection slicer.

Add helper:

```go
// vimVisualRangeForRender returns the normalized inclusive selection range
// (r1,c1) <= (r2,c2). For VisualLine, c1=0 and c2=len(row). Returns ok=false
// if not in visual mode.
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
	return r1, c1, r2, c2, true
}
```

Modify the inner-line render block in `view()`. Replace the existing cursor-line branch with one that splices in selection styling. The cleanest path: when in visual mode, render every wrapped line through a `renderWrappedLineWithSelection` helper; otherwise use the existing code path. Add the helper:

```go
// renderWrappedLineWithSelection renders a single wrapped line, splicing
// selection-styled segments where the visual selection intersects.
// `logicalRow` is the source row in m.value; `wrappedStart` is the column
// offset (within the logical row) of the first rune in wrappedLine.
func (m *Model) renderWrappedLineWithSelection(
	wrappedLine []rune,
	logicalRow int,
	wrappedStart int,
	baseStyle lipgloss.Style,
	cursorAtCol int, // -1 if cursor not on this wrapped line
) string {
	selR1, selC1, selR2, selC2, ok := m.vimVisualRangeForRender()
	styles := m.activeStyle()
	selStyle := styles.computedSelectedText().Inherit(baseStyle)

	if !ok || logicalRow < selR1 || logicalRow > selR2 {
		// Cursor handling stays in the existing code path.
		return baseStyle.Render(string(wrappedLine))
	}

	// Compute the slice of this wrapped line that falls inside the selection.
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
```

Integrate inside the loop where each `wrappedLine` is rendered. Find this block in `view()`:

```go
			if m.row == l && lineInfo.RowOffset == wl {
				s.WriteString(style.Render(string(wrappedLine[:lineInfo.ColumnOffset])))
				if m.col >= len(line) && lineInfo.CharOffset >= m.width {
					m.virtualCursor.SetChar(" ")
					s.WriteString(m.virtualCursor.View())
				} else {
					m.virtualCursor.SetChar(string(wrappedLine[lineInfo.ColumnOffset]))
					s.WriteString(style.Render(m.virtualCursor.View()))
					s.WriteString(style.Render(string(wrappedLine[lineInfo.ColumnOffset+1:])))
				}
			} else {
				s.WriteString(style.Render(string(wrappedLine)))
			}
```

Replace the `else` branch (non-cursor lines) with selection-aware rendering, and route the cursor branch's pre/post slices through the selection-aware helper too. For brevity, swap:

```go
			} else {
				wrappedStart := 0
				// Approximate: count chars before this wrapped line by summing
				// preceding wrapped sublines.
				for prevWL := 0; prevWL < wl; prevWL++ {
					wrappedStart += len(wrappedLines[prevWL])
				}
				s.WriteString(m.renderWrappedLineWithSelection(wrappedLine, l, wrappedStart, style, -1))
			}
```

For the cursor branch, keep its existing rendering — the visual cursor sits on top of whatever color the selection had, which matches vim's behavior. Selection styling on the non-cursor portion of the cursor line is handled by rendering the pre and post slices through the helper:

```go
			if m.row == l && lineInfo.RowOffset == wl {
				wrappedStart := 0
				for prevWL := 0; prevWL < wl; prevWL++ {
					wrappedStart += len(wrappedLines[prevWL])
				}
				pre := wrappedLine[:lineInfo.ColumnOffset]
				s.WriteString(m.renderWrappedLineWithSelection(pre, l, wrappedStart, style, -1))
				if m.col >= len(line) && lineInfo.CharOffset >= m.width {
					m.virtualCursor.SetChar(" ")
					s.WriteString(m.virtualCursor.View())
				} else {
					m.virtualCursor.SetChar(string(wrappedLine[lineInfo.ColumnOffset]))
					s.WriteString(style.Render(m.virtualCursor.View()))
					post := wrappedLine[lineInfo.ColumnOffset+1:]
					s.WriteString(m.renderWrappedLineWithSelection(post, l, wrappedStart+lineInfo.ColumnOffset+1, style, -1))
				}
			} else {
				wrappedStart := 0
				for prevWL := 0; prevWL < wl; prevWL++ {
					wrappedStart += len(wrappedLines[prevWL])
				}
				s.WriteString(m.renderWrappedLineWithSelection(wrappedLine, l, wrappedStart, style, -1))
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_VisualRendering -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/textarea.go textarea/vim_test.go
git commit -m "feat(textarea): render vim visual selection in view()"
```

---

### Task 21: Cursor shapes per vim mode

**Files:**
- Modify: `textarea/textarea.go` (add `applyVimCursorShape`, hook on enable/disable and mode change)
- Modify: `textarea/vim.go` (call `applyVimCursorShape` after every mode change)
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_CursorShapes(t *testing.T) {
	t.Run("Normal mode uses Block", func(t *testing.T) {
		m := vimSetup(t)
		if got := m.Styles().Cursor.Shape; got != tea.CursorBlock {
			t.Fatalf("shape: got %v want Block", got)
		}
	})
	t.Run("Insert mode uses Bar", func(t *testing.T) {
		m := vimSetup(t)
		m, _ = m.Update(keyPress('i'))
		if got := m.Styles().Cursor.Shape; got != tea.CursorBar {
			t.Fatalf("shape: got %v want Bar", got)
		}
	})
	t.Run("Replace mode uses Underline", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('r'))
		if got := m.Styles().Cursor.Shape; got != tea.CursorUnderline {
			t.Fatalf("shape during pending r: got %v want Underline", got)
		}
	})
	t.Run("disable restores original shape", func(t *testing.T) {
		m := newTextArea()
		original := m.Styles().Cursor.Shape
		m.SetVimEnabled(true)
		m.SetVimEnabled(false)
		if got := m.Styles().Cursor.Shape; got != original {
			t.Fatalf("shape after disable: got %v want %v", got, original)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./textarea/ -run TestVim_CursorShapes -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Add to `textarea/textarea.go`:

```go
// applyVimCursorShape sets the cursor shape based on the current vim mode.
// No-op if vim is not enabled.
func (m *Model) applyVimCursorShape() {
	if !m.vimEnabled {
		return
	}
	var shape tea.CursorShape
	switch m.vim.mode {
	case ModeInsert:
		shape = tea.CursorBar
	case ModeReplace:
		shape = tea.CursorUnderline
	default:
		shape = tea.CursorBlock
	}
	m.styles.Cursor.Shape = shape
	m.updateVirtualCursorStyle()
}
```

Update `SetVimEnabled` to stash and restore:

```go
func (m *Model) SetVimEnabled(enabled bool) {
	if enabled == m.vimEnabled {
		return
	}
	if enabled {
		m.vim = vimState{mode: ModeNormal, savedCursorShape: m.styles.Cursor.Shape}
		m.vimEnabled = true
		m.undo = undoStack{capacity: defaultUndoCapacity}
		m.snapshotUndo()
		m.applyVimCursorShape()
	} else {
		m.styles.Cursor.Shape = m.vim.savedCursorShape
		m.vimEnabled = false
		m.vim = vimState{mode: ModeInsert}
		m.undo = undoStack{}
		m.updateVirtualCursorStyle()
	}
}
```

Update `SetVimMode` to apply the shape:

```go
func (m *Model) SetVimMode(mode VimMode) {
	if !m.vimEnabled {
		return
	}
	m.vim.mode = mode
	m.vim.pendingOp = 0
	m.vim.pendingCount = 0
	m.vim.pendingFindKind = 0
	m.applyVimCursorShape()
}
```

In `vim.go`, after every `m.vim.mode = ...` assignment in `vimUpdate`, append `m.applyVimCursorShape()`. There are several places — easiest pattern: at the end of `vimUpdate` (just before `return true`), unconditionally call:

```go
	m.applyVimCursorShape()
	return true
```

Replace each `return true` near the top of the function with `goto end` and add an `end:` label, OR refactor each branch to set a local `applied = true` and call at the bottom. Simplest: leave the early returns alone (each is a single-step shortcut), and add `defer m.applyVimCursorShape()` at the top of `vimUpdate`:

```go
func (m *Model) vimUpdate(msg tea.KeyPressMsg) bool {
	defer m.applyVimCursorShape()
	// ...
}
```

Also call from the Esc-Insert→Normal case in `Update`:

```go
			case m.vimEnabled && key.Matches(msg, vimEscBinding):
				m.snapshotUndo()
				m.vim.mode = ModeNormal
				if m.col > 0 {
					m.SetCursorColumn(m.col - 1)
				}
				m.applyVimCursorShape()
```

For Replace mode shape during pending r: set `m.vim.mode = ModeReplace` when `r` is pressed, and switch back to ModeNormal once the char is consumed. Replace the `"r"` case:

```go
	case "r":
		m.vim.pendingOp = 'r'
		m.vim.mode = ModeReplace
```

And in the pending-`r` branch:

```go
	if m.vim.pendingOp == 'r' {
		m.vim.pendingOp = 0
		if r, ok := singleRune(msg); ok {
			m.snapshotUndo()
			m.vimReplaceCharUnderCursor(r)
		}
		m.vim.mode = ModeNormal
		return true
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./textarea/ -run TestVim_CursorShapes -v`
Expected: PASS.
Run: `go test ./textarea/ -v` → all PASS.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/textarea.go textarea/vim_test.go
git commit -m "feat(textarea): swap cursor shape per vim mode"
```

---

### Task 22: Final polish — SetVimMode programmatic round-trip and doc comments

**Files:**
- Modify: `textarea/textarea.go` (doc comments on public methods)
- Modify: `textarea/vim.go` (doc comments on exported types)
- Modify: `textarea/vim_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestVim_SetVimModeProgrammatic(t *testing.T) {
	m := vimSetup(t)
	m.SetVimMode(ModeInsert)
	if m.VimMode() != ModeInsert {
		t.Fatalf("mode: got %v", m.VimMode())
	}
	if m.Styles().Cursor.Shape != tea.CursorBar {
		t.Fatalf("shape: got %v want Bar", m.Styles().Cursor.Shape)
	}
	m.SetVimMode(ModeNormal)
	if m.VimMode() != ModeNormal {
		t.Fatalf("mode: got %v", m.VimMode())
	}
}

func TestVim_DisabledIgnoresSetVimMode(t *testing.T) {
	m := newTextArea()
	m.SetVimMode(ModeNormal) // disabled — no-op
	if m.VimMode() != ModeInsert {
		t.Fatalf("mode: got %v want Insert (vim disabled)", m.VimMode())
	}
}

func TestVim_EnabledFromCol0Insert(t *testing.T) {
	// Regression: enabling vim while focus is at col 0 must NOT corrupt cursor.
	m := newTextArea()
	m.SetValue("hello")
	m.SetCursorColumn(0)
	m.SetVimEnabled(true)
	if m.Column() != 0 || m.Line() != 0 {
		t.Fatalf("pos: got row=%d col=%d", m.Line(), m.Column())
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./textarea/ -run TestVim_SetVimMode -v` and `TestVim_DisabledIgnoresSetVimMode` and `TestVim_EnabledFromCol0Insert -v`.

If all pass, no implementation needed — proceed to doc comments. If any fails, fix the underlying bug.

- [ ] **Step 3: Add doc comments**

Verify the doc comments on `VimMode`, `ModeInsert..ModeReplace`, `VimEnabled`, `SetVimEnabled`, `VimMode()`, `SetVimMode`, and `StyleState.SelectedText` are all present and accurate. Add a package-level doc paragraph to the top of `textarea/vim.go`:

```go
// This file implements opt-in vim modal keybindings for the textarea. The
// surface covers Normal / Insert / Visual / Replace modes, motions, operators
// over motions, text objects, counts, and a single unnamed yank register.
// Marks, macros, named registers, search, and block visual are out of scope.
// See docs/superpowers/specs/2026-06-10-textarea-vim-keybindings-design.md.
```

- [ ] **Step 4: Run all tests**

Run: `go test ./textarea/ -v`
Expected: all PASS.

Run: `go vet ./textarea/`
Expected: no issues.

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add textarea/vim.go textarea/textarea.go textarea/vim_test.go
git commit -m "feat(textarea): polish vim mode docs and round-trip tests"
```

---

## Self-Review

**Spec coverage check:**

| Spec section | Implemented in task |
| --- | --- |
| Public API (4 methods) | 1, 22 |
| `VimMode` constants | 1 |
| `vimState` struct | 1, 13 (extended with `pendingObject`), 21 (cursor swap) |
| Mode transitions Esc | 2, 18 |
| Pending operator state machine | 10, 12, 13, 17 |
| Counts | 6 |
| Word motions (vim word vs WORD) | 3 |
| Line motions (0 ^ $ gg G) | 4 |
| Find/till and ; , | 5 |
| Insert entries i I a A o O s | 7 |
| x X r ~ | 8, 21 (Replace mode shape) |
| Linewise dd cc yy D C Y | 10 |
| Yank buffer + p / P | 10, 11 |
| Operator-over-motion d/c/y + motion | 12 |
| Text objects iw aw | 13 |
| Text objects quoted strings | 14 |
| Text objects brackets (single-line) | 15 |
| Text objects ip ap | 16 |
| gu gU over motion | 17 |
| Visual char + line entries | 18 |
| Visual consume (d c y o ~) | 19 |
| Selection rendering in view() | 20 |
| Cursor shapes per mode | 21 |
| Undo/redo (snapshots + u + Ctrl-r) | 9 |
| `SelectedText` field on `StyleState` | 20 |
| Default-disabled invariant | 1, 22 |

**Placeholder scan:** No TBDs, no "implement later", no "fill in details", no untyped "add appropriate error handling". Every step contains real code.

**Type consistency check:** Method names verified across tasks — `vimMotionWordForward`, `vimMotionFirstNonBlank`, `vimDeleteRange`, `vimYankRange`, `vimVisualRange`, `snapshotUndo`, `restoreSnapshot`, `applyVimCursorShape`, `vimTextObjectRange` are used consistently. The `vimState.pendingObject` field added in Task 13 is referenced in Task 13 only; the linewise-op handler in Task 10 doesn't reference it. The `pendingOp` field is repurposed in Task 17 to carry sentinel values 'L' / 'U' / 'g' / 'r' — documented in code via the consts.

**Behavioral risk noted:** Task 20's selection rendering touches `view()` which is performance-critical (called on every Update). The added work is bounded by the rendered viewport rows × line width, same as the existing renderer. No memoization concerns because vim mode + selection state isn't keyed in the wrap cache (the wrap cache only stores `(runes, width) -> [][]rune`; it's untouched).

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-06-10-textarea-vim-keybindings.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints.

Which approach?
