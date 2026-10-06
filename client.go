package main

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The domain types below are what the rest of the TUI works with. MPD has
// no album or artist ids — everything is addressed by tag values or by
// file URI — so a Track's ID is its URI and an Album's ID is derived from
// its (AlbumArtist, Album) tag pair.

type Album struct {
	ID          string
	Name        string
	AlbumArtist string
	Year        int
	TrackCount  int // 0 when not known (the full album list skips counting)
}

type Track struct {
	ID          string // file URI, relative to MPD's music directory
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	AlbumID     string
	TrackNo     int
	DiscNo      int
	Year        int
	Genre       string
	DurationMS  int
	Codec       string
	SampleRate  int
	BitDepth    int
	Channels    int

	// Queue-only: position and stable song id within MPD's play queue.
	// QueueID is -1 for tracks that aren't queue entries.
	Pos     int
	QueueID int
}

// ArtKey identifies the cover art a track would share with its siblings —
// MPD's albumart command resolves per directory, so the directory is the
// natural cache key.
func (t Track) ArtKey() string { return path.Dir(t.ID) }

type Status struct {
	State      string // "playing", "paused", or "stopped"
	Track      *Track
	QueueIndex int
	PositionMS int
	DurationMS int
	Volume     float64 // -1 when MPD has no mixer
	Muted      bool
	Shuffle    bool
	Repeat     string // "off", "all", or "one"
	Consume    bool
	Updating   bool
	Error      string // MPD's last player error, e.g. a file it couldn't open
}

type SearchResult struct {
	Artists []string
	Albums  []Album
	Tracks  []Track
}

const albumIDSep = "\x1f"

func makeAlbumID(albumArtist, album string) string {
	return albumArtist + albumIDSep + album
}

func splitAlbumID(id string) (albumArtist, album string) {
	albumArtist, album, _ = strings.Cut(id, albumIDSep)
	return albumArtist, album
}

// --- response parsing ---

