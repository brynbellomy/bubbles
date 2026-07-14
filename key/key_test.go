package key

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestMatches_SpecialKeyStringPath(t *testing.T) {
	// Enter arrives as a special key with empty Text; String() returns
	// "enter" via Keystroke(). The fast path must still catch it.
	binding := NewBinding(WithKeys("enter"))
	msg := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	require.True(t, Matches(msg, binding), "enter must match via string path")
}

func TestMatches_BarePrintableStringPath(t *testing.T) {
	// Bare "a" arrives with Text="a"; String() returns "a".
	binding := NewBinding(WithKeys("a"))
	msg := tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})
	require.True(t, Matches(msg, binding), "bare printable must match via string path")
}

func TestMatches_CtrlPrintableStructuralFallback(t *testing.T) {
	// ctrl+comma under legacy/modifyOtherKeys-off terminals: the byte 0x2C
	// arrives with Code=',', Mod=ModCtrl, Text=",". String() returns the
	// Text (","), which never equals "ctrl+," — only structural matching
	// on Mod+Code catches it.
	binding := NewBinding(WithKeys("ctrl+,"))
	msg := tea.KeyPressMsg(tea.Key{Code: ',', Mod: tea.ModCtrl, Text: ","})
	require.True(t, Matches(msg, binding), "ctrl+comma must match via structural fallback")
}

func TestMatches_CtrlPrintableKittyProtocol(t *testing.T) {
	// Under the kitty keyboard protocol, ctrl+comma arrives with empty
	// Text; String() returns "ctrl+," via Keystroke(). The string path
	// catches it.
	binding := NewBinding(WithKeys("ctrl+,"))
	msg := tea.KeyPressMsg(tea.Key{Code: ',', Mod: tea.ModCtrl})
	require.True(t, Matches(msg, binding), "ctrl+comma must match via string path under kitty protocol")
}

func TestMatches_ShiftedLetterStructuralFallback(t *testing.T) {
	// shift+a in legacy mode arrives as Text="A", Code='A', Mod=0 (some
	// terminals don't set ModShift for shifted letters). String() returns
	// "A". A binding for "A" must match.
	binding := NewBinding(WithKeys("A"))
	msg := tea.KeyPressMsg(tea.Key{Code: 'A', Text: "A"})
	require.True(t, Matches(msg, binding), "shifted letter must match")
}

func TestMatches_NoMatchWhenDisabled(t *testing.T) {
	binding := NewBinding(WithKeys("ctrl+,"))
	binding.SetEnabled(false)
	msg := tea.KeyPressMsg(tea.Key{Code: ',', Mod: tea.ModCtrl, Text: ","})
	require.False(t, Matches(msg, binding), "disabled binding must not match")
}

func TestMatches_NoMatchForDifferentKey(t *testing.T) {
	binding := NewBinding(WithKeys("ctrl+,"))
	msg := tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})
	require.False(t, Matches(msg, binding), "unrelated key must not match")
}
