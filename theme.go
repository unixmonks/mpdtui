package main

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Theme is a named, single (non-adaptive) built-in color palette — see
// builtinThemes below. The app's own "default" look isn't one of these: it
// keeps distinct light/dark values so it auto-adapts to the terminal's
// background (see defaultPalette), which a single hex per color can't do.
type Theme struct {
	Name   string
	Accent string
	// Focus is a lighter tint of Accent, used for the highlighted row in
	// whichever Miller column is actually being navigated right now (the
	// "current" column). The parent and preview columns either side of it
	// keep the plain Accent highlight, so the one column your cursor keys
	// actually move within always reads as visually distinct from the
	// merely-contextual columns around it.
	Focus  string
	Subtle string
	Good   string
	Bad    string
}

var builtinThemes = map[string]Theme{
	"dracula": {
		Name:   "Dracula",
		Accent: "#bd93f9",
		Focus:  "#dbc4fc",
		Subtle: "#6272a4",
		Good:   "#50fa7b",
		Bad:    "#ff5555",
	},
	"nord": {
		Name:   "Nord",
		Accent: "#88c0d0",
		Focus:  "#bedce5",
		Subtle: "#4c566a",
		Good:   "#a3be8c",
		Bad:    "#bf616a",
	},
	"gruvbox": {
		Name:   "Gruvbox Dark",
		Accent: "#fe8019",
		Focus:  "#feb980",
		Subtle: "#928374",
		Good:   "#b8bb26",
		Bad:    "#fb4934",
	},
	"catppuccin": {
		Name:   "Catppuccin Mocha",
		Accent: "#cba6f7",
		Focus:  "#e2cefb",
		Subtle: "#6c7086",
		Good:   "#a6e3a1",
		Bad:    "#f38ba8",
	},
	"solarized": {
		Name:   "Solarized Dark",
		Accent: "#268bd2",
		Focus:  "#88bfe6",
		Subtle: "#586e75",
		Good:   "#859900",
		Bad:    "#dc322f",
	},
	"tokyonight": {
		Name:   "Tokyo Night",
		Accent: "#7aa2f7",
		Focus:  "#b6ccfb",
		Subtle: "#565f89",
		Good:   "#9ece6a",
		Bad:    "#f7768e",
	},
	"onedark": {
		Name:   "One Dark",
		Accent: "#61afef",
		Focus:  "#a8d3f6",
		Subtle: "#5c6370",
		Good:   "#98c379",
		Bad:    "#e06c75",
	},
	"rosepine": {
		Name:   "Rosé Pine",
		Accent: "#c4a7e7",
		Focus:  "#dfcff2",
		Subtle: "#6e6a86",
		Good:   "#31748f",
		Bad:    "#eb6f92",
	},
	"everforest": {
		Name:   "Everforest",
		Accent: "#a7c080",
		Focus:  "#cfdcb9",
		Subtle: "#859289",
		Good:   "#83c092",
		Bad:    "#e67e80",
	},
	"monokai": {
		Name:   "Monokai",
		Accent: "#66d9ef",
		Focus:  "#abeaf6",
		Subtle: "#75715e",
		Good:   "#a6e22e",
		Bad:    "#f92672",
	},
}

// themeOrder is the ctrl+t picker's list, in display/navigation order; ""
// is the app's own adaptive default, always first.
var themeOrder = []string{
	"", "dracula", "nord", "gruvbox", "catppuccin",
	"solarized", "tokyonight", "onedark", "rosepine", "everforest", "monokai",
}

// themeLabel is the human-readable name shown in the picker and the
// confirmation toast, for a name as found in themeOrder/builtinThemes.
func themeLabel(name string) string {
	if name == "" {
		return "Default"
	}
	return builtinThemes[name].Name
}

// palette is the resolved, ready-to-apply color set: an AdaptiveColor pair
// for the default theme, or plain fixed lipgloss.Colors for everything
// else (including a custom override — see resolveTheme).
type palette struct {
	accent, focus, subtle, good, bad lipgloss.TerminalColor
}

func defaultPalette() palette {
	return palette{
		accent: lipgloss.AdaptiveColor{Light: "#7048e8", Dark: "#a78bfa"},
		focus:  lipgloss.AdaptiveColor{Light: "#b09af2", Dark: "#cfbffc"},
		subtle: lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"},
		good:   lipgloss.AdaptiveColor{Light: "#2b8a3e", Dark: "#69db7c"},
		bad:    lipgloss.AdaptiveColor{Light: "#c92a2a", Dark: "#ff8787"},
	}
}

func (t Theme) palette() palette {
	return palette{
		accent: lipgloss.Color(t.Accent),
		focus:  lipgloss.Color(t.Focus),
		subtle: lipgloss.Color(t.Subtle),
		good:   lipgloss.Color(t.Good),
		bad:    lipgloss.Color(t.Bad),
	}
}

