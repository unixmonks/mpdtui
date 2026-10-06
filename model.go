package main

import (
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tabArtists = iota
	tabAlbums
	tabGenres
	tabQueue
	tabPlaylists
	numTabs
)

// addPrompt is the small modal shown for "A" (add to playlist): it replaces
// the whole screen while active rather than trying to composite a floating
// box over the list, which keeps the rendering simple and unambiguous.
type addPrompt struct {
	trackID    string
	trackLabel string
	input      textinput.Model
}

type Model struct {
	client  *Client
	events  chan tea.Msg
	initCmd tea.Cmd

	width, height int

	tabs      [numTabs][]screen
	activeTab int

	status    Status
	connected bool

	prompt *addPrompt

	// searchOverlay is the "ctrl+k" global search popup — see search.go.
	searchOverlay *searchOverlay

	pendingD bool
	ddGen    int

	// flashText is a transient message shown in the footer; flashGen lets
	// an older message's clear tick leave a newer one alone.
	flashText string
	flashGen  int

	// artCache/artFetching are keyed by Track.ArtKey and shared across every
	// Model copy bubbletea hands back and forth (maps, like slices, carry
	// their backing storage by reference). playingIndex is a pointer for
	// the same reason: the Queue delegate is handed it once at screen
	// construction and needs to keep seeing updates made to it long after.
	artCache     map[string]image.Image
	artFetching  map[string]bool
	playingIndex *int

	// showCoverArt is the "c" toggle for the Queue screen's art pane.
	showCoverArt bool

	// showHelp is the "?" full-screen shortcut reference.
	showHelp bool

	// showTabBar is the "ctrl+b" toggle for the top-of-screen section bar.
	showTabBar bool

	// themeName is the active theme's key in themeOrder/builtinThemes ("" for
	// the adaptive default) — see theme.go.
	themeName string

	// themePicker is the "ctrl+t" theme picker popup — see theme.go.
	themePicker *themePicker

	// preview is the Miller-column "one level ahead" pane for whatever's
	// selected in the active tab's current screen — see refreshPreview.
	// It's not part of any tab's navigation stack; "into" promotes it into
	// the stack instead of re-fetching. previewKey identifies which item
	// it's a preview of, so it isn't refetched on every render.
	preview    *screen
	previewKey string
}

func newModel(client *Client, events chan tea.Msg, themeName string) Model {
	playingIndex := -1
	m := Model{
		client:       client,
		events:       events,
		connected:    true,
		artCache:     map[string]image.Image{},
		artFetching:  map[string]bool{},
		playingIndex: &playingIndex,
		showCoverArt: true,
		showTabBar:   true,
		themeName:    themeName,
	}
	s, cmd := newArtistsScreen(client)
	m.tabs[tabArtists] = []screen{s}
	m.initCmd = tea.Batch(cmd, waitForMsg(events), cmdFetchStatus(client), statusTick())
	return m
}

