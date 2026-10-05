package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// searchDebounceDelay is how long the overlay waits after the last keystroke
// before hitting the server, so fast typing doesn't fire a request per key.
const searchDebounceDelay = 200 * time.Millisecond

// searchPageSize is roughly how many items ctrl+u/ctrl+d jump by in
// navigate mode. It's an approximation rather than an exact screenful,
// since section headers and description lines are interleaved unevenly
// between the actual selectable rows.
const searchPageSize = 5

// searchOverlay is the "ctrl+k" global search popup, reachable from any tab.
// It's modal, like vim: typing a query is "insert mode"; Enter freezes the
// query and drops into "navigate mode" (vim motions, space to multi-select
// tracks/albums, Enter to replace the queue with the selection); Esc from
// navigate mode returns to editing the query.
//
// Results come from Client.Search (case-insensitive substring matching) and are
// re-ranked client-side with the same fuzzy library bubbles' own list filter
// uses, grouped artists-then-albums-then-tracks. The full ranked set is
// kept — nothing is truncated — and the popup scrolls to it instead.
type searchOverlay struct {
	input      textinput.Model
	navigating bool

	// gen tags each keystroke's eventual debounce/fetch so a slow response
	// to an earlier query can't clobber a newer one.
	gen int

	artists []item
	albums  []item
	tracks  []item
	cursor  int // flat index across artists, then albums, then tracks

	// selected holds the space-toggled tracks/albums, in the order they
	// were picked — that's the order they're queued in on commit.
	selected []item
}

func newSearchOverlay() *searchOverlay {
	ti := textinput.New()
	ti.Placeholder = "search artists, albums, tracks…"
	ti.CharLimit = 100
	ti.Width = 50
	ti.Focus()
	return &searchOverlay{input: ti}
}

func (ov *searchOverlay) clearResults() {
	ov.artists = nil
	ov.albums = nil
	ov.tracks = nil
	ov.cursor = 0
}

func (ov *searchOverlay) total() int {
	return len(ov.artists) + len(ov.albums) + len(ov.tracks)
}

func (ov *searchOverlay) moveCursor(delta int) {
	n := ov.total()
	if n == 0 {
		ov.cursor = 0
		return
	}
	ov.cursor = min(max(ov.cursor+delta, 0), n-1)
}

// current resolves the flat cursor to the item it currently points at.
func (ov *searchOverlay) current() (item, bool) {
	i := ov.cursor
	if i < len(ov.artists) {
		return ov.artists[i], true
	}
	i -= len(ov.artists)
	if i < len(ov.albums) {
		return ov.albums[i], true
	}
	i -= len(ov.albums)
	if i < len(ov.tracks) {
		return ov.tracks[i], true
	}
	return item{}, false
}

func (ov *searchOverlay) selectedIndex(it item) int {
	for i, s := range ov.selected {
		if s.kind == it.kind && s.id == it.id {
			return i
		}
	}
	return -1
}

func (ov *searchOverlay) isSelected(it item) bool {
	return ov.selectedIndex(it) >= 0
}

// toggleSelected marks the item under the cursor for queueing, or unmarks
// it if it's already marked. Tracks, albums, and artists are all
// selectable — cmdCommitSearchQueue expands each to its constituent
// tracks when the selection is committed.
func (ov *searchOverlay) toggleSelected() {
	it, ok := ov.current()
	if !ok {
		return
	}
	switch it.kind {
	case itemTrack, itemAlbum, itemArtist:
	default:
		return
	}
	if i := ov.selectedIndex(it); i >= 0 {
		ov.selected = append(ov.selected[:i], ov.selected[i+1:]...)
		return
	}
	ov.selected = append(ov.selected, it)
}

