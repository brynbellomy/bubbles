# Textarea Vim Keybindings — Design

**Date:** 2026-06-10
**Status:** Approved
**Component:** `charm.land/bubbles/v2/textarea`

## Summary

Add opt-in modal vim keybindings to the `textarea` component. Default behavior is unchanged. When enabled, the textarea behaves like a "useful subset" of vim: Normal / Insert / Visual / Replace modes, motions, operators-over-motions, text objects, inline find, counts, undo/redo, and a single unnamed yank register. Marks, macros, named registers, search (`/`?), and block visual are explicitly out of scope.

## Goals

1. Provide modal vim keybindings that feel like vim to a daily vim user — at minimum, every common motion + operator combination in muscle memory works.
2. Zero behavior change for existing consumers. Vim mode is strictly opt-in.
3. No new external dependencies.
4. Keep the implementation self-contained within the `textarea` package.

## Non-Goals

- Named registers, marks, macros, ex commands, search (`/?`), block visual (`Ctrl-v`).
- Rebindable vim keys in v1. We hardcode vim defaults; a `VimKeyMap` struct can come later without breaking changes.
- Multi-line bracket text objects (e.g., `i(` spanning lines). v1 stays within the current logical line for bracket objects.

## Public API

New methods on `Model`:

```go
func (m *Model) SetVimEnabled(enabled bool)
func (m Model) VimEnabled() bool
func (m Model) VimMode() VimMode
func (m *Model) SetVimMode(mode VimMode)
```

Mode type:

```go
type VimMode int

const (
    ModeInsert VimMode = iota
    ModeNormal
    ModeVisualChar
    ModeVisualLine
    ModeReplace // single-shot, for r{char}
)
```

Behavior:

- `VimEnabled() == false` by default. Existing consumers see byte-identical behavior.
- `SetVimEnabled(true)` puts the model in `ModeNormal` immediately, swaps the cursor to Block, and snapshots the current state into the undo stack (so first edit is reversible).
- `SetVimEnabled(false)` restores `ModeInsert`, restores the user's original cursor shape, and clears all vim state (selection, pending operator, undo stack).
- `SetVimMode(mode)` forces an immediate mode transition. Used by callers wanting programmatic control (e.g., a host app that wants to drop into Normal on focus). Clears `pendingOp`, `pendingCount`, and any pending find prompt. No effect if `VimEnabled() == false`.
- The existing `KeyMap` continues to drive Insert-mode behavior. Vim keys are hardcoded in `vim.go`.

## Internal State

A `vimState` struct held on `Model`:

```go
type vimState struct {
    mode             VimMode
    pendingOp        rune    // 0, 'd', 'c', 'y', 'g', 'r', 'f', 'F', 't', 'T'
    pendingCount     int     // 0 means "no count typed"
    lastFind         vimFind // for ; and ,
    selStartRow      int
    selStartCol      int
    yankBuf          string
    yankLinewise     bool
    savedCursorShape tea.CursorShape // restored on disable
}

type vimFind struct {
    kind rune // 'f', 'F', 't', 'T'
    ch   rune
}
```

## Mode Transitions

- **Insert → Normal:** `Esc` or `Ctrl-[`. Cursor moves left by 1 if `col > 0` (vim convention). Snapshots undo (one undo step per insert session).
- **Normal → Insert:** `i` (at cursor), `I` (first non-blank), `a` (after cursor), `A` (end of line), `o` (new line below), `O` (new line above), `s` (delete char, then insert). Each entry snapshots undo if it mutated state.
- **Normal → Visual:** `v` (charwise) anchors at cursor. `V` (linewise) anchors at start of cursor row.
- **Visual → Normal:** `Esc`, or after an operator consumes the selection.
- **Normal → Replace:** `r` makes the next typed char overwrite the char under cursor, then returns to Normal.
- **Any non-Insert → Normal:** `Esc` always clears `pendingOp`, `pendingCount`, and any pending find prompt.

### Operator-pending example

`3dw`:

1. `3` → `pendingCount = 3`.
2. `d` → `pendingOp = 'd'`.
3. `w` → motion runs 3 times; range `[startCol .. endCol]` gets deleted, yanked to `yankBuf` (charwise), state cleared, mode stays Normal.

### Multi-key sequences

