package main

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type screenKind int

const (
	screenArtists screenKind = iota
	screenAlbumsByArtist
	screenAlbums
	screenAlbumTracks
	screenGenres
	screenGenreTracks
	screenQueue
	screenPlaylists
	screenPlaylistTracks
)

// screen is one entry in a tab's navigation stack. Every screen owns its
// own list.Model so cursor position, scroll offset, and any active filter
// are preserved when the user drills in and backs out. There's no title
// bar to give it a heading — the Miller-column layout (see renderBody in
// model.go) already shows a screen's place in the hierarchy spatially, via
// the parent and preview panes either side of it.
type screen struct {
	id   int
	kind screenKind
	list list.Model
	ctx  string // artist / genre / playlist name this screen was opened for

	// selectID, if set, is the id of the item to highlight once this
	// screen's items finish loading (set by a search-overlay jump landing
	// on a specific track within its album); cleared after use.
	selectID string
}

var nextScreenID int

func newScreenID() int {
	nextScreenID++
	return nextScreenID
}

// newCompactList is for screens whose items carry no description (Artists,
// Genres, Playlists) — a single-line-per-item delegate instead of the
// default's title+description pair, so the list isn't full of empty second
// lines.
func newCompactList() list.Model {
	return newListWithDelegate(styledDefaultDelegate())
}

func newListWithDelegate(delegate list.ItemDelegate) list.Model {
	l := list.New(nil, delegate, 0, 0)
	// No title bar, status bar, pagination dots, or help line — every row
	// they'd otherwise take goes to the item list instead, and Miller
	// columns already show a screen's place in the hierarchy spatially.
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	// bubbles reserves a row for its own title/filter bar whenever
	// ShowFilter is on, regardless of whether a filter is actually active.
	// That row lives inside whichever column the filtered screen happens to
	// be — misaligning it against its Miller-column neighbors — so it's
	// left permanently off; renderFilterRow in model.go draws one shared,
	// full-width filter row in the app header instead. This has no effect
	// on "/" itself: FilteringEnabled (left on) is what actually lets it
	// trigger, per bubbles' own list.go.
	l.SetShowFilter(false)

	// listKeys is built from the (possibly user-overridden) key config in
	// keys.go/keyconfig.go — see applyKeyConfig for quit/help/paging details.
	l.KeyMap = listKeys
	return l
}

func setItems(l *list.Model, items []item) tea.Cmd {
	list := make([]list.Item, len(items))
	for i, it := range items {
		list[i] = it
	}
	cmd := l.SetItems(list)
	// bubbles' list.SetItems recomputes how much height the title/status/
	// pagination/help rows need, but it measures the pagination row's height
	// using the *previous* item count's TotalPages (it's only updated at the
	// end of that same call) — so going from "no items yet" to a real count
	// crossing the single-page threshold reserves the wrong height and the
	// list renders one line taller than its size, pushing everything below
	// it down and off screen. Re-applying the current size forces another
	// pass that measures against the now-correct TotalPages.
	l.SetSize(l.Width(), l.Height())
	return cmd
}

// --- screen constructors; each returns the screen plus the tea.Cmd that
// loads its data. ---

func newArtistsScreen(c *Client) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenArtists, list: newCompactList()}
	id := s.id
	return s, func() tea.Msg {
		artists, err := c.Artists()
		items := make([]item, len(artists))
		for i, a := range artists {
			items[i] = artistItem(a)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newAlbumsByArtistScreen(c *Client, artist string) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenAlbumsByArtist, ctx: artist, list: newCompactList()}
	id := s.id
	return s, func() tea.Msg {
		albums, err := c.AlbumsByArtist(artist)
		sort.Slice(albums, func(i, j int) bool {
			return strings.ToLower(albums[i].Name) < strings.ToLower(albums[j].Name)
		})
		items := make([]item, len(albums))
		for i, a := range albums {
			items[i] = albumItem(a)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newAlbumsScreen(c *Client) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenAlbums, list: newCompactList()}
	id := s.id
	return s, func() tea.Msg {
		albums, err := c.Albums()
		items := make([]item, len(albums))
		for i, a := range albums {
			items[i] = albumItem(a)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newAlbumTracksScreen(c *Client, album Album) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenAlbumTracks, ctx: album.ID, list: newCompactList()}
	id := s.id
	return s, func() tea.Msg {
		tracks, err := c.AlbumTracks(album.ID)
		showDisc := false
		for _, t := range tracks {
			if t.DiscNo != tracks[0].DiscNo {
				showDisc = true
				break
			}
		}
		items := make([]item, len(tracks))
		for i, t := range tracks {
			items[i] = albumTrackItem(t, showDisc)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newGenresScreen(c *Client) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenGenres, list: newCompactList()}
	id := s.id
	return s, func() tea.Msg {
		genres, err := c.Genres()
		items := make([]item, len(genres))
		for i, g := range genres {
			items[i] = genreItem(g)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newGenreTracksScreen(c *Client, genre string) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenGenreTracks, ctx: genre, list: newListWithDelegate(trackColumnsDelegate{})}
	id := s.id
	return s, func() tea.Msg {
		tracks, err := c.GenreTracks(genre)
		items := make([]item, len(tracks))
		for i, t := range tracks {
			items[i] = trackItem(t)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newQueueScreen(c *Client, playingIndex *int) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenQueue, list: newListWithDelegate(trackColumnsDelegate{playingIndex: playingIndex})}
	return s, loadQueue(c, s.id)
}

func loadQueue(c *Client, id int) tea.Cmd {
	return func() tea.Msg {
		tracks, err := c.Queue()
		items := make([]item, len(tracks))
		for i, t := range tracks {
			items[i] = trackItem(t)
		}
		return queueLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newPlaylistsScreen(c *Client) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenPlaylists, list: newCompactList()}
	id := s.id
	return s, func() tea.Msg {
		names, err := c.Playlists()
		items := make([]item, len(names))
		for i, n := range names {
			items[i] = playlistItem(n)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

// reloadPlaylists refreshes the Playlists list in place (after an idle
// "stored_playlist" notification), keeping its cursor where it was.
func reloadPlaylists(c *Client, id int) tea.Cmd {
	return func() tea.Msg {
		names, err := c.Playlists()
		items := make([]item, len(names))
		for i, n := range names {
			items[i] = playlistItem(n)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}

func newPlaylistTracksScreen(c *Client, name string) (screen, tea.Cmd) {
	s := screen{id: newScreenID(), kind: screenPlaylistTracks, ctx: name, list: newListWithDelegate(trackColumnsDelegate{})}
	id := s.id
	return s, func() tea.Msg {
		tracks, err := c.Playlist(name)
		items := make([]item, len(tracks))
		for i, t := range tracks {
			items[i] = trackItem(t)
		}
		return itemsLoadedMsg{screenID: id, items: items, err: err}
	}
}
