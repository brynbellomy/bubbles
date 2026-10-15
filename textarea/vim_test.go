package textarea

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// keyEsc returns a synthesized Esc KeyPressMsg.
func keyEsc() tea.Msg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}

// keyEnter returns a synthesized Enter KeyPressMsg.
func keyEnter() tea.Msg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
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

func TestVim_BasicMotions_hjkl(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		row, col int
		key      rune
		wantRow  int
		wantCol  int
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

func TestVim_EnterMotionNormal(t *testing.T) {
	t.Run("enter moves down then to first non-blank", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello\n   world")
		m.row = 0
		m.SetCursorColumn(2)
		m, _ = m.Update(keyEnter())
		if m.Line() != 1 {
			t.Fatalf("row: got %d want 1", m.Line())
		}
		if m.Column() != 3 {
			t.Fatalf("col: got %d want 3 (first non-blank)", m.Column())
		}
	})
	t.Run("enter at last row stays put on first non-blank", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\n  b")
		m.row = 1
		m.SetCursorColumn(0)
		m, _ = m.Update(keyEnter())
		if m.Line() != 1 {
			t.Fatalf("row: got %d want 1", m.Line())
		}
		// CursorDown is a no-op at last row; first-non-blank still adjusts col.
		if m.Column() != 2 {
			t.Fatalf("col: got %d want 2", m.Column())
		}
	})
	t.Run("enter extends visual selection down a line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello\n  world")
		m.row = 0
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyEnter())
		if m.VimMode() != ModeVisualChar {
			t.Fatalf("mode: got %v want ModeVisualChar", m.VimMode())
		}
		if m.Line() != 1 || m.Column() != 2 {
			t.Fatalf("cursor: got row=%d col=%d want row=1 col=2", m.Line(), m.Column())
		}
		if m.vim.selStartRow != 0 || m.vim.selStartCol != 0 {
			t.Fatalf("anchor moved: got row=%d col=%d", m.vim.selStartRow, m.vim.selStartCol)
		}
	})
	t.Run("enter in visual line extends row range", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 0
		m, _ = m.Update(keyPress('V'))
		m, _ = m.Update(keyEnter())
		if m.VimMode() != ModeVisualLine {
			t.Fatalf("mode: got %v want ModeVisualLine", m.VimMode())
		}
		if m.Line() != 1 {
			t.Fatalf("row: got %d want 1", m.Line())
		}
		if m.vim.selStartRow != 0 {
			t.Fatalf("anchor row moved: got %d want 0", m.vim.selStartRow)
		}
	})
	t.Run("d<CR> deletes current line and next line (linewise)", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc\nd")
		m.row = 1
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyEnter())
		// d<CR> is dj — delete current row + next row, leaving "a\nd".
		got := m.Value()
		if got != "a\nd" {
			t.Fatalf("value after d<CR>: got %q want %q", got, "a\nd")
		}
	})
	t.Run("y<CR> yanks current line and next line (linewise)", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc\nd")
		m.row = 1
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('y'))
		m, _ = m.Update(keyEnter())
		// y<CR> is yj — yank rows 1+2 linewise without mutating buffer.
		if got := m.Value(); got != "a\nb\nc\nd" {
			t.Fatalf("y<CR> should not mutate buffer: got %q", got)
		}
		if !m.vim.yankLinewise {
			t.Fatalf("y<CR> should yank linewise")
		}
		if want := "b\nc"; m.vim.yankBuf != want {
			t.Fatalf("yank buf: got %q want %q", m.vim.yankBuf, want)
		}
	})
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
		// Enter insert via 'a', type "bc", Esc.
		m, _ = m.Update(keyPress('a')) // append: mode Insert
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

func TestVim_Paste(t *testing.T) {
	t.Run("p pastes linewise after current row", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc")
		m.row = 0
		m, _ = m.Update(keyPress('y'))
		m, _ = m.Update(keyPress('y')) // yank "a" linewise
		m.row = 2                      // cursor on "c"
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
		{"di)", "f(bar)g", 3, []rune{'d', 'i', ')'}, "f()g"},
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
		if m.Value() != "hlo" {
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
	// Sanity: the styled output should include the selection style escape
	// sequence (256-color background 99).
	raw := m.View()
	if !strings.Contains(raw, "48;5;99") {
		t.Fatalf("selection style not present in view; raw=%q", raw)
	}
}

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

func TestVim_CountedOperatorMotion(t *testing.T) {
	t.Run("3dw deletes 3 words", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("one two three four")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('w'))
		if m.Value() != "four" {
			t.Fatalf("got %q want %q", m.Value(), "four")
		}
	})
	t.Run("d3w same as 3dw", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("one two three four")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('w'))
		if m.Value() != "four" {
			t.Fatalf("got %q want %q", m.Value(), "four")
		}
	})
	t.Run("3dd deletes 3 lines", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc\nd\ne")
		m.row = 1
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('d'))
		if m.Value() != "a\ne" {
			t.Fatalf("got %q want %q", m.Value(), "a\ne")
		}
	})
}

