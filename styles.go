package main

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// accent/subtle/good/bad are the four semantic colors every style below
// derives from. They're package vars rather than constants because a theme
// can be switched live at runtime (ctrl+t) — see theme.go's applyPalette,
// which sets these and calls buildStyles again.
var (
	accent lipgloss.TerminalColor
	// focus is a lighter tint of accent, used only for the highlighted row
	// in whichever Miller column is actually being navigated right now
	// (see styledFocusDelegate and trackColumnsDelegate's focus field) —
	// the parent and preview columns either side of it keep the plain
	// accent highlight, so the column your cursor keys actually move
	// within is always the visually brighter one.
	focus  lipgloss.TerminalColor
	subtle lipgloss.TerminalColor
	good   lipgloss.TerminalColor
	bad    lipgloss.TerminalColor

	listTitleStyle lipgloss.Style

	footerStyle      lipgloss.Style
	footerLabelStyle lipgloss.Style

	playerTrackStyle     lipgloss.Style
	playerBarFilledStyle lipgloss.Style
	playerBarEmptyStyle  lipgloss.Style

	tabBarStyle      lipgloss.Style
	tabActiveStyle   lipgloss.Style
	tabInactiveStyle lipgloss.Style

	connectedStyle    lipgloss.Style
	disconnectedStyle lipgloss.Style

	errorStyle lipgloss.Style

	searchSelectedStyle lipgloss.Style

	promptBoxStyle lipgloss.Style
)

// buildStyles derives every lipgloss.Style in the app from the current
// accent/subtle/good/bad colors. Called once at startup and again whenever
// the theme changes.
func buildStyles() {
	listTitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(accent)

	footerStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(subtle).
		Padding(0, 1)

	footerLabelStyle = lipgloss.NewStyle().Foreground(subtle)

	playerTrackStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	playerBarFilledStyle = lipgloss.NewStyle().Foreground(accent)
	playerBarEmptyStyle = lipgloss.NewStyle().Foreground(subtle)

	tabBarStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(subtle).
		Padding(0, 1)

	tabActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(subtle)

	connectedStyle = lipgloss.NewStyle().Foreground(good)
	disconnectedStyle = lipgloss.NewStyle().Foreground(bad).Bold(true)

	errorStyle = lipgloss.NewStyle().Foreground(bad)

	searchSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)

	promptBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)
}

// styledDefaultDelegate is list.NewDefaultDelegate() configured the way
// every compact (single-line, title-only) list in the app wants it — no
// description row, no inter-item spacing — with its selected-row highlight
// recolored to the theme's accent instead of bubbles' built-in pink.
// Track-columns screens (Queue, genre/playlist tracks) don't use this:
// trackColumnsDelegate reads accent live at render time instead, so it
// never goes stale the way a delegate baked into a list at construction
// time would.
//
// Both newCompactList (construction) and retheme (rebuilding a live
// screen's delegate on a theme change) call this rather than
// list.NewDefaultDelegate() directly, so the two can't drift apart — a
// retheme that reset ShowDescription/spacing to bubbles' defaults previously
// reintroduced blank description lines and double-height rows on every
// compact list after a theme switch.
func styledDefaultDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(accent).BorderForeground(accent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(accent).BorderForeground(accent)
	return d
}

// styledFocusDelegate is styledDefaultDelegate with its selected-row
// highlight recolored to the theme's lighter focus tint instead of the
// full accent — used only for a compact-list screen (Artists, Albums by
// Artist, Genres, Playlists, Album Tracks) when it's the Miller column
// actually being navigated right now. See renderBody's render-only
// delegate swap.
func styledFocusDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(focus).BorderForeground(focus)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(focus).BorderForeground(focus)
	return d
}
