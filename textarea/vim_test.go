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