// leadingInt parses the leading run of digits in s — enough for tags like
// "3/12" (track of total) or "2014-05-01" (a date), returning 0 if none.
func leadingInt(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// parseSongs splits a flat song-list response into Tracks; each "file" key
// starts a new record.
func parseSongs(attrs []attr) []Track {
	var tracks []Track
	var cur *Track
	for _, a := range attrs {
		v := strings.TrimSpace(a.value)
		if a.key == "file" {
			tracks = append(tracks, Track{ID: a.value, QueueID: -1, Pos: -1})
			cur = &tracks[len(tracks)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch a.key {
		case "Title":
			cur.Title = v
		case "Artist":
			if cur.Artist == "" {
				cur.Artist = v
			}
		case "Album":
			cur.Album = v
		case "AlbumArtist":
			if cur.AlbumArtist == "" {
				cur.AlbumArtist = v
			}
		case "Track":
			cur.TrackNo = leadingInt(v)
		case "Disc":
			cur.DiscNo = leadingInt(v)
		case "Date":
			if cur.Year == 0 {
				cur.Year = leadingInt(v)
			}
		case "Genre":
			if cur.Genre == "" {
				cur.Genre = v
			}
		case "duration":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				cur.DurationMS = int(f * 1000)
			}
		case "Time":
			if cur.DurationMS == 0 {
				cur.DurationMS = leadingInt(v) * 1000
			}
		case "Format":
			// samplerate:bits:channels, e.g. 44100:16:2 (bits may be "f").
			parts := strings.Split(v, ":")
			if len(parts) == 3 {
				cur.SampleRate, _ = strconv.Atoi(parts[0])
				cur.BitDepth, _ = strconv.Atoi(parts[1])
				cur.Channels, _ = strconv.Atoi(parts[2])
			}
		case "Pos":
			cur.Pos, _ = strconv.Atoi(v)
		case "Id":
			cur.QueueID, _ = strconv.Atoi(v)
		}
	}
	for i := range tracks {
		t := &tracks[i]
		if t.Title == "" {
			base := path.Base(t.ID)
			t.Title = strings.TrimSuffix(base, path.Ext(base))
		}
		// MPD falls back from AlbumArtist to Artist when filtering, so the
		// album id has to make the same substitution to round-trip.
		aa := t.AlbumArtist
		if aa == "" {
			aa = t.Artist
		}
		t.AlbumID = makeAlbumID(aa, t.Album)
		if ext := path.Ext(t.ID); ext != "" && !strings.Contains(t.ID, "://") {
			t.Codec = strings.ToLower(ext[1:])
		}
	}
	return tracks
}

// values collects every value for key, skipping empties.
func values(attrs []attr, key string) []string {
	var out []string
	for _, a := range attrs {
		if a.key == key && strings.TrimSpace(a.value) != "" {
			out = append(out, a.value)
		}
	}
	return out
}

// parseAlbumList reads a "list album ... group date [group albumartist]"
// response. MPD only re-emits an outer group tag when it changes, so the
// most recent value of each is carried forward to each Album line.
func parseAlbumList(attrs []attr, fixedArtist string) []Album {
	var albums []Album
	seen := map[string]bool{}
	artist, date := fixedArtist, ""
	for _, a := range attrs {
		switch a.key {
		case "AlbumArtist":
			artist, date = a.value, ""
		case "Date":
			date = a.value
		case "Album":
			if strings.TrimSpace(a.value) == "" {
				continue
			}
			id := makeAlbumID(artist, a.value)
			if seen[id] {
				// The same album split across differing Date tags — keep
				// the first (earliest-sorting) one.
				continue
			}
			seen[id] = true
			albums = append(albums, Album{ID: id, Name: a.value, AlbumArtist: artist, Year: leadingInt(date)})
		}
	}
	return albums
}

func sortAlbumTracks(tracks []Track) {
	sort.SliceStable(tracks, func(i, j int) bool {
		a, b := tracks[i], tracks[j]
		if a.DiscNo != b.DiscNo {
			return a.DiscNo < b.DiscNo
		}
		if a.TrackNo != b.TrackNo {
			return a.TrackNo < b.TrackNo
		}
		return a.ID < b.ID
	})
}

// sortLibraryTracks orders tracks spanning many albums: by album artist,
// then album, then disc/track — how a browsing list reads naturally.
func sortLibraryTracks(tracks []Track) {
	sort.SliceStable(tracks, func(i, j int) bool {
		a, b := tracks[i], tracks[j]
		if x, y := strings.ToLower(a.AlbumID), strings.ToLower(b.AlbumID); x != y {
			return x < y
		}
		if a.DiscNo != b.DiscNo {
			return a.DiscNo < b.DiscNo
		}
		if a.TrackNo != b.TrackNo {
			return a.TrackNo < b.TrackNo
		}
		return a.ID < b.ID
	})
}

func sortFold(s []string) {
	sort.SliceStable(s, func(i, j int) bool { return strings.ToLower(s[i]) < strings.ToLower(s[j]) })
}

// --- library ---

func (c *Client) Artists() ([]string, error) {
	attrs, err := c.cmd("list", "albumartist")
	if err != nil {
		return nil, err
	}
	artists := values(attrs, "AlbumArtist")
	sortFold(artists)
	return artists, nil
}

func (c *Client) Genres() ([]string, error) {
	attrs, err := c.cmd("list", "genre")
	if err != nil {
		return nil, err
	}
	genres := values(attrs, "Genre")
	sortFold(genres)
	return genres, nil
}

func (c *Client) GenreTracks(genre string) ([]Track, error) {
	attrs, err := c.cmd("find", filterEq("Genre", genre))
	if err != nil {
		return nil, err
	}
	tracks := parseSongs(attrs)
	sortLibraryTracks(tracks)
	return tracks, nil
}

// Albums lists every album in the library, sorted by name. Track counts
// are left at 0 — counting every album individually would cost one round
// trip each.
func (c *Client) Albums() ([]Album, error) {
	attrs, err := c.cmd("list", "album", "group", "date", "group", "albumartist")
	if err != nil {
		return nil, err
	}
	albums := parseAlbumList(attrs, "")
	sort.SliceStable(albums, func(i, j int) bool {
		return strings.ToLower(albums[i].Name) < strings.ToLower(albums[j].Name)
	})
	return albums, nil
}

func (c *Client) AlbumsByArtist(artist string) ([]Album, error) {
	filter := filterEq("AlbumArtist", artist)
	attrs, err := c.cmd("list", "album", filter, "group", "date")
	if err != nil {
		return nil, err
	}
	albums := parseAlbumList(attrs, artist)

	// One extra round trip gets every album's track count at once.
	if counts, err := c.cmd("count", filter, "group", "album"); err == nil {
		n := map[string]int{}
		var cur string
		for _, a := range counts {
			switch a.key {
			case "Album":
				cur = a.value
			case "songs":
				n[cur], _ = strconv.Atoi(a.value)
			}
		}
		for i := range albums {
			albums[i].TrackCount = n[albums[i].Name]
		}
	}
	return albums, nil
}

func (c *Client) AlbumTracks(albumID string) ([]Track, error) {
	artist, album := splitAlbumID(albumID)
	attrs, err := c.cmd("find", filterAnd(filterEq("AlbumArtist", artist), filterEq("Album", album)))
	if err != nil {
		return nil, err
	}
	tracks := parseSongs(attrs)
	sortAlbumTracks(tracks)
	return tracks, nil
}

// ArtistTracks is every track credited to artist as album artist, album by
// album in track order.
func (c *Client) ArtistTracks(artist string) ([]Track, error) {
	attrs, err := c.cmd("find", filterEq("AlbumArtist", artist))
	if err != nil {
		return nil, err
	}
	tracks := parseSongs(attrs)
	sortLibraryTracks(tracks)
	return tracks, nil
}

// searchTrackLimit caps how many tracks one search returns; the overlay
// re-ranks client-side and anything past this is too vague a query anyway.
const searchTrackLimit = 300

// Search matches query case-insensitively against artists, albums, and any
// track tag. Artists and albums are matched client-side against MPD's tag
// lists (cheap even for large libraries); tracks use MPD's own "search".
func (c *Client) Search(query string) (SearchResult, error) {
	var result SearchResult
	q := strings.ToLower(query)

	artists, err := c.Artists()
	if err != nil {
		return result, err
	}
	for _, a := range artists {
		if strings.Contains(strings.ToLower(a), q) {
			result.Artists = append(result.Artists, a)
		}
	}

	albums, err := c.Albums()
	if err != nil {
		return result, err
	}
	for _, a := range albums {
		if strings.Contains(strings.ToLower(a.Name), q) || strings.Contains(strings.ToLower(a.AlbumArtist), q) {
			result.Albums = append(result.Albums, a)
		}
	}

	attrs, err := c.cmd("search", "any", query, "window", fmt.Sprintf("0:%d", searchTrackLimit))
	if err != nil {
		return result, err
	}
	result.Tracks = parseSongs(attrs)
	return result, nil
}

// Art returns the raw cover image bytes for uri: a cover file next to it
// (albumart) if there is one, otherwise a picture embedded in the file
// itself (readpicture).
func (c *Client) Art(uri string) ([]byte, error) {
	data, err := c.binary("albumart", uri)
	if err == nil && len(data) > 0 {
		return data, nil
	}
	data, err2 := c.binary("readpicture", uri)
	if err2 == nil && len(data) > 0 {
		return data, nil
	}
	if err2 != nil {
		return nil, err2
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no art for %s", uri)
}

// --- player ---

func (c *Client) Status() (Status, error) {
	var st Status
	var statusAttrs, songAttrs []attr
	err := c.run(func(cn *conn) error {
		var err error
		if statusAttrs, err = cn.cmd("status"); err != nil {
			return err
		}
		songAttrs, err = cn.cmd("currentsong")
		return err
	})
	if err != nil {
		return st, err
	}

	st.QueueIndex = -1
	st.Volume = -1
	var repeat, single bool
	for _, a := range statusAttrs {
		switch a.key {
		case "state":
			switch a.value {
			case "play":
				st.State = "playing"
			case "pause":
				st.State = "paused"
			default:
				st.State = "stopped"
			}
		case "volume":
			st.Volume, _ = strconv.ParseFloat(a.value, 64)
		case "random":
			st.Shuffle = a.value == "1"
		case "repeat":
			repeat = a.value == "1"
		case "single":
			single = a.value != "0"
		case "consume":
			st.Consume = a.value != "0"
		case "song":
			st.QueueIndex, _ = strconv.Atoi(a.value)
		case "elapsed":
			f, _ := strconv.ParseFloat(a.value, 64)
			st.PositionMS = int(f * 1000)
		case "duration":
			f, _ := strconv.ParseFloat(a.value, 64)
			st.DurationMS = int(f * 1000)
		case "updating_db":
			st.Updating = true
		case "error":
			st.Error = a.value
		}
	}
	switch {
	case single:
		st.Repeat = "one"
	case repeat:
		st.Repeat = "all"
	default:
		st.Repeat = "off"
	}

	if songs := parseSongs(songAttrs); len(songs) > 0 {
		st.Track = &songs[0]
		if st.DurationMS == 0 {
			st.DurationMS = st.Track.DurationMS
		}
	}

	c.mu.Lock()
	if c.muted && st.Volume > 0 {
		c.muted = false // someone else turned the volume back up
	}
	st.Muted = c.muted
	c.mu.Unlock()
	return st, nil
}

// TogglePlay pauses when playing, resumes when paused, and starts playback
// from the current (or first) queue entry when stopped.
func (c *Client) TogglePlay(state string) error {
	var err error
	switch state {
	case "playing":
		_, err = c.cmd("pause", "1")
	case "paused":
		_, err = c.cmd("pause", "0")
	default:
		_, err = c.cmd("play")
	}
	return err
}

func (c *Client) Next() error { _, err := c.cmd("next"); return err }
func (c *Client) Prev() error { _, err := c.cmd("previous"); return err }

func (c *Client) SetVolume(v float64) error {
	_, err := c.cmd("setvol", strconv.Itoa(int(v+0.5)))
	if err == nil {
		c.mu.Lock()
		c.muted = false
		c.mu.Unlock()
	}
	return err
}

// SetMute emulates muting, which MPD lacks: mute remembers the current
// volume and drops to 0; unmute restores it.
func (c *Client) SetMute(mute bool, current float64) error {
	c.mu.Lock()
	restore := c.preMuteVolume
	c.mu.Unlock()

	if mute {
		if _, err := c.cmd("setvol", "0"); err != nil {
			return err
		}
		c.mu.Lock()
		c.preMuteVolume = int(current + 0.5)
		c.muted = true
		c.mu.Unlock()
		return nil
	}
	if restore <= 0 {
		restore = 50
	}
	return c.SetVolume(float64(restore))
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (c *Client) SetShuffle(on bool) error {
	_, err := c.cmd("random", boolArg(on))
	return err
}

// SetRepeat maps the TUI's three-way repeat onto MPD's repeat+single pair:
// "all" loops the queue, "one" loops the current track.
func (c *Client) SetRepeat(mode string) error {
	repeat, single := "0", "0"
	switch mode {
	case "all":
		repeat = "1"
	case "one":
		repeat, single = "1", "1"
	}
	return c.commandList([]string{
		formatCommand("repeat", repeat),
		formatCommand("single", single),
	})
}

// --- queue ---

func (c *Client) Queue() ([]Track, error) {
	attrs, err := c.cmd("playlistinfo")
	if err != nil {
		return nil, err
	}
	return parseSongs(attrs), nil
}

func (c *Client) Enqueue(uris ...string) error {
	cmds := make([]string, len(uris))
	for i, u := range uris {
		cmds[i] = formatCommand("add", u)
	}
	return c.commandList(cmds)
}

// PlayNow appends a track to the queue and jumps playback straight to it.
func (c *Client) PlayNow(uri string) error {
	attrs, err := c.cmd("addid", uri)
	if err != nil {
		return err
	}
	for _, a := range attrs {
		if a.key == "Id" {
			_, err = c.cmd("playid", a.value)
			return err
		}
	}
	return fmt.Errorf("addid %s: no id returned", uri)
}

// ReplaceQueue clears the queue and fills it with uris, in order, in a
// single batch. If play is set, playback starts at the first one.
func (c *Client) ReplaceQueue(uris []string, play bool) error {
	cmds := make([]string, 0, len(uris)+2)
	cmds = append(cmds, formatCommand("clear"))
	for _, u := range uris {
		cmds = append(cmds, formatCommand("add", u))
	}
	if play && len(uris) > 0 {
		cmds = append(cmds, formatCommand("play", "0"))
	}
	return c.commandList(cmds)
}

func (c *Client) PlayQueueID(id int) error {
	_, err := c.cmd("playid", strconv.Itoa(id))
	return err
}

func (c *Client) ClearQueue() error {
	_, err := c.cmd("clear")
	return err
}

func (c *Client) RemoveQueueID(id int) error {
	_, err := c.cmd("deleteid", strconv.Itoa(id))
	return err
}

func (c *Client) MoveQueueID(id, to int) error {
	_, err := c.cmd("moveid", strconv.Itoa(id), strconv.Itoa(to))
	return err
}

// --- stored playlists ---

func (c *Client) Playlists() ([]string, error) {
	attrs, err := c.cmd("listplaylists")
	if err != nil {
		return nil, err
	}
	names := values(attrs, "playlist")
	sortFold(names)
	return names, nil
}

// Playlist returns a stored playlist's tracks, with Pos set to each one's
// index within the playlist (what playlistdelete addresses by).
func (c *Client) Playlist(name string) ([]Track, error) {
	attrs, err := c.cmd("listplaylistinfo", name)
	if err != nil {
		return nil, err
	}
	tracks := parseSongs(attrs)
	for i := range tracks {
		tracks[i].Pos = i
	}
	return tracks, nil
}

// LoadPlaylist appends a stored playlist to the queue.
func (c *Client) LoadPlaylist(name string) error {
	_, err := c.cmd("load", name)
	return err
}

// PlayPlaylist replaces the queue with a stored playlist and starts
// playing it from the top, in a single batch.
func (c *Client) PlayPlaylist(name string) error {
	return c.commandList([]string{
		formatCommand("clear"),
		formatCommand("load", name),
		formatCommand("play", "0"),
	})
}

// AddToPlaylist appends uri to a stored playlist, creating it if needed.
func (c *Client) AddToPlaylist(name, uri string) error {
	_, err := c.cmd("playlistadd", name, uri)
	return err
}

func (c *Client) RemoveFromPlaylist(name string, pos int) error {
	_, err := c.cmd("playlistdelete", name, strconv.Itoa(pos))
	return err
}

// Update asks MPD to rescan its music directory.
func (c *Client) Update() error {
	_, err := c.cmd("update")
	return err
}