// applyResults re-ranks a fresh SearchResult against the overlay's current
// query and rebuilds the display lists — the full ranked set, unclamped.
func (ov *searchOverlay) applyResults(result SearchResult) {
	query := strings.TrimSpace(ov.input.Value())

	artistIdx := fuzzyRank(query, result.Artists)
	ov.artists = make([]item, len(artistIdx))
	for i, idx := range artistIdx {
		ov.artists[i] = artistItem(result.Artists[idx])
	}

	albumLabels := make([]string, len(result.Albums))
	for i, a := range result.Albums {
		albumLabels[i] = a.Name + " " + a.AlbumArtist
	}
	albumIdx := fuzzyRank(query, albumLabels)
	ov.albums = make([]item, len(albumIdx))
	for i, idx := range albumIdx {
		ov.albums[i] = albumItem(result.Albums[idx])
	}

	trackLabels := make([]string, len(result.Tracks))
	for i, t := range result.Tracks {
		trackLabels[i] = t.Title + " " + t.Artist + " " + t.Album
	}
	trackIdx := fuzzyRank(query, trackLabels)
	ov.tracks = make([]item, len(trackIdx))
	for i, idx := range trackIdx {
		ov.tracks[i] = trackItem(result.Tracks[idx])
	}

	ov.cursor = 0
}

// fuzzyRank scores labels against query and returns every match's index
// into labels, best match first.
func fuzzyRank(query string, labels []string) []int {
	matches := fuzzy.Find(query, labels)
	idx := make([]int, len(matches))
	for i, m := range matches {
		idx[i] = m.Index
	}
	return idx
}

// --- messages / commands ---

type searchDebounceMsg struct{ gen int }

type searchResultsMsg struct {
	gen    int
	result SearchResult
	err    error
}

func searchDebounce(gen int) tea.Cmd {
	return tea.Tick(searchDebounceDelay, func(time.Time) tea.Msg { return searchDebounceMsg{gen: gen} })
}

func cmdSearch(c *Client, gen int, query string) tea.Cmd {
	return func() tea.Msg {
		result, err := c.Search(query)
		return searchResultsMsg{gen: gen, result: result, err: err}
	}
}

// trackIDsForItem resolves one search result to the track URIs it stands
// for: a track is itself; an album expands to its own track list; an
// artist expands to every track across all of that artist's albums.
func trackIDsForItem(c *Client, it item) ([]string, error) {
	switch it.kind {
	case itemTrack:
		return []string{it.id}, nil
	case itemAlbum:
		tracks, err := c.AlbumTracks(it.id)
		return trackIDs(tracks), err
	case itemArtist:
		tracks, err := c.ArtistTracks(it.id)
		return trackIDs(tracks), err
	}
	return nil, nil
}

// cmdCommitSearchQueue replaces the whole queue with every track the
// selected results resolve to, in order, without starting playback.
func cmdCommitSearchQueue(c *Client, items []item) tea.Cmd {
	return func() tea.Msg {
		var ids []string
		for _, it := range items {
			trackIDs, err := trackIDsForItem(c, it)
			if err != nil {
				return action("", err)
			}
			ids = append(ids, trackIDs...)
		}
		err := c.ReplaceQueue(ids, false)
		return action(fmt.Sprintf("queued %d tracks", len(ids)), err)
	}
}

// --- Model integration ---

func (m *Model) startSearch() tea.Cmd {
	m.searchOverlay = newSearchOverlay()
	return textinput.Blink
}

// handleSearchKey is handleKey's dispatch while the search overlay is open,
// split by its two modes.
func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchOverlay.navigating {
		return m.handleSearchNavKey(msg)
	}
	return m.handleSearchEditKey(msg)
}

// handleSearchEditKey is "insert mode": every key but Esc (close) and Enter
// (freeze the query and drop into navigate mode) goes straight to the query
// textinput.
func (m Model) handleSearchEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchOverlay = nil
		return m, nil

	case "enter":
		if m.searchOverlay.total() == 0 {
			return m, nil
		}
		m.searchOverlay.navigating = true
		m.searchOverlay.input.Blur()
		return m, nil

	default:
		prev := m.searchOverlay.input.Value()
		var cmd tea.Cmd
		m.searchOverlay.input, cmd = m.searchOverlay.input.Update(msg)
		if m.searchOverlay.input.Value() != prev {
			m.searchOverlay.gen++
			cmd = tea.Batch(cmd, searchDebounce(m.searchOverlay.gen))
		}
		return m, cmd
	}
}

