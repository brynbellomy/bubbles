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