func waitForMsg(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

type ddClearMsg struct{ gen int }

func ddTimeout(gen int) tea.Cmd {
	return tea.Tick(600*time.Millisecond, func(time.Time) tea.Msg { return ddClearMsg{gen: gen} })
}

type flashClearMsg struct{ gen int }

// flash shows text in the footer for a few seconds. List status bars are
// hidden (see newListWithDelegate), so this is where action results and
// errors surface.
func (m *Model) flash(text string, d time.Duration) tea.Cmd {
	m.flashText = text
	m.flashGen++
	gen := m.flashGen
	return tea.Tick(d, func(time.Time) tea.Msg { return flashClearMsg{gen: gen} })
}

func (m *Model) flashErr(err error) tea.Cmd {
	return m.flash(errorStyle.Render(err.Error()), 6*time.Second)
}

func (m Model) Init() tea.Cmd { return m.initCmd }

// --- navigation helpers (pointer receiver so callers mutate the Model's
// local copy in place inside Update) ---

func (m *Model) currentScreen() *screen {
	stack := m.tabs[m.activeTab]
	if len(stack) == 0 {
		return nil
	}
	return &stack[len(stack)-1]
}

func (m *Model) footerHeight() int { return 2 } // border-top + 1 content line

// headerHeight is the tab bar's height when it's showing (a label row plus
// its bottom border) plus one more row when the active screen is filtering
// — see renderFilterRow, which draws in the same spot the old breadcrumb
// title used to.
func (m *Model) headerHeight() int {
	h := 0
	if m.showTabBar {
		h += 2
	}
	if _, ok := m.renderFilterRow(); ok {
		h++
	}
	return h
}

func (m *Model) contentSize() (int, int) {
	h := m.height - m.footerHeight() - m.headerHeight()
	if h < 3 {
		h = 3
	}
	return m.width, h
}

func (m *Model) resizeScreen(s *screen) {
	if s == nil {
		return
	}
	w, h := m.contentSize()
	if s.kind == screenQueue && m.showCoverArt {
		w, _ = queueSplit(w, h)
	}
	s.list.SetSize(w, h)
}

// queueSplit divides the Queue screen's content area between the track list
// and the now-playing art pane. The list always gets queueMinListWidth
// first; only whatever's left over — up to queueArtMaxWidth — goes to art,
// and art disappears entirely once there isn't enough room left for it to
// read as a picture rather than a smear of blocks.
const (
	queueMinListWidth = 40
	queueArtGap       = 2
	queueArtMinWidth  = 10
	queueArtMaxWidth  = 40
)

func queueSplit(total, height int) (listW, artW int) {
	avail := total - queueMinListWidth - queueArtGap
	if avail < queueArtMinWidth {
		return total, 0
	}
	artW = min(avail, queueArtMaxWidth)
	return total - artW - queueArtGap, artW
}

func (m *Model) resizeAll() {
	for t := range m.tabs {
		for i := range m.tabs[t] {
			m.resizeScreen(&m.tabs[t][i])
		}
	}
}

// retheme applies a new palette and re-styles every already-constructed
// screen (every tab's stack, plus the Miller-column preview) so a live
// theme switch (ctrl+t) takes effect everywhere instantly. A list
// delegate's colors are otherwise baked in once at construction and
// wouldn't pick up a later palette change on their own — see
// styledDefaultDelegate in styles.go.
func (m *Model) retheme(p palette) {
	applyPalette(p)
	restyle := func(s *screen) {
		switch s.kind {
		case screenGenreTracks, screenQueue, screenPlaylistTracks:
			// trackColumnsDelegate reads the theme live at render time.
		default:
			s.list.SetDelegate(styledDefaultDelegate())
		}
	}
	for t := range m.tabs {
		for i := range m.tabs[t] {
			restyle(&m.tabs[t][i])
		}
	}
	if m.preview != nil {
		restyle(m.preview)
	}
}

// push adds s on top of the active tab's navigation stack. There's no
// breadcrumb text to maintain: with Miller columns, the parent and preview
// panes either side of it show that context spatially instead.
func (m *Model) push(s screen) {
	m.resizeScreen(&s)
	m.tabs[m.activeTab] = append(m.tabs[m.activeTab], s)
}

func (m *Model) goBack() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil {
		return nil
	}
	if cur.list.FilterState() != list.Unfiltered {
		prevSel, _ := cur.list.SelectedItem().(item)
		var cmd tea.Cmd
		cur.list, cmd = cur.list.Update(tea.KeyMsg{Type: tea.KeyEsc})
		cmds := []tea.Cmd{cmd}
		if newSel, ok := cur.list.SelectedItem().(item); ok && newSel != prevSel {
			cmds = append(cmds, m.refreshPreview())
		}
		return tea.Batch(cmds...)
	}
	if len(m.tabs[m.activeTab]) > 1 {
		m.tabs[m.activeTab] = m.tabs[m.activeTab][:len(m.tabs[m.activeTab])-1]
	}
	return m.refreshPreview()
}

// newRootScreen builds tab i's top-of-stack screen from scratch (the one
// every tab starts on before any drilling in).
func newRootScreen(client *Client, playingIndex *int, i int) (screen, tea.Cmd) {
	switch i {
	case tabArtists:
		return newArtistsScreen(client)
	case tabAlbums:
		return newAlbumsScreen(client)
	case tabGenres:
		return newGenresScreen(client)
	case tabQueue:
		return newQueueScreen(client, playingIndex)
	case tabPlaylists:
		return newPlaylistsScreen(client)
	}
	return screen{}, nil
}

func (m *Model) switchTab(i int) tea.Cmd {
	m.activeTab = i
	if len(m.tabs[i]) > 0 {
		m.resizeScreen(m.currentScreen())
		return m.refreshPreview()
	}
	s, cmd := newRootScreen(m.client, m.playingIndex, i)
	m.resizeScreen(&s)
	m.tabs[i] = []screen{s}
	return tea.Batch(cmd, m.refreshPreview())
}

// jumpToTab switches to tab i and resets it to a fresh root screen, even if
// it already had a navigation stack — used when a search-overlay result
// jumps into a tab, so it lands cleanly instead of stacking onto wherever
// that tab was last left.
func (m *Model) jumpToTab(i int) tea.Cmd {
	m.activeTab = i
	s, cmd := newRootScreen(m.client, m.playingIndex, i)
	m.resizeScreen(&s)
	m.tabs[i] = []screen{s}
	return tea.Batch(cmd, m.refreshPreview())
}