func TestVim_xYanksToBuffer(t *testing.T) {
	t.Run("x then p round-trips", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('x')) // delete 'h', value "ello"
		m, _ = m.Update(keyPress('p')) // paste after cursor (col 0 → after)
		if m.Value() != "ehllo" {
			t.Fatalf("got %q want %q", m.Value(), "ehllo")
		}
	})
	t.Run("3x yanks 3 chars", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m, _ = m.Update(keyPress('3'))
		m, _ = m.Update(keyPress('x'))
		if m.vim.yankBuf != "hel" {
			t.Fatalf("yank: got %q want hel", m.vim.yankBuf)
		}
	})
}

func TestVim_LinewiseOpMotions(t *testing.T) {
	t.Run("dj deletes current and next line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc\nd")
		m.row = 1
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('j'))
		if m.Value() != "a\nd" {
			t.Fatalf("got %q want %q", m.Value(), "a\nd")
		}
	})
	t.Run("dG deletes from current to last line", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("a\nb\nc\nd")
		m.row = 1
		m, _ = m.Update(keyPress('d'))
		m, _ = m.Update(keyPress('G'))
		if m.Value() != "a" {
			t.Fatalf("got %q want %q", m.Value(), "a")
		}
	})
}

// TestVim_VisualSelectionVisibleWithDefaultStyles guards the default: a
// consumer that configures nothing should still see the selection. Before
// DefaultStyles supplied a SelectedText style, visual mode rendered
// identically to normal mode and the selection was invisible.
func TestVim_VisualSelectionVisibleWithDefaultStyles(t *testing.T) {
	render := func(visual bool) string {
		m := vimSetup(t)
		m.Prompt = ""
		m.ShowLineNumbers = false
		m.SetWidth(20)
		m.SetValue("hello world")
		m.SetCursorColumn(0)
		if visual {
			m, _ = m.Update(keyPress('v'))
			m, _ = m.Update(keyPress('l'))
			m, _ = m.Update(keyPress('l'))
		}
		return m.View()
	}

	plain, selected := render(false), render(true)
	if ansi.Strip(plain) != ansi.Strip(selected) {
		t.Fatalf("visual mode changed the text, not just its styling:\nplain=%q\nselected=%q",
			ansi.Strip(plain), ansi.Strip(selected))
	}
	if plain == selected {
		t.Fatalf("visual selection is invisible with default styles; view=%q", selected)
	}

	// The default dark background (238) should be what makes the difference.
	if !strings.Contains(selected, "48;5;238") {
		t.Fatalf("expected the default selection background in the view; got %q", selected)
	}
}

// TestVim_VisualSelectionStyleOverride pins that the consumer's style wins
// over the default.
func TestVim_VisualSelectionStyleOverride(t *testing.T) {
	m := vimSetup(t)
	m.Prompt = ""
	m.ShowLineNumbers = false
	m.SetWidth(20)

	st := m.Styles()
	st.Focused.SelectedText = lipgloss.NewStyle().Background(lipgloss.Color("201"))
	m.SetStyles(st)

	m.SetValue("hello world")
	m.SetCursorColumn(0)
	m, _ = m.Update(keyPress('v'))
	m, _ = m.Update(keyPress('l'))

	view := m.View()
	if !strings.Contains(view, "48;5;201") {
		t.Fatalf("consumer selection style not applied; view=%q", view)
	}
	if strings.Contains(view, "48;5;238") {
		t.Fatalf("default selection style leaked through the override; view=%q", view)
	}
}

// TestVim_VisualLineSelectionCoversWholeLines checks that V selects entire
// rows, including the row the cursor started on and every row in between.
func TestVim_VisualLineSelectionCoversWholeLines(t *testing.T) {
	m := vimSetup(t)
	m.Prompt = ""
	m.ShowLineNumbers = false
	m.SetWidth(20)
	m.SetHeight(4)

	st := m.Styles()
	st.Focused.SelectedText = lipgloss.NewStyle().Background(lipgloss.Color("201"))
	m.SetStyles(st)

	m.SetValue("alpha\nbravo\ncharlie")
	m.row = 0
	m.SetCursorColumn(0)
	m, _ = m.Update(keyPress('V'))
	m, _ = m.Update(keyPress('j'))

	lines := strings.Split(m.View(), "\n")
	// Rows 0 and 1 are selected in full; row 2 is not selected at all.
	for i, want := range []bool{true, true, false} {
		if i >= len(lines) {
			t.Fatalf("view has only %d lines: %q", len(lines), m.View())
		}
		got := strings.Contains(lines[i], "48;5;201")
		if got != want {
			t.Errorf("line %d (%q): selected=%v, want %v", i, ansi.Strip(lines[i]), got, want)
		}
	}
}

// TestVim_EscLeavesVisualModes pins the contract consumers rely on when they
// route esc: both visual modes fall back to Normal.
func TestVim_EscLeavesVisualModes(t *testing.T) {
	for _, tc := range []struct {
		key  rune
		mode VimMode
	}{
		{'v', ModeVisualChar},
		{'V', ModeVisualLine},
	} {
		m := vimSetup(t)
		m.SetValue("hello world")
		m.SetCursorColumn(0)

		m, _ = m.Update(keyPress(tc.key))
		if m.VimMode() != tc.mode {
			t.Fatalf("%c entered %v, want %v", tc.key, m.VimMode(), tc.mode)
		}

		m, _ = m.Update(keyEsc())
		if m.VimMode() != ModeNormal {
			t.Errorf("esc from %v left mode at %v, want ModeNormal", tc.mode, m.VimMode())
		}
	}
}

