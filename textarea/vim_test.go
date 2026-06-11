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