// findScreen locates a screen by id anywhere across every tab's stack, so a
// background fetch can land correctly even if the user has since navigated
// elsewhere.
func (m *Model) findScreen(id int) *screen {
	for t := range m.tabs {
		for i := range m.tabs[t] {
			if m.tabs[t][i].id == id {
				return &m.tabs[t][i]
			}
		}
	}
	if m.preview != nil && m.preview.id == id {
		return m.preview
	}
	return nil
}

// isCurrentScreen reports whether id is the active tab's top-of-stack
// screen — used to decide whether a just-finished load should also
// refresh the Miller-column preview (only the screen actually on screen
// drives it).
func (m *Model) isCurrentScreen(id int) bool {
	cur := m.currentScreen()
	return cur != nil && cur.id == id
}

func (m *Model) queueScreen() *screen {
	if len(m.tabs[tabQueue]) == 0 {
		return nil
	}
	return &m.tabs[tabQueue][0]
}

// --- item-kind-driven actions, shared across every screen that happens to
// contain that kind of item (search results, album tracks, genre tracks,
// and playlist tracks are all just lists of track items, for instance). ---

// screenForItem builds the screen that it's children live on — what "into"
// pushes, and what a Miller-column preview shows ahead of time. ok is false
// for items with no children (a track).
func screenForItem(client *Client, it item) (s screen, cmd tea.Cmd, ok bool) {
	switch it.kind {
	case itemArtist:
		s, cmd = newAlbumsByArtistScreen(client, it.id)
	case itemGenre:
		s, cmd = newGenreTracksScreen(client, it.id)
	case itemPlaylist:
		s, cmd = newPlaylistTracksScreen(client, it.id)
	case itemAlbum:
		s, cmd = newAlbumTracksScreen(client, *it.album)
	default:
		return screen{}, nil, false
	}
	return s, cmd, true
}

// previewKeyFor identifies an item for previewKey's dedupe check.
func previewKeyFor(it item) string {
	return fmt.Sprintf("%d:%s", it.kind, it.id)
}

// drillInto pushes a new screen for the selected item's children ("l" — the
// tree-navigation counterpart to goBack's "h"). Tracks have no children, so
// it's a no-op on a track; playing one is enter's job (playCurrent). If the
// Miller-column preview pane already has this exact item loaded, it's
// promoted straight into the stack instead of being fetched again.
func (m *Model) drillInto() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil {
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok {
		return nil
	}

	if m.preview != nil && m.previewKey == previewKeyFor(it) {
		s := *m.preview
		m.preview, m.previewKey = nil, ""
		m.push(s)
		return m.refreshPreview()
	}

	s, cmd, hasChildren := screenForItem(m.client, it)
	if !hasChildren {
		return nil
	}
	m.push(s)
	return tea.Batch(cmd, m.refreshPreview())
}

