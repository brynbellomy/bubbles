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