// TestVim_VisualYankFiresOnYank pins the onYank callback contract: a
// visual-mode `y` invokes the registered callback with the selected text
// and linewise flag, then drops to Normal mode. Delete/change operations
// do not fire the callback — they keep classic cut/change semantics.
func TestVim_VisualYankFiresOnYank(t *testing.T) {
	t.Run("visual-char y invokes callback", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello world")
		m.SetCursorColumn(0)
		var gotText string
		var gotLinewise bool
		var calls int
		m.SetOnYank(func(text string, linewise bool) {
			gotText = text
			gotLinewise = linewise
			calls++
		})
		// Select "ello" (chars 1..4 inclusive): v then move to col 4.
		m, _ = m.Update(keyPress('l')) // col 1
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('l')) // col 2
		m, _ = m.Update(keyPress('l')) // col 3
		m, _ = m.Update(keyPress('l')) // col 4
		m, _ = m.Update(keyPress('y'))
		if calls != 1 {
			t.Fatalf("onYank calls: got %d want 1", calls)
		}
		if gotText != "ello" {
			t.Fatalf("onYank text: got %q want %q", gotText, "ello")
		}
		if gotLinewise {
			t.Fatalf("onYank linewise: got true want false (visual-char)")
		}
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode after y: got %v want ModeNormal", m.VimMode())
		}
		// Internal register also populated.
		if m.vim.yankBuf != "ello" {
			t.Fatalf("yankBuf: got %q want %q", m.vim.yankBuf, "ello")
		}
		if m.vim.yankLinewise {
			t.Fatalf("yankLinewise: got true want false")
		}
	})

	t.Run("visual-line y invokes callback linewise", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("alpha\nbravo\ncharlie")
		m.row = 0
		m.SetCursorColumn(0)
		var gotText string
		var gotLinewise bool
		var calls int
		m.SetOnYank(func(text string, linewise bool) {
			gotText = text
			gotLinewise = linewise
			calls++
		})
		m, _ = m.Update(keyPress('V'))
		m, _ = m.Update(keyPress('j')) // extend to row 1
		m, _ = m.Update(keyPress('y'))
		if calls != 1 {
			t.Fatalf("onYank calls: got %d want 1", calls)
		}
		// Linewise visual yank includes the trailing newline (the range
		// extends to the start of the row after the last selected line).
		if gotText != "alpha\nbravo\n" {
			t.Fatalf("onYank text: got %q want %q", gotText, "alpha\nbravo\n")
		}
		if !gotLinewise {
			t.Fatalf("onYank linewise: got false want true (visual-line)")
		}
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode after y: got %v want ModeNormal", m.VimMode())
		}
		if !m.vim.yankLinewise {
			t.Fatalf("yankLinewise: got false want true")
		}
	})

	t.Run("nil callback is a no-op", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		m.SetOnYank(nil)
		m, _ = m.Update(keyPress('v'))
		// Yank without moving: selection is just the char under cursor.
		m, _ = m.Update(keyPress('y'))
		if m.VimMode() != ModeNormal {
			t.Fatalf("mode after y: got %v want ModeNormal", m.VimMode())
		}
		if m.vim.yankBuf != "h" {
			t.Fatalf("yankBuf: got %q want %q", m.vim.yankBuf, "h")
		}
	})

	t.Run("visual d does not fire onYank", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		var calls int
		m.SetOnYank(func(text string, linewise bool) { calls++ })
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('d'))
		if calls != 0 {
			t.Fatalf("onYank calls on visual d: got %d want 0", calls)
		}
		// d still populates the internal register (cut semantics).
		if m.vim.yankBuf != "h" {
			t.Fatalf("yankBuf after d: got %q want %q", m.vim.yankBuf, "h")
		}
		if m.Value() != "ello" {
			t.Fatalf("value after d: got %q want %q", m.Value(), "ello")
		}
	})

	t.Run("visual c does not fire onYank", func(t *testing.T) {
		m := vimSetup(t)
		m.SetValue("hello")
		m.SetCursorColumn(0)
		var calls int
		m.SetOnYank(func(text string, linewise bool) { calls++ })
		m, _ = m.Update(keyPress('v'))
		m, _ = m.Update(keyPress('c'))
		if calls != 0 {
			t.Fatalf("onYank calls on visual c: got %d want 0", calls)
		}
		if m.vim.yankBuf != "h" {
			t.Fatalf("yankBuf after c: got %q want %q", m.vim.yankBuf, "h")
		}
		if m.VimMode() != ModeInsert {
			t.Fatalf("mode after c: got %v want ModeInsert", m.VimMode())
		}
	})
}

