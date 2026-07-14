package key

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Binding describes a set of keybindings and, optionally, their associated
// help text.
type Binding struct {
	keys     []string
	help     Help
	disabled bool
}

// BindingOpt is an initialization option for a keybinding. It's used as an
// argument to NewBinding.
type BindingOpt func(*Binding)

// NewBinding returns a new keybinding from a set of BindingOpt options.
func NewBinding(opts ...BindingOpt) Binding {
	b := &Binding{}
	for _, opt := range opts {
		opt(b)
	}
	return *b
}

// WithKeys initializes a keybinding with the given keystrokes.
func WithKeys(keys ...string) BindingOpt {
	return func(b *Binding) {
		b.keys = keys
	}
}

// WithHelp initializes a keybinding with the given help text.
func WithHelp(key, desc string) BindingOpt {
	return func(b *Binding) {
		b.help = Help{Key: key, Desc: desc}
	}
}

// WithDisabled initializes a disabled keybinding.
func WithDisabled() BindingOpt {
	return func(b *Binding) {
		b.disabled = true
	}
}

// SetKeys sets the keys for the keybinding.
func (b *Binding) SetKeys(keys ...string) {
	b.keys = keys
}

// Keys returns the keys for the keybinding.
func (b Binding) Keys() []string {
	return b.keys
}

// SetHelp sets the help text for the keybinding.
func (b *Binding) SetHelp(key, desc string) {
	b.help = Help{Key: key, Desc: desc}
}

// Help returns the Help information for the keybinding.
func (b Binding) Help() Help {
	return b.help
}

// Enabled returns whether or not the keybinding is enabled. Disabled
// keybindings won't be activated and won't show up in help. Keybindings are
// enabled by default.
func (b Binding) Enabled() bool {
	return !b.disabled && b.keys != nil
}

// SetEnabled enables or disables the keybinding.
func (b *Binding) SetEnabled(v bool) {
	b.disabled = !v
}

// Unbind removes the keys and help from this binding, effectively nullifying
// it. This is a step beyond disabling it, since applications can enable
// or disable key bindings based on application state.
func (b *Binding) Unbind() {
	b.keys = nil
	b.help = Help{}
}

// Help is help information for a given keybinding.
type Help struct {
	Key  string
	Desc string
}

// Matches checks if the given key matches the given bindings.
//
// The comparison tries the string representation first (fast path, preserves
// existing behaviour for special keys like "enter" and printable chars like
// "a"). When that fails and the key is a tea.KeyPressMsg, it falls back to
// structural matching on Mod+Code. The fallback catches modified printable
// keys where String() returns the bare Text — e.g. a terminal delivering
// ctrl+comma as {Code:',', Mod:ModCtrl, Text:","} makes String() return ",",
// which never equals "ctrl+,". Structural matching compares Mod+Code directly
// and matches.
func Matches[Key fmt.Stringer](k Key, b ...Binding) bool {
	keys := k.String()
	var teaKey tea.Key
	if tk, ok := any(k).(tea.KeyPressMsg); ok {
		teaKey = tk.Key()
	}
	for _, binding := range b {
		if !binding.Enabled() {
			continue
		}
		for _, v := range binding.keys {
			if keys == v {
				return true
			}
		}
		if teaKey.Code != 0 || teaKey.Mod != 0 || teaKey.Text != "" {
			for _, v := range binding.keys {
				if keyMatch(teaKey, v) {
					return true
				}
			}
		}
	}
	return false
}

// keyMatch mirrors ultraviolet's keyMatchString: parse the binding into
// modifier bits + a code/text, then compare Mod (masked) and Code, falling
// back to Text for printable characters.
func keyMatch(k tea.Key, s string) bool {
	var (
		mod  tea.KeyMod
		code rune
		text string
	)
	parts := strings.Split(s, "+")
	for _, part := range parts {
		switch part {
		case "ctrl":
			mod |= tea.ModCtrl
		case "alt":
			mod |= tea.ModAlt
		case "shift":
			mod |= tea.ModShift
		case "meta":
			mod |= tea.ModMeta
		case "hyper":
			mod |= tea.ModHyper
		case "super":
			mod |= tea.ModSuper
		case "capslock":
			mod |= tea.ModCapsLock
		case "scrolllock":
			mod |= tea.ModScrollLock
		case "numlock":
			mod |= tea.ModNumLock
		default:
			if utf8.RuneCountInString(part) == 1 {
				code, _ = utf8.DecodeRuneInString(part)
			} else {
				code = 0
				text = part
			}
		}
	}

	// Mask off shift/caps when a printable character is expected, so
	// shift+a (Text="A", Mod=ModShift) still matches a binding for "A".
	smod := mod &^ (tea.ModShift | tea.ModCapsLock)
	if smod == 0 && text == "" && unicode.IsPrint(code) {
		if mod&tea.ModShift != 0 || mod&tea.ModCapsLock != 0 {
			return k.Text == string(unicode.ToUpper(code))
		}
		return k.Text == string(code)
	}

	// Otherwise compare Mod and Code, with a Text fallback for multi-rune
	// keys.
	return (k.Mod == mod && k.Code == code) ||
		(k.Text != "" && k.Text == text)
}
