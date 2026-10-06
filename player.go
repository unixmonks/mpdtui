package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func clampVolume(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func action(text string, err error) tea.Msg {
	return actionResultMsg{text: text, err: err}
}

func cmdTogglePlayPause(c *Client, state string) tea.Cmd {
	return func() tea.Msg { return action("", c.TogglePlay(state)) }
}

func cmdNext(c *Client) tea.Cmd {
	return func() tea.Msg { return action("", c.Next()) }
}

func cmdPrev(c *Client) tea.Cmd {
	return func() tea.Msg { return action("", c.Prev()) }
}

func cmdVolume(c *Client, current float64, delta float64) tea.Cmd {
	return func() tea.Msg {
		if current < 0 {
			return action("", fmt.Errorf("MPD has no volume control for this output"))
		}
		v := clampVolume(current + delta)
		return action(fmt.Sprintf("volume %.0f%%", v), c.SetVolume(v))
	}
}

func cmdToggleMute(c *Client, muted bool, volume float64) tea.Cmd {
	return func() tea.Msg {
		if volume < 0 {
			return action("", fmt.Errorf("MPD has no volume control for this output"))
		}
		return action("", c.SetMute(!muted, volume))
	}
}

func cmdToggleShuffle(c *Client, on bool) tea.Cmd {
	return func() tea.Msg {
		next := !on
		text := "shuffle off"
		if next {
			text = "shuffle on"
		}
		return action(text, c.SetShuffle(next))
	}
}

func nextRepeatMode(mode string) string {
	switch mode {
	case "off", "":
		return "all"
	case "all":
		return "one"
	default:
		return "off"
	}
}

func cmdCycleRepeat(c *Client, mode string) tea.Cmd {
	return func() tea.Msg {
		next := nextRepeatMode(mode)
		return action("repeat "+next, c.SetRepeat(next))
	}
}

func cmdEnqueueTrack(c *Client, t Track) tea.Cmd {
	return func() tea.Msg {
		return action(fmt.Sprintf("queued: %s — %s", t.Artist, t.Title), c.Enqueue(t.ID))
	}
}

func cmdPlayNow(c *Client, t Track) tea.Cmd {
	return func() tea.Msg {
		return action(fmt.Sprintf("playing: %s — %s", t.Artist, t.Title), c.PlayNow(t.ID))
	}
}

func trackIDs(tracks []Track) []string {
	ids := make([]string, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	return ids
}

// cmdPlayAlbum is Enter on an album item: replace the queue with the whole
// album, in track order, and start playing it from the top.
func cmdPlayAlbum(c *Client, albumID, label string) tea.Cmd {
	return func() tea.Msg {
		tracks, err := c.AlbumTracks(albumID)
		if err != nil {
			return action("", err)
		}
		err = c.ReplaceQueue(trackIDs(tracks), true)
		return action(fmt.Sprintf("playing %s (%d tracks)", label, len(tracks)), err)
	}
}

// cmdPlayTracksFrom is Enter on a track within a browsing screen (album
// tracks, genre tracks, playlist tracks): replace the queue with that track
// and everything listed under it, and start playing from it.
func cmdPlayTracksFrom(c *Client, ids []string, label string) tea.Cmd {
	return func() tea.Msg {
		err := c.ReplaceQueue(ids, true)
		return action(fmt.Sprintf("playing %s (%d tracks)", label, len(ids)), err)
	}
}

func cmdEnqueueAlbum(c *Client, albumID, label string) tea.Cmd {
	return func() tea.Msg {
		tracks, err := c.AlbumTracks(albumID)
		if err != nil {
			return action("", err)
		}
		err = c.Enqueue(trackIDs(tracks)...)
		return action(fmt.Sprintf("queued %d tracks from %s", len(tracks), label), err)
	}
}

func cmdEnqueueArtist(c *Client, artist string) tea.Cmd {
	return func() tea.Msg {
		tracks, err := c.ArtistTracks(artist)
		if err != nil {
			return action("", err)
		}
		err = c.Enqueue(trackIDs(tracks)...)
		return action(fmt.Sprintf("queued %d tracks by %s", len(tracks), artist), err)
	}
}

func cmdEnqueueGenre(c *Client, genre string) tea.Cmd {
	return func() tea.Msg {
		tracks, err := c.GenreTracks(genre)
		if err != nil {
			return action("", err)
		}
		err = c.Enqueue(trackIDs(tracks)...)
		return action(fmt.Sprintf("queued %d %s tracks", len(tracks), genre), err)
	}
}

func cmdEnqueuePlaylist(c *Client, name string) tea.Cmd {
	return func() tea.Msg {
		return action(fmt.Sprintf("queued playlist %s", name), c.LoadPlaylist(name))
	}
}

func cmdRemoveQueueID(c *Client, id int) tea.Cmd {
	return func() tea.Msg { return action("removed", c.RemoveQueueID(id)) }
}

func cmdMoveQueueID(c *Client, id, to int) tea.Cmd {
	return func() tea.Msg { return action("", c.MoveQueueID(id, to)) }
}

func cmdClearQueue(c *Client) tea.Cmd {
	return func() tea.Msg { return action("queue cleared", c.ClearQueue()) }
}

func cmdPlayQueueID(c *Client, id int) tea.Cmd {
	return func() tea.Msg { return action("", c.PlayQueueID(id)) }
}

func cmdAddToPlaylist(c *Client, name, uri string) tea.Cmd {
	return func() tea.Msg {
		return action(fmt.Sprintf("added to %q", name), c.AddToPlaylist(name, uri))
	}
}

func cmdRemoveFromPlaylist(c *Client, name string, pos int) tea.Cmd {
	return func() tea.Msg { return action("removed from playlist", c.RemoveFromPlaylist(name, pos)) }
}

func cmdUpdateDB(c *Client) tea.Cmd {
	return func() tea.Msg { return action("database update started", c.Update()) }
}

func cmdFetchStatus(c *Client) tea.Cmd {
	return func() tea.Msg {
		st, err := c.Status()
		return playerStatusMsg{status: st, err: err}
	}
}

func statusTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return statusTickMsg{} })
}