- `gg`, `gu{motion}`, `gU{motion}` use `pendingOp = 'g'` (it's treated as a pseudo-operator for sequencing purposes).
- `f{c}`, `F{c}`, `t{c}`, `T{c}` use `pendingOp` set to the find kind; the next character is consumed as the target.
- `r{c}` uses `pendingOp = 'r'`; the next character overwrites.

## Keymap (Tier 2 Surface)

### Normal mode — motions

| Key             | Action                                                |
| --------------- | ----------------------------------------------------- |
| `h j k l`       | char left/down/up/right (no line wrap on `h`/`l`)     |
| `w` / `W`       | word forward (word = alnum+`_`; WORD = whitespace)    |
| `b` / `B`       | word backward                                         |
| `e` / `E`       | end of word forward                                   |
| `0`             | line start                                            |
| `^`             | first non-blank on line                               |
| `$`             | line end                                              |
| `gg`            | first line                                            |
| `G`             | last line; `{n}G` goes to line `n`                    |
| `f{c}` / `F{c}` | inline find next/prev occurrence on current line      |
| `t{c}` / `T{c}` | inline till — like find but cursor stops one short    |
| `;` / `,`       | repeat last find / reverse                            |

### Normal mode — edits

| Key                                 | Action                                                              |
| ----------------------------------- | ------------------------------------------------------------------- |
| `x`                                 | delete char under cursor (yanked charwise)                          |
| `X`                                 | delete char before cursor                                           |
| `r{c}`                              | replace char under cursor with `c`                                  |
| `~`                                 | toggle case under cursor, move right                                |
| `dd` / `cc` / `yy`                  | linewise op on current line (yanked linewise)                       |
| `D` / `C` / `Y`                     | op from cursor to end of line                                       |
| `d{motion}` / `c{motion}` / `y{motion}` | op over motion (charwise yank, except linewise motions)         |
| `p` / `P`                           | paste after / before (linewise → new line; charwise → inline)       |
| `o` / `O`                           | open line below / above + enter Insert                              |
| `i` / `I` / `a` / `A` / `s`         | enter Insert at various positions                                   |
| `v` / `V`                           | enter Visual char / line                                            |
| `u`                                 | undo                                                                |
| `Ctrl-r`                            | redo                                                                |
| `gu{motion}` / `gU{motion}`         | lowercase / uppercase over motion                                   |

### Text objects (after `d`/`c`/`y` or in Visual)

`iw aw i" a" i' a' i\` a\` i( a( i) a) i{ a{ i} a} i[ a[ i] a] ip ap`

- `iw` / `aw`: inner / a word.
- `i"` / `a"` / `i'` / `a'` / `` i` `` / `` a` ``: quoted strings on the current line. `i` excludes the quotes; `a` includes them.
- `i(`, `a(`, `i)`, `a)`, `i{`, `a{`, `i}`, `a}`, `i[`, `a[`, `i]`, `a]`: bracket pairs **on the current logical line only** in v1.
- `ip` / `ap`: paragraph = run of non-blank lines, blank-line-delimited. `ap` includes the trailing blank line.

### Visual mode

- All Normal-mode motions extend the selection from the anchor.
- `d` / `c` / `y` consume the selection. `c` and `d` delete the selection and (for `c`) enter Insert. `y` copies and exits to Normal.
- `o` swaps anchor and cursor.
- `~` toggles case on the selection.
- `Esc` cancels the selection.

### Insert mode

- Unchanged from current textarea behavior.
- Adds `Esc` and `Ctrl-[` → Normal.

## Counts

- Digits `1`-`9` start a count in Normal/Visual. `0` is a motion when no count is in progress, and a digit afterwards (`10`).
- Counts apply to motions (`3w`), operators (`3dw` = `d3w`), and edits (`5x`, `10dd`).
- Counts cap at 9999 to prevent runaway loops.
- `pendingCount = 0` means "no count typed"; count of `0` is impossible because of the digit rule above.

## Cursor Placement After Edits

Cursor placement after every edit operation matches vim defaults. Explicitly:

- `yy` and `y{motion}`: cursor stays where it was when the yank started.
- `p` (paste after): linewise → first non-blank of pasted line(s); charwise → last char of pasted text.
- `P` (paste before): same rules but pasted before cursor / current line.
- `dd`: cursor on first non-blank of the line that takes the deleted line's row, or the previous line if the last line was deleted.
- `dw`, `cw`, etc.: cursor at the start of the affected range.
- `x`, `X`: cursor on the char that takes the deleted slot, clamped to end of line.
- `o` / `O`: cursor at column 0 of the newly opened line.
- `s`: cursor at the position of the deleted char (insert mode).
- Undo/redo: cursor restored from the snapshot.

When a behavior is ambiguous or under-tested, default to nvim's behavior on the same input.

## Selection & Rendering

- Add a new `SelectedText lipgloss.Style` field to `StyleState`.
- When `mode == ModeVisualChar || ModeVisualLine`, compute the normalized selection range `(minRow, minCol) .. (maxRow, maxCol)` from `selStartRow/Col` and the current `row/col`.
- Inside the existing per-wrapped-line render loop in `view()`, slice each rendered run into `pre / sel / post` based on the selection range and render `sel` through `SelectedText`.
- Linewise visual selects whole rows including trailing space padding.
- Selection rendering composes with cursor-line styling and soft wrapping.

## Undo / Redo

```go
type undoSnapshot struct {
    value    [][]rune // deep copy
    row, col int
}

type undoStack struct {
    snapshots []undoSnapshot
    capacity  int // default 200
    head      int // index of current state in snapshots
}
```

Snapshot triggers:

- On entering Normal from Insert — captures the whole insert session as one undo step (matches vim).
- Before each Normal-mode edit operation (`x`, `X`, `dd`, `dw`, the start of `cw`, `p`, `P`, `~`, `r`, case ops, etc.).
- Before `SetVimEnabled(true)` returns — initial state is a valid undo target.

Semantics:

- `u` decrements `head` and restores that snapshot. No-op at `head == 0`.
- `Ctrl-r` increments `head` and restores. No-op at the top.
- Any edit after an undo truncates `snapshots[head+1:]` (standard redo behavior).
- Bounded ring at 200 snapshots. Oldest dropped when full.

## Cursor Shapes per Mode

| Mode             | Shape               |
| ---------------- | ------------------- |
| `ModeInsert`     | `tea.CursorBar`     |
| `ModeNormal`     | `tea.CursorBlock`   |
| `ModeVisualChar` | `tea.CursorBlock`   |
| `ModeVisualLine` | `tea.CursorBlock`   |
| `ModeReplace`    | `tea.CursorUnderline` |

When vim is enabled, mode changes override `m.styles.Cursor.Shape`. The user's pre-vim shape is stashed in `vimSavedCursorShape` and restored on disable.

## Dispatch Integration

Inside `Update()`, before the existing key switch:

```go
if m.vimEnabled && m.vim.mode != ModeInsert {
    if msg, ok := msg.(tea.KeyPressMsg); ok {
        handled, cmd := m.vimUpdate(msg)
        if handled {
            // housekeeping: recalculateHeight, view refresh, repositionView, cursor blink
            return m, cmd
        }
    }
}
// fall through to existing Insert-mode dispatch
```

`vimUpdate` lives in `vim.go`. The existing Insert-mode switch gets one new case at the top:

```go
case m.vimEnabled && (key.Matches(msg, vimEscKey) || msg.String() == "ctrl+["):
    m.snapshotUndo()
    m.vim.mode = ModeNormal
    if m.col > 0 { m.SetCursorColumn(m.col - 1) }
    m.applyVimCursorShape()
```

`PasteMsg` handling is unchanged — paste always inserts at cursor regardless of mode (matches vim behavior under the hood when paste is bracketed).

## Word Definitions

Vim distinguishes "word" (the default `iskeyword`) from "WORD" (whitespace-delimited):

- **word**: contiguous run of alphanumerics + `_`. Non-word non-whitespace chars (`,.;:(){}[]` etc) are their own "word" of length 1 for motion purposes.
- **WORD**: contiguous run of non-whitespace.

The existing `wordLeft` / `wordRight` use whitespace boundaries (WORD semantics). `w/b/e` need new implementations that respect punctuation boundaries; `W/B/E` reuse the existing helpers.

## File Layout

**New files:**

- `textarea/vim.go` — `VimMode` constants, `vimState`, `vimFind`, `undoStack`, `vimUpdate`, motion implementations not already on `Model` (`w/b/e` with punctuation semantics, find/till, paragraph motion), operator dispatch, text-object resolvers, undo/redo logic.
- `textarea/vim_test.go` — table-driven tests.

**Modified files:**

- `textarea/textarea.go`:
  - Add `vimEnabled bool`, `vim vimState`, `undo undoStack` fields to `Model`.
  - Add `SelectedText` field to `StyleState`.
  - Add the 4 public methods listed under "Public API".
  - Add the dispatch hook at the top of `Update()`.
  - Add the `Esc`/`Ctrl-[` Insert-mode case.
  - Extend `view()`'s per-wrapped-line render loop to apply selection styling.
  - Add `snapshotUndo()`, `applyVimCursorShape()`, helpers.

## Testing Strategy

`vim_test.go` is table-driven, with one table per category:

1. **Mode transitions**: every entry into and out of each mode.
2. **Motions**: each motion in isolation. Boundary cases: col 0, end of line, single-row buffer, empty line, multi-byte runes.
3. **Operators over motions**: `dw d$ d0 dG di" caw yi(` etc. Verify deleted text, cursor position, yank buffer contents, and yank-linewise flag.
4. **Linewise ops**: `dd cc yy D C Y p P`. Verify line count, cursor position, paste placement.
5. **Text objects**: every supported object, with and without operators, and inside Visual.
6. **Visual mode**: anchor + extend + consume. Verify selection range and rendered output via a test helper that strips ANSI and locates the selection-styled run.
7. **Counts**: `3w`, `5x`, `10dd`, `7G`. Verify cap at 9999.
8. **Find / till**: `f{c}`, `T{c}`, `;`, `,`. Verify behavior at end of line and missing target.
9. **Undo / redo**: insert session as one step, edit-then-undo, edit-then-undo-then-redo, edit-after-undo truncates redo, bounded ring drops oldest.
10. **Cursor shapes**: each mode → expected shape; restore on disable.
11. **Opt-in**: `VimEnabled() == false` preserves all existing tests pass unchanged.

Tests synthesize `tea.KeyPressMsg` and assert on `Value()`, `Line()`, `Column()`, `VimMode()`, and (for selection) a test-only accessor for the normalized range.

## Open Questions

None at this point. Refinements expected during implementation will be tracked in the implementation plan.

## Out of Scope (Future Work)

- Named registers, marks, macros.
- Search (`/`, `?`, `n`, `N`).
- Block visual (`Ctrl-v`).
- Multi-line bracket text objects.
- `VimKeyMap` for rebinding.
- `%` jump to matching bracket.
- `J` join lines, `K` keyword lookup, etc.