// vimKeyMsgs turns a key script into key presses. <Esc>, <CR> and <BS> name
// the special keys; every other rune is typed as itself.
func vimKeyMsgs(script string) []tea.Msg {
	special := map[string]tea.Msg{
		"<Esc>": keyEsc(),
		"<CR>":  keyEnter(),
		"<BS>":  tea.KeyPressMsg{Code: tea.KeyBackspace},
	}
	var msgs []tea.Msg
	for len(script) > 0 {
		matched := false
		for name, msg := range special {
			if strings.HasPrefix(script, name) {
				msgs = append(msgs, msg)
				script = script[len(name):]
				matched = true
				break
			}
		}
		if !matched {
			r := []rune(script)[0]
			msgs = append(msgs, keyPress(r))
			script = script[len(string(r)):]
		}
	}
	return msgs
}

// vimCase is one key script run from a seeded buffer and cursor, checked
// against the buffer, cursor and mode vim leaves.
type vimCase struct {
	name     string
	seed     string
	row, col int
	keys     string
	want     string
	wantRow  int
	wantCol  int
	wantMode VimMode
}

func vimRunCase(t *testing.T, tc vimCase) Model {
	t.Helper()
	m := vimSetup(t)
	m.SetValue(tc.seed)
	m.row = tc.row
	m.SetCursorColumn(tc.col)
	for _, msg := range vimKeyMsgs(tc.keys) {
		m, _ = m.Update(msg)
	}
	return m
}

func vimRunCases(t *testing.T, cases []vimCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := vimRunCase(t, tc)
			if got := m.Value(); got != tc.want {
				t.Fatalf("value: got %q want %q", got, tc.want)
			}
			if m.Line() != tc.wantRow || m.Column() != tc.wantCol {
				t.Fatalf("cursor: got (%d,%d) want (%d,%d)", m.Line(), m.Column(), tc.wantRow, tc.wantCol)
			}
			if m.VimMode() != tc.wantMode {
				t.Fatalf("mode: got %v want %v", m.VimMode(), tc.wantMode)
			}
		})
	}
}

func TestVim_Join(t *testing.T) {
	vimRunCases(t, []vimCase{
		{name: "J inserts one space, cursor on it", seed: "foo\nbar", keys: "J", want: "foo bar", wantCol: 3, wantMode: ModeNormal},
		{name: "J strips leading whitespace", seed: "foo\n    bar", keys: "J", want: "foo bar", wantCol: 3, wantMode: ModeNormal},
		{name: "J keeps trailing whitespace, adds none", seed: "foo \nbar", keys: "J", want: "foo bar", wantCol: 4, wantMode: ModeNormal},
		{name: "J no space before )", seed: "foo(\n  )", keys: "J", want: "foo()", wantCol: 4, wantMode: ModeNormal},
		{name: "J no space after period (joinspaces off)", seed: "end.\nnext", keys: "J", want: "end. next", wantCol: 4, wantMode: ModeNormal},
		{name: "J with empty next line", seed: "foo\n", keys: "J", want: "foo", wantCol: 2, wantMode: ModeNormal},
		{name: "J with whitespace-only next line", seed: "foo\n   ", keys: "J", want: "foo", wantCol: 2, wantMode: ModeNormal},
		{name: "J from empty line adds no space", seed: "\n  bar", keys: "J", want: "bar", wantCol: 0, wantMode: ModeNormal},
		{name: "J from middle of line", seed: "foo\nbar", col: 1, keys: "J", want: "foo bar", wantCol: 3, wantMode: ModeNormal},
		{name: "J on last line does nothing", seed: "foo\nbar", row: 1, col: 1, keys: "J", want: "foo\nbar", wantRow: 1, wantCol: 1, wantMode: ModeNormal},
		{name: "1J joins two lines", seed: "a\nb\nc", keys: "1J", want: "a b\nc", wantCol: 1, wantMode: ModeNormal},
		{name: "2J joins two lines", seed: "a\nb\nc", keys: "2J", want: "a b\nc", wantCol: 1, wantMode: ModeNormal},
		{name: "3J joins three lines", seed: "a\nb\nc\nd", keys: "3J", want: "a b c\nd", wantCol: 3, wantMode: ModeNormal},
		{name: "count past end clamps", seed: "a\nb", keys: "5J", want: "a b", wantCol: 1, wantMode: ModeNormal},
		{name: "count on last line does nothing", seed: "a\nb", row: 1, keys: "3J", want: "a\nb", wantRow: 1, wantMode: ModeNormal},
		{name: "middle rows", seed: "x\na\n  b\ny", row: 1, keys: "J", want: "x\na b\ny", wantRow: 1, wantCol: 1, wantMode: ModeNormal},
		// do_join tracks the end of the previous line's text, which an
		// emptied whitespace-only line resets.
		{name: "3J over blank middle line", seed: "a \n\nb", keys: "3J", want: "a  b", wantCol: 2, wantMode: ModeNormal},
		{name: "J undo is one step", seed: "a\n  b\nc", keys: "3Ju", want: "a\n  b\nc", wantMode: ModeNormal},
		{name: "gJ keeps whitespace", seed: "foo\n    bar", keys: "gJ", want: "foo    bar", wantCol: 3, wantMode: ModeNormal},
		{name: "gJ adds no space", seed: "foo\nbar", keys: "gJ", want: "foobar", wantCol: 3, wantMode: ModeNormal},
		{name: "3gJ", seed: "a\n b\n c\nd", keys: "3gJ", want: "a b c\nd", wantCol: 3, wantMode: ModeNormal},
		{name: "gJ on last line does nothing", seed: "a\nb", row: 1, keys: "gJ", want: "a\nb", wantRow: 1, wantMode: ModeNormal},
		{name: "gJ with empty next line", seed: "ab\n", keys: "gJ", want: "ab", wantCol: 1, wantMode: ModeNormal},
		{name: "visual line J", seed: "a\nb\nc\nd", keys: "VjjJ", want: "a b c\nd", wantCol: 3, wantMode: ModeNormal},
		{name: "visual J upward selection", seed: "a\nb\nc", row: 1, keys: "VkJ", want: "a b\nc", wantCol: 1, wantMode: ModeNormal},
		{name: "visual J single line joins two", seed: "a\nb\nc", keys: "vJ", want: "a b\nc", wantCol: 1, wantMode: ModeNormal},
		{name: "visual J on last line does nothing", seed: "a\nb", row: 1, keys: "VJ", want: "a\nb", wantRow: 1, wantMode: ModeNormal},
		{name: "visual gJ", seed: "a\n  b\nc", keys: "VjgJ", want: "a  b\nc", wantCol: 1, wantMode: ModeNormal},
	})
}