// --- footer rendering ---

// minTrackWidth/minBarWidth bound how far renderFooter will squeeze the
// track title and progress bar before it just lets the line overflow —
// below this a narrow terminal is going to clip something regardless.
const (
	minTrackWidth = 8
	minBarWidth   = 6
)

// flash, when set, is a short-lived action result or error (see
// Model.flash) shown in place of the now-playing line.
func renderFooter(st Status, width int, connected bool, flash string) string {
	conn := connectedStyle.Render("●")
	if !connected {
		conn = disconnectedStyle.Render("●")
	}

	// footerStyle pads 1 column on each side, so content must be sized to
	// width-2: sizing it to the full width made lipgloss wrap the 2-column
	// overflow onto an extra line, which pushed the tab bar and list title
	// off the top of the screen.
	inner := width - 2
	if inner < 1 {
		inner = 1
	}

	if flash != "" {
		return footerStyle.Width(width).Render(lipglossJoin(flash, conn, inner))
	}

	if st.Track == nil {
		state := st.State
		if state == "" {
			state = "idle"
		}
		label := footerLabelStyle.Render(fmt.Sprintf("♪ %s", strings.ToUpper(state)))
		if st.Error != "" {
			// A queue whose files MPD can't open fails every track and
			// stops; without this the footer just says STOPPED.
			label += " " + errorStyle.Render(st.Error)
		}
		line := lipglossJoin(label, conn, inner)
		return footerStyle.Width(width).Render(line)
	}

	icon := "▶"
	if st.State != "playing" {
		icon = "⏸"
	}
	track := fmt.Sprintf("%s %s — %s", icon, st.Track.Artist, st.Track.Title)
	trackStyle := playerTrackStyle
	if st.Error != "" {
		track = "✕ " + st.Error
		trackStyle = errorStyle
	}

	pos := formatDuration(st.PositionMS)
	dur := formatDuration(st.DurationMS)

	flags := ""
	if st.Shuffle {
		flags += " ⇄"
	}
	if st.Repeat != "" && st.Repeat != "off" {
		flags += " ⟳" + st.Repeat[:1]
	}
	if st.Consume {
		flags += " ✂"
	}
	if st.Muted {
		flags += " ✕"
	}
	if st.Updating {
		flags += " ↻"
	}

	vol := "vol n/a"
	if st.Volume >= 0 {
		vol = fmt.Sprintf("vol %.0f%%", st.Volume)
	}
	meta := fmt.Sprintf("%s / %s  %s%s", pos, dur, vol, flags)
	right := meta + "  " + conn

	// Everything but the bar is fixed width; the bar gets whatever room is
	// left, shrinking the track title first if the terminal is too narrow
	// for all of it. progressBar wraps its interior in brackets, so the
	// interior it's given is 2 columns less than the bar's total width.
	barTotal := inner - lipgloss.Width(track) - lipgloss.Width(right) - 2
	if barTotal < minBarWidth {
		overflow := minBarWidth - barTotal
		trackWidth := lipgloss.Width(track) - overflow
		if trackWidth < minTrackWidth {
			trackWidth = minTrackWidth
		}
		track = ansi.Truncate(track, trackWidth, "…")
		barTotal = inner - lipgloss.Width(track) - lipgloss.Width(right) - 2
		if barTotal < minBarWidth {
			barTotal = minBarWidth
		}
	}
	bar := progressBar(st.PositionMS, st.DurationMS, barTotal-2)

	line := trackStyle.Render(track) + " " + bar + " " + footerLabelStyle.Render(meta) + "  " + conn

	return footerStyle.Width(width).Render(line)
}

func progressBar(posMS, durMS, width int) string {
	if durMS <= 0 {
		durMS = 1
	}
	filled := width * posMS / durMS
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	bracket := footerLabelStyle
	return bracket.Render("[") +
		playerBarFilledStyle.Render(strings.Repeat("█", filled)) +
		playerBarEmptyStyle.Render(strings.Repeat("░", width-filled)) +
		bracket.Render("]")
}

// lipglossJoin pads `left` and right-aligns `right` within width, truncating
// left if the terminal is too narrow to fit both.
func lipglossJoin(left, right string, width int) string {
	room := width - lipgloss.Width(right) - 1
	if room < 0 {
		room = 0
	}
	if lipgloss.Width(left) > room {
		left = ansi.Truncate(left, room, "…")
	}
	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}
