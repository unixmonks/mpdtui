package main

import "fmt"

type itemKind int

const (
	itemArtist itemKind = iota
	itemAlbum
	itemGenre
	itemPlaylist
	itemTrack
)

// item is a single list.Item used across every screen in the app. Using one
// concrete type instead of per-kind types keeps the delegate and the
// select/enqueue/remove logic in model.go simple: it always has an id to
// act on and, for tracks, the full Track to enqueue.
type item struct {
	kind  itemKind
	id    string // artist name / album id / genre name / playlist name / track URI
	title string
	desc  string
	track *Track
	album *Album
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

func artistItem(name string) item {
	return item{kind: itemArtist, id: name, title: name}
}

func genreItem(name string) item {
	return item{kind: itemGenre, id: name, title: name}
}

func playlistItem(name string) item {
	return item{kind: itemPlaylist, id: name, title: name}
}

func albumItem(a Album) item {
	desc := a.AlbumArtist
	if a.Year > 0 {
		desc = fmt.Sprintf("%d · %s", a.Year, desc)
	}
	if a.TrackCount > 0 {
		desc = fmt.Sprintf("%s · %d tracks", desc, a.TrackCount)
	}
	return item{
		kind:  itemAlbum,
		id:    a.ID,
		title: a.Name,
		desc:  desc,
		album: &a,
	}
}

func trackFormat(t Track) string {
	if t.SampleRate > 0 && t.BitDepth > 0 {
		return fmt.Sprintf("%s %gkHz/%dbit", t.Codec, float64(t.SampleRate)/1000, t.BitDepth)
	}
	return t.Codec
}

func trackItem(t Track) item {
	return item{
		kind:  itemTrack,
		id:    t.ID,
		title: t.Title,
		desc:  fmt.Sprintf("%s — %s  %s  %s", t.Artist, t.Album, formatDuration(t.DurationMS), trackFormat(t)),
		track: &t,
	}
}

// albumTrackItem is trackItem's counterpart for the album-tracks screen:
// artist/album/duration/codec are all implied or peripheral there, so the
// title is just the track number, artist, and title. showDisc adds a disc
// prefix ("1-01") for multi-disc albums, where track numbers alone repeat
// per disc.
func albumTrackItem(t Track, showDisc bool) item {
	num := fmt.Sprintf("%02d", t.TrackNo)
	if showDisc {
		num = fmt.Sprintf("%d-%02d", t.DiscNo, t.TrackNo)
	}
	return item{
		kind:  itemTrack,
		id:    t.ID,
		title: fmt.Sprintf("%s %s - %s", num, t.Artist, t.Title),
		track: &t,
	}
}

func formatDuration(ms int) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