// handleSearchNavKey is "normal mode": the app's usual vim-style list
// bindings (listKeys) move the cursor, space toggles the current item into
// the queue selection, "into" jumps straight to where it lives, Enter
// commits the selection to the queue, and Esc goes back to editing the
// query.
func (m Model) handleSearchNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ov := m.searchOverlay
	switch {
	case msg.String() == "esc":
		ov.navigating = false
		ov.input.Focus()
		return m, textinput.Blink

	case msg.String() == "enter":
		cmd := m.commitSearchSelection()
		m.searchOverlay = nil
		return m, cmd

	case msg.String() == " ":
		ov.toggleSelected()
		return m, nil

	case key.Matches(msg, keys.into):
		cmd := m.jumpToSearchLocation()
		m.searchOverlay = nil
		return m, cmd

	case key.Matches(msg, listKeys.CursorUp):
		ov.moveCursor(-1)
		return m, nil
	case key.Matches(msg, listKeys.CursorDown):
		ov.moveCursor(1)
		return m, nil
	case key.Matches(msg, listKeys.PrevPage):
		ov.moveCursor(-searchPageSize)
		return m, nil
	case key.Matches(msg, listKeys.NextPage):
		ov.moveCursor(searchPageSize)
		return m, nil
	case key.Matches(msg, listKeys.GoToStart):
		ov.cursor = 0
		return m, nil
	case key.Matches(msg, listKeys.GoToEnd):
		ov.cursor = max(ov.total()-1, 0)
		return m, nil
	}
	return m, nil
}

// commitSearchSelection is navigate mode's Enter: queue every space-marked
// item, or — if nothing was marked — just the one under the cursor, so a
// single pick doesn't require pressing space first.
func (m *Model) commitSearchSelection() tea.Cmd {
	if m.searchOverlay == nil {
		return nil
	}
	items := m.searchOverlay.selected
	if len(items) == 0 {
		if it, ok := m.searchOverlay.current(); ok {
			items = []item{it}
		}
	}
	if len(items) == 0 {
		return nil
	}
	return cmdCommitSearchQueue(m.client, items)
}

// jumpToSearchLocation is navigate mode's "into": switch to the item's home
// tab (reset to root so it lands cleanly instead of stacking onto wherever
// that tab was last left) and drill straight to where it lives, pre-selecting
// it when it's a track within its album.
func (m *Model) jumpToSearchLocation() tea.Cmd {
	if m.searchOverlay == nil {
		return nil
	}
	it, ok := m.searchOverlay.current()
	if !ok {
		return nil
	}
	switch it.kind {
	case itemArtist, itemAlbum:
		tab := tabAlbums
		if it.kind == itemArtist {
			tab = tabArtists
		}
		rootCmd := m.jumpToTab(tab)
		s, cmd, _ := screenForItem(m.client, it)
		m.push(s)
		return tea.Batch(rootCmd, cmd, m.refreshPreview())

	case itemTrack:
		rootCmd := m.jumpToTab(tabAlbums)
		album := Album{ID: it.track.AlbumID, Name: it.track.Album}
		s, cmd := newAlbumTracksScreen(m.client, album)
		s.selectID = it.id
		m.push(s)
		return tea.Batch(rootCmd, cmd, m.refreshPreview())
	}
	return nil
}

// --- rendering ---

// searchRow is one line of the popup's scrollable results area: a section
// header or blank spacer (not selectable), or a selectable item's own row
// (it is valid, itemIdx matching searchOverlay.cursor's numbering), or an
// item's description line (not selectable, immediately follows its item).
type searchRow struct {
	text       string
	it         item
	itemIdx    int
	selectable bool
}