func TestVim_Shift(t *testing.T) {
	vimRunCases(t, []vimCase{
		{name: ">> indents by shiftwidth", seed: "foo", keys: ">>", want: "    foo", wantCol: 4, wantMode: ModeNormal},
		{name: ">> twice", seed: "foo", keys: ">>>>", want: "        foo", wantCol: 8, wantMode: ModeNormal},
		{name: "<< removes shiftwidth", seed: "      foo", keys: "<<", want: "  foo", wantCol: 2, wantMode: ModeNormal},
		{name: "<< floors at zero", seed: "  foo", keys: "<<", want: "foo", wantCol: 0, wantMode: ModeNormal},
		{name: "<< never removes text", seed: "foo", col: 2, keys: "<<", want: "foo", wantCol: 0, wantMode: ModeNormal},
		{name: ">> cursor to first non-blank", seed: "  foo bar", col: 7, keys: ">>", want: "      foo bar", wantCol: 6, wantMode: ModeNormal},
		{name: "3>> shifts three lines", seed: "a\nb\nc\nd", keys: "3>>", want: "    a\n    b\n    c\nd", wantCol: 4, wantMode: ModeNormal},
		{name: ">3> shifts three lines", seed: "a\nb\nc\nd", keys: ">3>", want: "    a\n    b\n    c\nd", wantCol: 4, wantMode: ModeNormal},
		{name: "3<< unindents three lines", seed: "    a\n  b\nc\n    d", keys: "3<<", want: "a\nb\nc\n    d", wantMode: ModeNormal},
		{name: "count past end clamps", seed: "a\nb\nc", row: 1, keys: "5>>", want: "a\n    b\n    c", wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "count on last line does nothing", seed: "a\nb", row: 1, keys: "2>>", want: "a\nb", wantRow: 1, wantMode: ModeNormal},
		{name: ">> on last line", seed: "a\nb", row: 1, keys: ">>", want: "a\n    b", wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "empty lines stay empty", seed: "a\n\nb", keys: "3>>", want: "    a\n\n    b", wantCol: 4, wantMode: ModeNormal},
		{name: ">> on empty line", seed: "", keys: ">>", want: "", wantMode: ModeNormal},
		{name: ">> on whitespace-only line", seed: "  ", keys: ">>", want: "      ", wantCol: 5, wantMode: ModeNormal},
		{name: "<< on whitespace-only line", seed: "      ", keys: "<<", want: "  ", wantCol: 1, wantMode: ModeNormal},
		{name: ">j shifts two lines", seed: "a\nb\nc", keys: ">j", want: "    a\n    b\nc", wantCol: 4, wantMode: ModeNormal},
		{name: "<k from below", seed: "    a\n    b", row: 1, keys: "<k", want: "a\nb", wantMode: ModeNormal},
		{name: ">G", seed: "a\nb\nc", row: 1, keys: ">G", want: "a\n    b\n    c", wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: ">ip", seed: "a\nb\n\nc", keys: ">ip", want: "    a\n    b\n\nc", wantCol: 4, wantMode: ModeNormal},
		{name: ">w shifts current line", seed: "a b\nc", keys: ">w", want: "    a b\nc", wantCol: 4, wantMode: ModeNormal},
		{name: "Esc cancels pending >", seed: "a", keys: "><Esc>>", want: "a", wantMode: ModeNormal},
		{name: "3>> undo is one step", seed: "a\nb\nc", keys: "3>>u", want: "a\nb\nc", wantMode: ModeNormal},
		{name: "visual > shifts selection", seed: "a\nb\nc", keys: "Vj>", want: "    a\n    b\nc", wantCol: 4, wantMode: ModeNormal},
		{name: "visual count is the amount", seed: "a\nb\nc", keys: "Vj3>", want: "            a\n            b\nc", wantCol: 12, wantMode: ModeNormal},
		{name: "visual < shifts selection", seed: "a\n        b\n    c", row: 2, keys: "vk<", want: "a\n    b\nc", wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "visual 2< ", seed: "        a\n  b", keys: "Vj2<", want: "a\nb", wantMode: ModeNormal},
		{name: "charwise visual shifts whole lines", seed: "abc\ndef", col: 1, keys: "vj>", want: "    abc\n    def", wantCol: 4, wantMode: ModeNormal},
		{name: "visual > undo", seed: "a\nb", keys: "Vj>u", want: "a\nb", wantMode: ModeNormal},
	})
}

