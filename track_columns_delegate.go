package main

import (
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// trackColumnsDelegate renders each row as aligned Artist / Track / Album /
// Duration columns instead of the title+description pair the other screens
// use. It backs any track list that mixes artists/albums — the Queue,
// genre tracks, and playlist tracks — where Artist and Album both carry
// real information rather than being implied by a breadcrumb. As the
// terminal narrows, the gaps between columns close up first; once they're
// already at their floor, Artist gives up width next, then Album, and
// Track — the field most worth reading in full — only shrinks as a last
// resort. Track is also the one that grows to soak up any extra width on
// a wide terminal.
//
// playingIndex points at the Model's shared "currently playing" index (see
// Model.playingIndex) rather than being copied in at construction time, so
// this delegate keeps seeing live updates as playback advances without
// needing the list to be rebuilt. It's only meaningful for the Queue
// screen; other screens leave it nil and just never show the marker.
type trackColumnsDelegate struct {
	playingIndex *int

	// focus marks a delegate used only for rendering a Miller column's
	// render-only copy when that column is the one actually being
	// navigated right now (see renderBody in model.go): its highlighted
	// row is colored with the theme's lighter focus tint instead of full
	// accent, so it reads as visually distinct from the parent/preview
	// columns' plain-accent selections either side of it.
	focus bool
}

const (
	trackColDurWidth = 5 // fits up to "99:59"
	trackColGapMax   = 2 // comfortable spacing between columns
	trackColGapMin   = 1 // spacing never squeezes past this, even before text does
	trackColMarkerW  = 2 // "▶ " / "  " now-playing marker

	trackColArtistBase = 18
	trackColAlbumBase  = 20
	trackColTrackBase  = 30

	trackColArtistMin = 8
	trackColAlbumMin  = 8
	trackColTrackMin  = 6
)

func (trackColumnsDelegate) Height() int                         { return 1 }
func (trackColumnsDelegate) Spacing() int                        { return 0 }
func (trackColumnsDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// trackColumnWidths splits the available width into artist/track/album
// columns plus the gap between each (duration is fixed). Squeezing the
// gaps down to trackColGapMin is tried before shrinking any column's text;
// once gaps are already minimal, Artist gives way first, then Album, and
// Track last.
func trackColumnWidths(avail int) (artistW, trackW, albumW, gap int) {
	avail -= trackColDurWidth
	if avail < 0 {
		avail = 0
	}

	artistW, trackW, albumW = trackColArtistBase, trackColTrackBase, trackColAlbumBase
	gap = trackColGapMax
	for gap > trackColGapMin && artistW+trackW+albumW+gap*3 > avail {
		gap--
	}

	room := avail - gap*3
	if room < 0 {
		room = 0
	}
	total := artistW + trackW + albumW

	if extra := room - total; extra > 0 {
		// Extra room beyond everyone's comfortable size goes to the track
		// title — the field most worth reading in full.
		trackW += extra
		return artistW, trackW, albumW, gap
	}

	deficit := total - room
	shrink := min(deficit, max(artistW-trackColArtistMin, 0))
	artistW -= shrink
	deficit -= shrink

	if deficit > 0 {
		shrink = min(deficit, max(albumW-trackColAlbumMin, 0))
		albumW -= shrink
		deficit -= shrink
	}

	if deficit > 0 {
		shrink = min(deficit, max(trackW-trackColTrackMin, 0))
		trackW -= shrink
	}
	if trackW < 0 {
		trackW = 0
	}
	return artistW, trackW, albumW, gap
}

// padCell truncates s to fit width w (adding an ellipsis if it's cut) and
// pads it out to exactly w cells so columns line up down the list.
func padCell(s string, w int, alignRight bool) string {
	if w <= 0 {
		return ""
	}
	tail := "…"
	if alignRight {
		tail = ""
	}
	s = ansi.Truncate(s, w, tail)
	if pad := w - ansi.StringWidth(s); pad > 0 {
		if alignRight {
			s = strings.Repeat(" ", pad) + s
		} else {
			s += strings.Repeat(" ", pad)
		}
	}
	return s
}

func (d trackColumnsDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(item)
	if !ok || it.track == nil {
		return
	}
	track := it.track

	// The now-playing marker only ever applies to the Queue screen (the only
	// caller that passes a non-nil playingIndex); other screens skip the
	// column entirely rather than leaving a permanent blank gutter for an
	// arrow that can never appear there.
	markerW := 0
	if d.playingIndex != nil {
		markerW = trackColMarkerW
	}

	styles := list.NewDefaultItemStyles()
	textwidth := m.Width() - styles.NormalTitle.GetPaddingLeft() - styles.NormalTitle.GetPaddingRight() - markerW
	if textwidth <= 0 {
		return
	}

	artistW, trackW, albumW, gapW := trackColumnWidths(textwidth)

	isPlaying := d.playingIndex != nil && *d.playingIndex == index
	marker := ""
	if markerW > 0 {
		marker = "  "
		if isPlaying {
			marker = "▶ "
		}
	}

	gap := strings.Repeat(" ", gapW)
	line := marker + padCell(track.Artist, artistW, false) + gap +
		padCell(track.Title, trackW, false) + gap +
		padCell(track.Album, albumW, false) + gap +
		padCell(formatDuration(track.DurationMS), trackColDurWidth, true)

	switch {
	case m.FilterState() == list.Filtering && m.FilterValue() == "":
		line = styles.DimmedTitle.Render(line)
	case index == m.Index() && m.FilterState() != list.Filtering:
		c := accent
		if d.focus {
			c = focus
		}
		line = styles.SelectedTitle.Foreground(c).BorderForeground(c).Render(line)
	case isPlaying:
		line = styles.NormalTitle.Foreground(accent).Bold(true).Render(line)
	default:
		line = styles.NormalTitle.Render(line)
	}

	io.WriteString(w, line)
}