// refreshPreview recomputes the Miller-column preview pane for whatever's
// now selected in the active tab's current screen. It's a no-op for tabs
// that aren't a browsing hierarchy (just the Queue) and clears the preview
// when there's nothing selected or the selection has no children.
func (m *Model) refreshPreview() tea.Cmd {
	if m.activeTab == tabQueue {
		m.preview, m.previewKey = nil, ""
		return nil
	}
	cur := m.currentScreen()
	if cur == nil {
		m.preview, m.previewKey = nil, ""
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok {
		m.preview, m.previewKey = nil, ""
		return nil
	}
	key := previewKeyFor(it)
	if key == m.previewKey {
		return nil // already previewing this exact item
	}
	s, cmd, hasChildren := screenForItem(m.client, it)
	if !hasChildren {
		m.preview, m.previewKey = nil, ""
		return nil
	}
	m.preview, m.previewKey = &s, key
	return cmd
}

// playCurrent plays the selected item. Enter is reserved for this alone —
// navigating into a folder-like item is drillInto's job ("l"). On an
// artist, album, genre, or playlist it replaces the queue with all of its
// tracks and plays from the top; on a track outside the Queue screen it
// replaces the queue with that track plus everything listed below it.
// Within the Queue screen itself, a track just jumps playback to that
// position rather than rebuilding the queue from its own tail.
func (m *Model) playCurrent() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil {
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok {
		return nil
	}
	switch it.kind {
	case itemAlbum:
		return cmdPlayAlbum(m.client, it.id, it.title)
	case itemArtist:
		return cmdPlayArtist(m.client, it.id)
	case itemGenre:
		return cmdPlayGenre(m.client, it.id)
	case itemPlaylist:
		return cmdPlayPlaylist(m.client, it.id)
	case itemTrack:
		if cur.kind == screenQueue {
			return cmdPlayQueueID(m.client, it.track.QueueID)
		}
		items := cur.list.Items()
		ids := make([]string, 0, len(items)-cur.list.Index())
		for _, li := range items[cur.list.Index():] {
			if ti, ok := li.(item); ok && ti.kind == itemTrack {
				ids = append(ids, ti.id)
			}
		}
		return cmdPlayTracksFrom(m.client, ids, it.title)
	}
	return nil
}

func (m *Model) enqueueCurrent() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil {
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok {
		return nil
	}
	switch it.kind {
	case itemTrack:
		return cmdEnqueueTrack(m.client, *it.track)
	case itemAlbum:
		return cmdEnqueueAlbum(m.client, it.id, it.title)
	case itemArtist:
		return cmdEnqueueArtist(m.client, it.id)
	case itemGenre:
		return cmdEnqueueGenre(m.client, it.id)
	case itemPlaylist:
		return cmdEnqueuePlaylist(m.client, it.id)
	}
	return nil
}

func (m *Model) startAddToPlaylist() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil {
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok || it.kind != itemTrack {
		return nil
	}
	ti := textinput.New()
	ti.Placeholder = "playlist name"
	ti.CharLimit = 64
	ti.Width = 40
	ti.Focus()
	m.prompt = &addPrompt{trackID: it.id, trackLabel: it.title, input: ti}
	return textinput.Blink
}

func (m *Model) removeCurrent() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil {
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok || it.track == nil {
		return nil
	}
	switch cur.kind {
	case screenQueue:
		return cmdRemoveQueueID(m.client, it.track.QueueID)
	case screenPlaylistTracks:
		return tea.Batch(
			cmdRemoveFromPlaylist(m.client, cur.ctx, it.track.Pos),
			// stored_playlist idle events don't say which playlist
			// changed, so this screen reloads itself.
			m.reloadScreenAfter(cur),
		)
	}
	return nil
}

func (m *Model) clearQueue() tea.Cmd {
	cur := m.currentScreen()
	if cur == nil || cur.kind != screenQueue {
		return nil
	}
	return cmdClearQueue(m.client)
}

func (m *Model) moveCurrent(dir int) tea.Cmd {
	cur := m.currentScreen()
	if cur == nil || cur.kind != screenQueue {
		return nil
	}
	it, ok := cur.list.SelectedItem().(item)
	if !ok || it.track == nil {
		return nil
	}
	to := it.track.Pos + dir
	if to < 0 || to >= len(cur.list.Items()) {
		return nil
	}
	// Move the cursor along with the track so repeated J/K keeps carrying
	// the same one; the queue reload that follows keeps the index.
	if cur.list.FilterState() == list.Unfiltered {
		cur.list.Select(cur.list.Index() + dir)
	}
	return cmdMoveQueueID(m.client, it.track.QueueID, to)
}

// reloadScreenAfter re-runs a playlist-tracks screen's load once an edit has
// had a moment to land, keeping its cursor where it was.
func (m *Model) reloadScreenAfter(s *screen) tea.Cmd {
	if s.kind != screenPlaylistTracks {
		return nil
	}
	name, id := s.ctx, s.id
	c := m.client
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		tracks, err := c.Playlist(name)
		items := make([]item, len(tracks))
		for i, t := range tracks {
			items[i] = trackItem(t)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	})
}