func TestVim_Search(t *testing.T) {
	const buf = "foo bar\nbaz foo\nfoo"
	vimRunCases(t, []vimCase{
		{name: "/ finds next match", seed: buf, keys: "/foo<CR>", want: buf, wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "/ skips match under cursor", seed: buf, row: 1, col: 4, keys: "/foo<CR>", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "n repeats forward", seed: buf, keys: "/foo<CR>n", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "n wraps at bottom", seed: buf, keys: "/foo<CR>nn", want: buf, wantMode: ModeNormal},
		{name: "N reverses", seed: buf, keys: "/foo<CR>N", want: buf, wantMode: ModeNormal},
		{name: "N wraps at top", seed: buf, keys: "/foo<CR>NN", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "? searches backward with wrap", seed: buf, keys: "?foo<CR>", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "n after ? continues backward", seed: buf, keys: "?foo<CR>n", want: buf, wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "N after ? goes forward", seed: buf, keys: "?foo<CR>N", want: buf, wantMode: ModeNormal},
		{name: "count on /", seed: buf, keys: "2/foo<CR>", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "count on n wraps", seed: buf, keys: "/foo<CR>3n", want: buf, wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "count on N", seed: buf, row: 2, keys: "/foo<CR>2N", want: buf, wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "sole match wraps onto itself", seed: "abc", col: 1, keys: "/b<CR>", want: "abc", wantCol: 1, wantMode: ModeNormal},
		{name: "no match leaves cursor", seed: buf, keys: "/qqq<CR>", want: buf, wantMode: ModeNormal},
		{name: "Esc cancels the prompt", seed: buf, keys: "/foo<Esc>n", want: buf, wantMode: ModeNormal},
		{name: "backspace edits the prompt", seed: buf, keys: "/fox<BS>o<CR>", want: buf, wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "backspace on empty prompt cancels", seed: buf, keys: "/<BS>j", want: buf, wantRow: 1, wantMode: ModeNormal},
		{name: "empty pattern reuses last", seed: buf, keys: "/foo<CR>/<CR>", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "empty ? reuses last backward", seed: buf, keys: "/foo<CR>?<CR>", want: buf, wantMode: ModeNormal},
		{name: "prompt swallows normal-mode keys", seed: buf, keys: "/dd<Esc>", want: buf, wantMode: ModeNormal},
		{name: "case sensitive by default", seed: "Foo foo", keys: "/Foo<CR>", want: "Foo foo", wantMode: ModeNormal},
		{name: "\\c ignores case", seed: "x Foo", keys: "/foo\\c<CR>", want: "x Foo", wantCol: 2, wantMode: ModeNormal},
		{name: ". matches any char", seed: "foo bar", keys: "/b.r<CR>", want: "foo bar", wantCol: 4, wantMode: ModeNormal},
		{name: "[] collection", seed: "a1 b2", keys: "/b[0-9]<CR>", want: "a1 b2", wantCol: 3, wantMode: ModeNormal},
		{name: "( is literal", seed: "f(x)", keys: "/(<CR>", want: "f(x)", wantCol: 1, wantMode: ModeNormal},
		{name: "+ is literal", seed: "a+b aab", keys: "/a+<CR>", want: "a+b aab", wantMode: ModeNormal},
		{name: "\\+ repeats", seed: "xb aab", keys: "/a\\+b<CR>", want: "xb aab", wantCol: 3, wantMode: ModeNormal},
		{name: "\\| alternates", seed: "x yz", keys: "/z\\|y<CR>", want: "x yz", wantCol: 2, wantMode: ModeNormal},
		{name: "\\< word start", seed: "abar bar", keys: "/\\<bar<CR>", want: "abar bar", wantCol: 5, wantMode: ModeNormal},
		{name: "^ anchors line start", seed: buf, keys: "/^foo<CR>", want: buf, wantRow: 2, wantMode: ModeNormal},
		{name: "^ mid-pattern is literal", seed: "a^b ab", col: 3, keys: "/a^b<CR>", want: "a^b ab", wantMode: ModeNormal},
		{name: "$ anchors line end", seed: buf, keys: "/foo$<CR>", want: buf, wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "$ alone rests on last char", seed: "ab\ncd", keys: "/$<CR>n", want: "ab\ncd", wantRow: 1, wantCol: 1, wantMode: ModeNormal},
		{name: "\\. is literal", seed: "axb a.b", keys: "/a\\.b<CR>", want: "axb a.b", wantCol: 4, wantMode: ModeNormal},
		{name: "\\{n} counts", seed: "ab aab", keys: "/a\\{2}b<CR>", want: "ab aab", wantCol: 3, wantMode: ModeNormal},
		{name: "invalid pattern leaves cursor", seed: "abc", keys: "/[c-a]<CR>", want: "abc", wantMode: ModeNormal},
		{name: "multibyte columns are runes", seed: "héllo wörld", keys: "/w<CR>", want: "héllo wörld", wantCol: 6, wantMode: ModeNormal},
		{name: "search extends visual selection", seed: "foo bar", keys: "v/bar<CR>d", want: "ar", wantMode: ModeNormal},
		{name: "search in visual keeps visual", seed: "foo bar", keys: "v/bar<CR>", want: "foo bar", wantCol: 4, wantMode: ModeVisualChar},
		{name: "* whole word", seed: "foo foobar\nfoo", keys: "*", want: "foo foobar\nfoo", wantRow: 1, wantMode: ModeNormal},
		{name: "# whole word backward", seed: "foo foobar\nfoo", row: 1, keys: "#", want: "foo foobar\nfoo", wantMode: ModeNormal},
		{name: "* from middle of word", seed: "foo foo", col: 1, keys: "*", want: "foo foo", wantCol: 4, wantMode: ModeNormal},
		{name: "# from middle of word skips it", seed: "foo foo", col: 5, keys: "#", want: "foo foo", wantMode: ModeNormal},
		{name: "* uses keyword after cursor", seed: "  foo x foo", keys: "*", want: "  foo x foo", wantCol: 8, wantMode: ModeNormal},
		{name: "* on punctuation picks next keyword", seed: "a.b b", col: 1, keys: "*", want: "a.b b", wantCol: 4, wantMode: ModeNormal},
		{name: "* without keyword uses non-blank run", seed: "a ->\nx->", col: 2, keys: "*", want: "a ->\nx->", wantRow: 1, wantCol: 1, wantMode: ModeNormal},
		{name: "* sole occurrence wraps to its start", seed: "foo bar", col: 5, keys: "*", want: "foo bar", wantCol: 4, wantMode: ModeNormal},
		{name: "2*", seed: "foo foo foo", keys: "2*", want: "foo foo foo", wantCol: 8, wantMode: ModeNormal},
		{name: "n after *", seed: "foo foo foo", keys: "*n", want: "foo foo foo", wantCol: 8, wantMode: ModeNormal},
		{name: "N after #", seed: "foo foo foo", col: 8, keys: "#N", want: "foo foo foo", wantCol: 8, wantMode: ModeNormal},
		{name: "* on punctuation run is literal", seed: "a .*\naaa .*", col: 2, keys: "*", want: "a .*\naaa .*", wantRow: 1, wantCol: 4, wantMode: ModeNormal},
		{name: "* on punctuation searches the next keyword whole", seed: "a.b axb a.b", col: 1, keys: "*", want: "a.b axb a.b", wantCol: 10, wantMode: ModeNormal},
		{name: "visual J undo returns to selection start", seed: "a\nb\nc", keys: "VjJu", want: "a\nb\nc", wantMode: ModeNormal},
		{name: "* on unicode word", seed: "café cafés café", keys: "*", want: "café cafés café", wantCol: 11, wantMode: ModeNormal},
		{name: "* on blank line does nothing", seed: "\nfoo", keys: "*", want: "\nfoo", wantMode: ModeNormal},
		{name: "n without search does nothing", seed: buf, keys: "n", want: buf, wantMode: ModeNormal},
	})
}