func buildSearchRows(ov *searchOverlay) []searchRow {
	var rows []searchRow
	idx := 0
	appendSection := func(title string, items []item) {
		if len(items) == 0 {
			return
		}
		rows = append(rows, searchRow{text: ""}, searchRow{text: footerLabelStyle.Render(title)})
		for _, it := range items {
			rows = append(rows, searchRow{it: it, itemIdx: idx, selectable: true})
			if it.desc != "" {
				rows = append(rows, searchRow{text: "      " + footerLabelStyle.Render(it.desc)})
			}
			idx++
		}
	}
	appendSection("ARTISTS", ov.artists)
	appendSection("ALBUMS", ov.albums)
	appendSection("TRACKS", ov.tracks)
	return rows
}

// searchBoxOverhead is everything promptBoxStyle and the popup's own fixed
// header add above the scrollable rows: a rounded border (top+bottom) and
// 1-line vertical padding on each side (4 rows total), plus the "Search"
// title, a blank line, the query input, and the mode hint line (4 rows).
const searchBoxOverhead = 8

// truncateLine ansi-aware-truncates s to width cells, preserving any
// lipgloss styling codes it carries (a naive byte-slice would corrupt them).
func truncateLine(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func searchHint(navigating bool) string {
	if navigating {
		return "j/k move · space select · l jump · enter queue selection · esc edit search"
	}
	return "enter to browse results · esc to close"
}

// renderSearchOverlay never lets its box grow taller than height: instead
// of letting the section list overflow (which pushed the box's top off
// screen), it windows the rows around the cursor and scrolls as it moves.
func renderSearchOverlay(width, height int, ov *searchOverlay) string {
	lines := []string{
		listTitleStyle.Render("Search"),
		"",
		ov.input.View(),
		footerLabelStyle.Render(searchHint(ov.navigating)),
	}

	switch {
	case strings.TrimSpace(ov.input.Value()) == "":
		lines = append(lines, "", footerLabelStyle.Render("type to search artists, albums, and tracks"))
	case ov.total() == 0:
		lines = append(lines, "", footerLabelStyle.Render("no matches"))
	default:
		rows := buildSearchRows(ov)
		cursorRow := 0
		for i, r := range rows {
			if r.selectable && r.itemIdx == ov.cursor {
				cursorRow = i
				break
			}
		}

		viewHeight := max(height-searchBoxOverhead, 3)
		scrollable := len(rows) > viewHeight
		if scrollable {
			viewHeight-- // reserve a line for the "N/M results" indicator
		}

		maxTop := max(len(rows)-viewHeight, 0)
		top := min(max(cursorRow-viewHeight/2, 0), maxTop)
		bottom := min(top+viewHeight, len(rows))

		for _, r := range rows[top:bottom] {
			if !r.selectable {
				lines = append(lines, r.text)
				continue
			}
			selMark, curMark := " ", " "
			if ov.isSelected(r.it) {
				selMark = "✓"
			}
			title := r.it.title
			if r.itemIdx == ov.cursor {
				curMark = "›"
				title = searchSelectedStyle.Render(title)
			}
			lines = append(lines, selMark+curMark+" "+title)
		}
		if scrollable {
			lines = append(lines, footerLabelStyle.Render(fmt.Sprintf("%d/%d results", ov.cursor+1, ov.total())))
		}
	}

	// Truncate each line to fit inside the box *before* rendering it, rather
	// than capping the whole rendered block's width afterward — MaxWidth on
	// the already-bordered block truncates raw cells including the right
	// border itself once any line (a long track title, say) runs past it,
	// which is what was clipping the box's right edge.
	boxWidth := min(max(width-4, 20), 72)
	innerWidth := max(boxWidth-6, 10) // boxWidth minus border(2) and padding(4)
	for i, l := range lines {
		lines[i] = truncateLine(l, innerWidth)
	}

	box := promptBoxStyle.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