// --- Update / View ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeAll()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case mpdChangedMsg:
		cmd := m.handleChanged(msg)
		return m, tea.Batch(cmd, waitForMsg(m.events))

	case mpdConnectedMsg:
		m.connected = true
		cmds := []tea.Cmd{waitForMsg(m.events), cmdFetchStatus(m.client)}
		if qs := m.queueScreen(); qs != nil {
			cmds = append(cmds, loadQueue(m.client, qs.id))
		}
		return m, tea.Batch(cmds...)

	case connLostMsg:
		m.connected = false
		return m, waitForMsg(m.events)

	case statusTickMsg:
		return m, tea.Batch(cmdFetchStatus(m.client), statusTick())

	case playerStatusMsg:
		if msg.err != nil {
			m.connected = false
			return m, nil
		}
		m.status = msg.status
		m.connected = true
		return m, m.ensureArtLoaded()

	case itemsLoadedMsg:
		if s := m.findScreen(msg.screenID); s != nil {
			cmd := setItems(&s.list, msg.items)
			if s.selectID != "" {
				for i, it := range msg.items {
					if it.id == s.selectID {
						s.list.Select(i)
						break
					}
				}
				s.selectID = ""
			}
			cmds := []tea.Cmd{cmd}
			if m.isCurrentScreen(msg.screenID) {
				cmds = append(cmds, m.refreshPreview())
			}
			if msg.err != nil {
				cmds = append(cmds, m.flashErr(msg.err))
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case queueLoadedMsg:
		if s := m.findScreen(msg.screenID); s != nil {
			cmd := setItems(&s.list, msg.items)
			if msg.err != nil {
				return m, tea.Batch(cmd, m.flashErr(msg.err))
			}
			return m, cmd
		}
		return m, nil

	case actionResultMsg:
		if msg.err != nil {
			return m, m.flashErr(msg.err)
		}
		if msg.text != "" {
			return m, m.flash(msg.text, 3*time.Second)
		}
		return m, nil

	case flashClearMsg:
		if msg.gen == m.flashGen {
			m.flashText = ""
		}
		return m, nil

	case ddClearMsg:
		if msg.gen == m.ddGen {
			m.pendingD = false
		}
		return m, nil

	case artLoadedMsg:
		delete(m.artFetching, msg.key)
		// A failed lookup is cached as nil too, so an album with no art
		// isn't re-requested on every status tick.
		m.artCache[msg.key] = msg.img
		return m, nil

	case searchDebounceMsg:
		if m.searchOverlay != nil && msg.gen == m.searchOverlay.gen {
			query := strings.TrimSpace(m.searchOverlay.input.Value())
			if query == "" {
				m.searchOverlay.clearResults()
				return m, nil
			}
			return m, cmdSearch(m.client, msg.gen, query)
		}
		return m, nil

	case searchResultsMsg:
		if m.searchOverlay != nil && msg.gen == m.searchOverlay.gen {
			m.searchOverlay.applyResults(msg.result)
		}
		return m, nil
	}

	// Anything else (spinner ticks, textinput blink, filter-match results,
	// etc.) belongs to whichever sub-component is currently live.
	if m.searchOverlay != nil {
		var cmd tea.Cmd
		m.searchOverlay.input, cmd = m.searchOverlay.input.Update(msg)
		return m, cmd
	}
	if m.prompt != nil {
		var cmd tea.Cmd
		m.prompt.input, cmd = m.prompt.input.Update(msg)
		return m, cmd
	}
	if cur := m.currentScreen(); cur != nil {
		prevSel, _ := cur.list.SelectedItem().(item)
		var cmd tea.Cmd
		cur.list, cmd = cur.list.Update(msg)
		cmds := []tea.Cmd{cmd}
		// Filtering resolves asynchronously (bubbles sends a FilterMatchesMsg
		// once matching finishes), so it's this fallthrough — not handleKey's
		// Filtering branch — that actually sees the selection land on the new
		// top match. Without this, the Miller-column preview would keep
		// showing whatever was selected before you started typing.
		if newSel, ok := cur.list.SelectedItem().(item); ok && newSel != prevSel {
			cmds = append(cmds, m.refreshPreview())
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// handleChanged reacts to MPD idle notifications: the queue reloads on
// "playlist", stored-playlist lists reload on "stored_playlist", and any
// player/mixer/options change re-polls status right away instead of
// waiting for the next tick.
func (m *Model) handleChanged(subsystems []string) tea.Cmd {
	var cmds []tea.Cmd
	pollStatus := false
	for _, sub := range subsystems {
		switch sub {
		case "playlist":
			if qs := m.queueScreen(); qs != nil {
				cmds = append(cmds, loadQueue(m.client, qs.id))
			}
			pollStatus = true
		case "stored_playlist":
			for _, s := range m.tabs[tabPlaylists] {
				if s.kind == screenPlaylists {
					cmds = append(cmds, reloadPlaylists(m.client, s.id))
				}
			}
		case "player", "mixer", "options", "update":
			pollStatus = true
		}
	}
	if pollStatus {
		cmds = append(cmds, cmdFetchStatus(m.client))
	}
	return tea.Batch(cmds...)
}

// ensureArtLoaded keeps playingIndex in sync with the current status and,
// if the now-playing track's art isn't cached (or already in flight),
// kicks off a fetch for it.
func (m *Model) ensureArtLoaded() tea.Cmd {
	*m.playingIndex = -1
	if m.status.State == "stopped" || m.status.Track == nil {
		return nil
	}
	if m.status.QueueIndex >= 0 {
		*m.playingIndex = m.status.QueueIndex
	}

	t := m.status.Track
	key := t.ArtKey()
	if _, done := m.artCache[key]; done || m.artFetching[key] {
		return nil
	}
	m.artFetching[key] = true
	return loadArt(m.client, key, t.ID)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.forceQuit) {
		return m, tea.Quit
	}

	if m.searchOverlay != nil {
		return m.handleSearchKey(msg)
	}

	if m.themePicker != nil {
		return m.handleThemePickerKey(msg)
	}

	if m.prompt != nil {
		switch msg.String() {
		case "esc":
			m.prompt = nil
			return m, nil
		case "enter":
			name := m.prompt.input.Value()
			trackID := m.prompt.trackID
			m.prompt = nil
			if name == "" {
				return m, nil
			}
			return m, cmdAddToPlaylist(m.client, name, trackID)
		default:
			var cmd tea.Cmd
			m.prompt.input, cmd = m.prompt.input.Update(msg)
			return m, cmd
		}
	}

	if m.showHelp {
		if key.Matches(msg, keys.help) || key.Matches(msg, keys.back) || key.Matches(msg, keys.quit) {
			m.showHelp = false
		}
		return m, nil
	}

	cur := m.currentScreen()

	if cur != nil && cur.list.FilterState() == list.Filtering {
		prevSel, _ := cur.list.SelectedItem().(item)
		var cmd tea.Cmd
		cur.list, cmd = cur.list.Update(msg)
		cmds := []tea.Cmd{cmd}
		if newSel, ok := cur.list.SelectedItem().(item); ok && newSel != prevSel {
			cmds = append(cmds, m.refreshPreview())
		}
		return m, tea.Batch(cmds...)
	}

	wasPendingD := m.pendingD
	m.pendingD = false
	if wasPendingD && key.Matches(msg, keys.remove) {
		return m, m.removeCurrent()
	}

	switch {
	case key.Matches(msg, keys.tab1):
		return m, m.switchTab(tabArtists)
	case key.Matches(msg, keys.tab2):
		return m, m.switchTab(tabAlbums)
	case key.Matches(msg, keys.tab3):
		return m, m.switchTab(tabGenres)
	case key.Matches(msg, keys.tab4):
		return m, m.switchTab(tabQueue)
	case key.Matches(msg, keys.tab5):
		return m, m.switchTab(tabPlaylists)

	case key.Matches(msg, keys.nextTab):
		return m, m.switchTab((m.activeTab + 1) % numTabs)
	case key.Matches(msg, keys.prevTab):
		return m, m.switchTab((m.activeTab - 1 + numTabs) % numTabs)

	case key.Matches(msg, keys.back):
		return m, m.goBack()
	case key.Matches(msg, keys.into):
		return m, m.drillInto()

	case key.Matches(msg, keys.quit):
		return m, tea.Quit

	case key.Matches(msg, keys.selectItem):
		return m, m.playCurrent()

	case key.Matches(msg, keys.playPause):
		return m, cmdTogglePlayPause(m.client, m.status.State)
	case key.Matches(msg, keys.next):
		return m, cmdNext(m.client)
	case key.Matches(msg, keys.prev):
		return m, cmdPrev(m.client)
	case key.Matches(msg, keys.volUp):
		return m, cmdVolume(m.client, m.status.Volume, 5)
	case key.Matches(msg, keys.volDown):
		return m, cmdVolume(m.client, m.status.Volume, -5)
	case key.Matches(msg, keys.mute):
		return m, cmdToggleMute(m.client, m.status.Muted, m.status.Volume)
	case key.Matches(msg, keys.shuffle):
		return m, cmdToggleShuffle(m.client, m.status.Shuffle)
	case key.Matches(msg, keys.repeat):
		return m, cmdCycleRepeat(m.client, m.status.Repeat)

	case key.Matches(msg, keys.enqueue):
		return m, m.enqueueCurrent()
	case key.Matches(msg, keys.addToPlaylist):
		return m, m.startAddToPlaylist()
	case key.Matches(msg, keys.remove):
		m.pendingD = true
		m.ddGen++
		return m, ddTimeout(m.ddGen)
	case key.Matches(msg, keys.clearQueue):
		return m, m.clearQueue()
	case key.Matches(msg, keys.moveDown):
		return m, m.moveCurrent(1)
	case key.Matches(msg, keys.moveUp):
		return m, m.moveCurrent(-1)

	case key.Matches(msg, keys.toggleCover):
		m.showCoverArt = !m.showCoverArt
		m.resizeScreen(m.queueScreen())
		return m, nil

	case key.Matches(msg, keys.help):
		m.showHelp = true
		return m, nil

	case key.Matches(msg, keys.search):
		return m, m.startSearch()

	case key.Matches(msg, keys.updateDB):
		return m, cmdUpdateDB(m.client)

	case key.Matches(msg, keys.toggleTabBar):
		m.showTabBar = !m.showTabBar
		m.resizeAll()
		return m, nil

	case key.Matches(msg, keys.cycleTheme):
		m.themePicker = newThemePicker(m.themeName)
		return m, nil
	}

	if cur != nil {
		prevSel, _ := cur.list.SelectedItem().(item)
		var cmd tea.Cmd
		cur.list, cmd = cur.list.Update(msg)
		cmds := []tea.Cmd{cmd}
		if newSel, ok := cur.list.SelectedItem().(item); ok && newSel != prevSel {
			cmds = append(cmds, m.refreshPreview())
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "starting mpdtui…"
	}

	if m.searchOverlay != nil {
		return renderSearchOverlay(m.width, m.height, m.searchOverlay)
	}

	if m.themePicker != nil {
		return renderThemePicker(m.width, m.height, m.themePicker)
	}

	if m.prompt != nil {
		box := promptBoxStyle.Render(fmt.Sprintf("Add to playlist\n\n%s\n\n%s", m.prompt.trackLabel, m.prompt.input.View()))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	if m.showHelp {
		return renderHelp(m.width, m.height)
	}

	var header string
	if m.showTabBar {
		header = renderTabBar(m.activeTab, m.width) + "\n"
	}
	if row, ok := m.renderFilterRow(); ok {
		header += row + "\n"
	}

	footer := renderFooter(m.status, m.width, m.connected, m.flashText)
	return header + m.renderBody() + "\n" + footer
}

// tabLabels are the section names shown in the tab bar, in tab-index order.
var tabLabels = [numTabs]string{
	tabArtists:   "Artists",
	tabAlbums:    "Albums",
	tabGenres:    "Genres",
	tabQueue:     "Queue",
	tabPlaylists: "Playlists",
}

func renderTabBar(active int, width int) string {
	labels := make([]string, numTabs)
	for i, name := range tabLabels {
		if i == active {
			labels[i] = tabActiveStyle.Render(name)
		} else {
			labels[i] = tabInactiveStyle.Render(name)
		}
	}
	return tabBarStyle.Width(width).Render(strings.Join(labels, "   "))
}

// renderFilterRow finds whichever screen (if any) is currently filtered —
// possibly the Queue tab's own single list, or one of the active Miller
// tab's parent/current/preview screens — and draws a filter box for it. A
// Miller-column screen's box is drawn at that column's exact offset and
// width, so it stays aligned above the right column even after "into"/
// "back" moves the filtered screen out of the active (current) slot and
// into the parent one, rather than disappearing or spanning columns it no
// longer belongs to.
func (m Model) renderFilterRow() (string, bool) {
	stack := m.tabs[m.activeTab]
	if len(stack) == 0 {
		return "", false
	}
	if cur := stack[len(stack)-1]; cur.kind == screenQueue {
		if cur.list.FilterState() == list.Unfiltered {
			return "", false
		}
		return renderFilterBox(cur.list, m.width), true
	}
	for _, c := range m.millerColumns(m.width) {
		if c.screen.list.FilterState() != list.Unfiltered {
			return strings.Repeat(" ", c.offset) + renderFilterBox(c.screen.list, c.width), true
		}
	}
	return "", false
}

func renderFilterBox(l list.Model, width int) string {
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Render(l.FilterInput.View())
}

// millerColumn is one column of the active tab's Miller-column layout, sized
// and positioned within the content area — shared by renderBody (to lay the
// columns out) and renderFilterRow (to align a filter box with whichever
// column its screen occupies).
type millerColumn struct {
	screen *screen
	width  int
	offset int
	// active marks the column actually being navigated right now — the
	// tab's current screen, where j/k and h/l act. renderBody draws its
	// highlighted row with the theme's lighter focus tint so it always
	// reads as visually distinct from the parent and preview columns
	// either side of it, which keep the plain accent highlight: those are
	// just context (where you came from, and what's one step ahead), not
	// where your cursor keys currently do anything.
	active bool
}

// millerColumns lays out the active tab's current screen as up to three
// columns — parent (one level up, if any) | current (where the cursor
// lives) | preview (one level ahead, if the current selection has
// children) — so browsing never needs a breadcrumb: the columns either
// side of "current" show that context spatially. Returns nil for the
// Queue tab, which isn't a browsing hierarchy and stays a single
// full-width list (see renderBody).
func (m Model) millerColumns(width int) []millerColumn {
	stack := m.tabs[m.activeTab]
	if len(stack) == 0 {
		return nil
	}
	cur := &stack[len(stack)-1]
	if cur.kind == screenQueue {
		return nil
	}

	hasParent := len(stack) > 1
	hasPreview := m.preview != nil

	n := 1
	if hasParent {
		n++
	}
	if hasPreview {
		n++
	}
	const gapWidth = 1
	widths := splitMillerWidths(width-(n-1)*gapWidth, [3]bool{hasParent, true, hasPreview})

	var cols []millerColumn
	offset, wi := 0, 0
	if hasParent {
		cols = append(cols, millerColumn{screen: &stack[len(stack)-2], width: widths[wi], offset: offset})
		offset += widths[wi] + gapWidth
		wi++
	}
	cols = append(cols, millerColumn{screen: cur, width: widths[wi], offset: offset, active: true})
	offset += widths[wi] + gapWidth
	wi++
	if hasPreview {
		cols = append(cols, millerColumn{screen: m.preview, width: widths[wi], offset: offset})
	}
	return cols
}

// renderBody draws the active tab's content: the Queue tab stays a single
// full-width list (plus its own cover-art pane), everything else lays out
// as millerColumns.
func (m Model) renderBody() string {
	stack := m.tabs[m.activeTab]
	if len(stack) == 0 {
		return "loading…"
	}
	w, h := m.contentSize()

	if cur := stack[len(stack)-1]; cur.kind == screenQueue {
		body := cur.list.View()
		if m.showCoverArt {
			if _, artW := queueSplit(w, h); artW > 0 {
				gap := lipgloss.NewStyle().Width(queueArtGap).Height(h).Render("")
				body = lipgloss.JoinHorizontal(lipgloss.Top, body, gap, m.renderQueueArt(artW, h))
			}
		}
		return body
	}

	cols := m.millerColumns(w)
	gap := lipgloss.NewStyle().Width(1).Height(h).Render("")
	body := ""
	for i, c := range cols {
		lc := c.screen.list
		lc.SetSize(c.width, h)
		if c.active {
			// Swap in the lighter-highlight delegate on this render-only
			// copy so the column actually being navigated reads as
			// visually distinct from its plain-accent parent/preview
			// neighbors, without touching the actual screen's delegate
			// (kept at full accent for if this screen stops being current
			// — e.g. goBack demoting it back to a parent).
			lc.SetDelegate(focusDelegateFor(c.screen.kind))
		}
		if i == 0 {
			body = lc.View()
			continue
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, gap, lc.View())
	}
	return body
}

// focusDelegateFor returns the lighter-highlight delegate variant for
// whichever Miller column is currently being navigated, matching whichever
// delegate that screen kind normally uses (see newListWithDelegate's
// callers in screen.go).
func focusDelegateFor(kind screenKind) list.ItemDelegate {
	switch kind {
	case screenGenreTracks, screenPlaylistTracks:
		return trackColumnsDelegate{focus: true}
	default:
		return styledFocusDelegate()
	}
}

// millerColWeights are the relative widths for (parent, current, preview)
// when all three are showing — current wider than parent, preview widest —
// matching ranger's own growing-rightward column proportions.
var millerColWeights = [3]int{2, 3, 4}

// splitMillerWidths divides total width across whichever of the three
// Miller columns are present, in (parent, current, preview) order,
// dropping absent slots and redistributing their share proportionally
// among the rest. The last present column absorbs any rounding remainder
// so the returned widths always sum to total.
func splitMillerWidths(total int, present [3]bool) []int {
	sum := 0
	remaining := 0
	for i, ok := range present {
		if ok {
			sum += millerColWeights[i]
			remaining++
		}
	}
	if sum == 0 {
		return nil
	}
	widths := make([]int, 0, remaining)
	used := 0
	for i, ok := range present {
		if !ok {
			continue
		}
		remaining--
		w := total * millerColWeights[i] / sum
		if remaining == 0 {
			w = total - used
		}
		widths = append(widths, w)
		used += w
	}
	return widths
}

// renderQueueArt renders the now-playing track's cover art (if it's been
// fetched) centered inside a w x h box; it returns a blank box of that size
// otherwise, so the layout doesn't jump around while art is loading or
// nothing is playing.
func (m Model) renderQueueArt(w, h int) string {
	var content string
	if m.status.Track != nil && m.status.State != "stopped" {
		if img := m.artCache[m.status.Track.ArtKey()]; img != nil {
			content = renderArt(img, w, h)
		}
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}