func TestVim_SearchPrompt(t *testing.T) {
	m := vimSetup(t)
	m.SetValue("foo")
	if _, open := m.VimSearchPrompt(); open {
		t.Fatalf("prompt open before /")
	}
	for _, msg := range vimKeyMsgs("/fo") {
		m, _ = m.Update(msg)
	}
	if got, open := m.VimSearchPrompt(); !open || got != "/fo" {
		t.Fatalf("prompt: got %q open=%v", got, open)
	}
	m, _ = m.Update(tea.PasteMsg{Content: "o\n"})
	if got, _ := m.VimSearchPrompt(); got != "/foo" {
		t.Fatalf("paste into prompt: got %q", got)
	}
	if m.Value() != "foo" {
		t.Fatalf("paste reached the buffer: %q", m.Value())
	}
	m, _ = m.Update(keyEnter())
	if _, open := m.VimSearchPrompt(); open {
		t.Fatalf("prompt open after Enter")
	}
	m, _ = m.Update(keyPress('?'))
	if got, open := m.VimSearchPrompt(); !open || got != "?" {
		t.Fatalf("? prompt: got %q open=%v", got, open)
	}
	m.SetVimMode(ModeNormal)
	if _, open := m.VimSearchPrompt(); open {
		t.Fatalf("SetVimMode left the prompt open")
	}
}