// basePalette resolves a theme name (a key in builtinThemes, or "" for the
// adaptive default) to its palette — the pure, unmodified starting point
// for both -theme/config-file resolution and ctrl+t cycling.
func basePalette(name string) (palette, error) {
	if name == "" {
		return defaultPalette(), nil
	}
	t, ok := builtinThemes[name]
	if !ok {
		return palette{}, fmt.Errorf("unknown theme %q", name)
	}
	return t.palette(), nil
}

// applyPalette sets the app's four semantic colors and rebuilds every style
// derived from them (see buildStyles in styles.go). Safe to call again at
// runtime — cycling with ctrl+t is exactly that.
func applyPalette(p palette) {
	accent, focus, subtle, good, bad = p.accent, p.focus, p.subtle, p.good, p.bad
	buildStyles()
}

// ThemeOverride is the optional [theme] table in the same TOML file used
// for keybinding overrides (-keys/$MPDTUI_KEYS): base picks a
// built-in theme (or the adaptive default, if omitted) to start from, and
// any of the four colors set beneath it override just that one value.
// These per-field overrides are a launch-time convenience only — cycling
// with ctrl+t moves between the pure built-ins and ignores them.
type ThemeOverride struct {
	Base   string `toml:"base"`
	Accent string `toml:"accent"`
	Focus  string `toml:"focus"`
	Subtle string `toml:"subtle"`
	Good   string `toml:"good"`
	Bad    string `toml:"bad"`
}

type themeFile struct {
	Theme ThemeOverride `toml:"theme"`
}

// resolveTheme figures out the starting theme (an explicit -theme name
// wins, then a [theme].base in the config file at path, then the adaptive
// default) and layers any per-color overrides from that same file on top.
// It returns the resolved palette and the base theme name (for ctrl+t to
// cycle onward from).
func resolveTheme(flagName, path string) (palette, string, error) {
	var ov ThemeOverride
	if path != "" {
		var tf themeFile
		if _, err := toml.DecodeFile(path, &tf); err != nil {
			return palette{}, "", fmt.Errorf("loading theme from %s: %w", path, err)
		}
		ov = tf.Theme
	}

	name := flagName
	if name == "" {
		name = ov.Base
	}
	p, err := basePalette(name)
	if err != nil {
		return palette{}, "", err
	}

	if ov.Accent != "" {
		p.accent = lipgloss.Color(ov.Accent)
	}
	if ov.Focus != "" {
		p.focus = lipgloss.Color(ov.Focus)
	}
	if ov.Subtle != "" {
		p.subtle = lipgloss.Color(ov.Subtle)
	}
	if ov.Good != "" {
		p.good = lipgloss.Color(ov.Good)
	}
	if ov.Bad != "" {
		p.bad = lipgloss.Color(ov.Bad)
	}
	return p, name, nil
}

// --- theme picker (ctrl+t) ---

// themePicker is the "ctrl+t" popup: moving the cursor previews a theme
// live across the whole app immediately, enter keeps whatever's currently
// highlighted, and esc reverts to whatever was active before the popup
// opened (original).
type themePicker struct {
	cursor   int
	original string
}

func newThemePicker(current string) *themePicker {
	cursor := 0
	for i, n := range themeOrder {
		if n == current {
			cursor = i
			break
		}
	}
	return &themePicker{cursor: cursor, original: current}
}

func (tp *themePicker) moveCursor(delta int) {
	n := len(themeOrder)
	tp.cursor = ((tp.cursor+delta)%n + n) % n
}

func (tp *themePicker) selected() string {
	return themeOrder[tp.cursor]
}

// handleThemePickerKey is handleKey's dispatch while the theme picker is
// open. Movement re-themes live so every step previews instantly; enter
// and esc both just close the popup — enter leaves the live preview in
// place, esc restores tp.original first.
func (m Model) handleThemePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tp := m.themePicker
	switch {
	case msg.String() == "esc":
		m.themeName = tp.original
		m.themePicker = nil
		p, _ := basePalette(m.themeName) // tp.original always came from themeOrder
		m.retheme(p)
		return m, nil

	case msg.String() == "enter":
		m.themePicker = nil
		if cur := m.currentScreen(); cur != nil {
			return m, cur.list.NewStatusMessage("theme: " + themeLabel(m.themeName))
		}
		return m, nil

	case key.Matches(msg, listKeys.CursorUp):
		tp.moveCursor(-1)
	case key.Matches(msg, listKeys.CursorDown):
		tp.moveCursor(1)
	default:
		return m, nil
	}

	m.themeName = tp.selected()
	p, _ := basePalette(m.themeName) // themeOrder entries always resolve
	m.retheme(p)
	return m, nil
}

// --- rendering ---

func renderThemePicker(width, height int, tp *themePicker) string {
	lines := []string{listTitleStyle.Render("Theme"), ""}
	for i, name := range themeOrder {
		label := themeLabel(name)
		if i == tp.cursor {
			lines = append(lines, "› "+searchSelectedStyle.Render(label))
		} else {
			lines = append(lines, "  "+label)
		}
	}
	lines = append(lines, "", footerLabelStyle.Render("j/k move · enter keep · esc cancel"))

	box := promptBoxStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