func TestVim_ReplaceMode(t *testing.T) {
	vimRunCases(t, []vimCase{
		{name: "R overtypes", seed: "abcd", keys: "Rxy<Esc>", want: "xycd", wantCol: 1, wantMode: ModeNormal},
		{name: "R appends past end of line", seed: "ab", keys: "Rwxyz<Esc>", want: "wxyz", wantCol: 3, wantMode: ModeNormal},
		{name: "R stays on its line", seed: "ab\ncd", keys: "Rwxyz<Esc>", want: "wxyz\ncd", wantCol: 3, wantMode: ModeNormal},
		{name: "R stays in Replace until Esc", seed: "abcd", keys: "Rx", want: "xbcd", wantCol: 1, wantMode: ModeReplace},
		{name: "backspace restores replaced char", seed: "abcd", keys: "Rxy<BS><Esc>", want: "xbcd", wantMode: ModeNormal},
		{name: "backspace removes appended char", seed: "ab", keys: "Rxyz<BS><BS><Esc>", want: "xb", wantMode: ModeNormal},
		{name: "backspace restores all", seed: "abcd", col: 1, keys: "Rxyz<BS><BS><BS>", want: "abcd", wantCol: 1, wantMode: ModeReplace},
		{name: "backspace before start only moves", seed: "abcd", col: 2, keys: "R<BS><BS>x<Esc>", want: "xbcd", wantMode: ModeNormal},
		{name: "backspace at line start stays", seed: "abcd", keys: "R<BS><Esc>", want: "abcd", wantMode: ModeNormal},
		{name: "Esc at column 0 stays", seed: "abcd", keys: "R<Esc>", want: "abcd", wantMode: ModeNormal},
		{name: "count repeats typed text", seed: "12345678", keys: "3Rab<Esc>", want: "ababab78", wantCol: 5, wantMode: ModeNormal},
		{name: "count repeat appends past end", seed: "12", keys: "2Rab<Esc>", want: "abab", wantCol: 3, wantMode: ModeNormal},
		{name: "count repeats net text after backspace", seed: "123456", keys: "2Rabc<BS><Esc>", want: "abab56", wantCol: 3, wantMode: ModeNormal},
		{name: "Enter breaks the line, deletes nothing", seed: "abcd", col: 1, keys: "Rx<CR>y<Esc>", want: "ax\nyd", wantRow: 1, wantMode: ModeNormal},
		{name: "backspace joins a typed line break", seed: "abcd", col: 1, keys: "Rx<CR><BS><Esc>", want: "axcd", wantCol: 1, wantMode: ModeNormal},
		{name: "R on empty line appends", seed: "", keys: "Rhi<Esc>", want: "hi", wantCol: 1, wantMode: ModeNormal},
		{name: "undo reverts the whole R", seed: "abcd", keys: "3Rxy<Esc>u", want: "abcd", wantMode: ModeNormal},
		{name: "r replaces one char in Normal", seed: "abc", keys: "rx", want: "xbc", wantMode: ModeNormal},
		{name: "r then Esc cancels", seed: "abc", keys: "r<Esc>", want: "abc", wantMode: ModeNormal},
	})
}

func TestVim_ReplaceModeStateAndShape(t *testing.T) {
	m := vimSetup(t)
	m.SetValue("abcd")
	m.SetCursorColumn(0)
	m, _ = m.Update(keyPress('r'))
	if m.VimMode() != ModeNormal {
		t.Fatalf("pending r: mode %v, want Normal", m.VimMode())
	}
	m, _ = m.Update(keyEsc())
	m, _ = m.Update(keyPress('R'))
	if m.VimMode() != ModeReplace {
		t.Fatalf("R: mode %v, want Replace", m.VimMode())
	}
	if got := m.Styles().Cursor.Shape; got != tea.CursorUnderline {
		t.Fatalf("R shape: got %v want Underline", got)
	}
	m, _ = m.Update(tea.PasteMsg{Content: "xyz"})
	if m.Value() != "xyzd" {
		t.Fatalf("paste in Replace: got %q want %q", m.Value(), "xyzd")
	}
	m, _ = m.Update(keyEsc())
	if got := m.Styles().Cursor.Shape; got != tea.CursorBlock {
		t.Fatalf("after Esc shape: got %v want Block", got)
	}
}

func TestVim_Pending(t *testing.T) {
	for _, keys := range []string{"3", "d", ">", "g", "f", "r", "di", "/", "/ab"} {
		t.Run(keys, func(t *testing.T) {
			m := vimRunCase(t, vimCase{seed: "abc", keys: keys})
			if !m.VimPending() {
				t.Fatalf("%q: VimPending false", keys)
			}
			m, _ = m.Update(keyEsc())
			if m.VimPending() {
				t.Fatalf("%q: Esc left VimPending true", keys)
			}
			if m.VimMode() != ModeNormal {
				t.Fatalf("%q: mode %v after Esc", keys, m.VimMode())
			}
		})
	}
	for _, keys := range []string{"", "l", "dd", "/a<CR>", "rx"} {
		m := vimRunCase(t, vimCase{seed: "abc", keys: keys})
		if m.VimPending() {
			t.Fatalf("%q: VimPending true", keys)
		}
	}
}
