package main

import (
	"os"
	"testing"
)

func TestParseAck(t *testing.T) {
	e := parseAck("ACK [50@0] {play} No such song")
	if e.Code != 50 || e.Command != "play" || e.Message != "No such song" {
		t.Fatalf("got %+v", e)
	}
}

func TestParseAlbumListCarriesGroups(t *testing.T) {
	attrs := []attr{
		{"AlbumArtist", "A Perfect Circle"}, {"Date", "2000"}, {"Album", "Mer De Noms"},
		{"Date", "2003"}, {"Album", "Thirteenth Step"},
		{"AlbumArtist", "Oasis"}, {"Date", "1994-08-29"}, {"Album", "Definitely Maybe"},
		{"Date", "1995"}, {"Album", "Definitely Maybe"}, // split by date: deduped
		{"Album", ""},
	}
	got := parseAlbumList(attrs, "")
	if len(got) != 3 {
		t.Fatalf("want 3 albums, got %+v", got)
	}
	if got[1].AlbumArtist != "A Perfect Circle" || got[1].Year != 2003 {
		t.Errorf("group values not carried forward: %+v", got[1])
	}
	if got[2].Year != 1994 {
		t.Errorf("date parse: %+v", got[2])
	}
}

func TestParseSongs(t *testing.T) {
	attrs := []attr{
		{"file", "Oasis/x/03. Wonderwall.flac"}, {"Format", "48000:24:2"}, {"Track", "3/12"},
		{"Title", "Wonderwall "}, {"Artist", "Oasis"}, {"Album", "Morning Glory"},
		{"duration", "258.732"}, {"Pos", "4"}, {"Id", "17"},
		{"file", "loose/untitled.mp3"}, {"Artist", "Someone"},
	}
	got := parseSongs(attrs)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	a := got[0]
	if a.Title != "Wonderwall" || a.TrackNo != 3 || a.DurationMS != 258732 || a.Pos != 4 || a.QueueID != 17 ||
		a.SampleRate != 48000 || a.BitDepth != 24 || a.Codec != "flac" || a.AlbumID != makeAlbumID("Oasis", "Morning Glory") {
		t.Errorf("got %+v", a)
	}
	if got[1].Title != "untitled" || got[1].QueueID != -1 {
		t.Errorf("got %+v", got[1])
	}
}

func TestFilterEscaping(t *testing.T) {
	got := quoteArg(filterEq("Album", `It's "Q"`))
	want := `"(Album == \"It's \\\"Q\\\"\")"`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestMPDAddress(t *testing.T) {
	cases := []struct{ host, port, addr, pw string }{
		{"", "", "localhost:6600", ""},
		{"secret@box", "6601", "box:6601", "secret"},
		{"/run/mpd/socket", "", "/run/mpd/socket", ""},
		{"@mpd", "", "@mpd", ""},
		{"pw@@mpd", "", "@mpd", "pw"},
	}
	for _, c := range cases {
		addr, pw := mpdAddress(c.host, c.port)
		if addr != c.addr || pw != c.pw {
			t.Errorf("mpdAddress(%q,%q) = %q,%q; want %q,%q", c.host, c.port, addr, pw, c.addr, c.pw)
		}
	}
}

// TestLiveServer runs against a real MPD when MPDTUI_TEST_ADDR is set. It
// clears and rewrites that server's queue and creates/deletes a stored
// playlist, so point it at a scratch instance.
func TestLiveServer(t *testing.T) {
	addr := os.Getenv("MPDTUI_TEST_ADDR")
	if addr == "" {
		t.Skip("MPDTUI_TEST_ADDR not set")
	}
	c := NewClient(addr, "")

	artists, err := c.Artists()
	if err != nil || len(artists) == 0 {
		t.Fatalf("Artists: %v (%d)", err, len(artists))
	}
	albums, err := c.AlbumsByArtist(artists[len(artists)/2])
	if err != nil || len(albums) == 0 {
		t.Fatalf("AlbumsByArtist(%q): %v (%d)", artists[len(artists)/2], err, len(albums))
	}
	tracks, err := c.AlbumTracks(albums[0].ID)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("AlbumTracks(%q): %v (%d)", albums[0].ID, err, len(tracks))
	}
	if albums[0].TrackCount != len(tracks) {
		t.Errorf("TrackCount %d != %d tracks", albums[0].TrackCount, len(tracks))
	}

	// Every album in the full list must resolve back to tracks — this is
	// what proves album ids round-trip through MPD's AlbumArtist fallback.
	all, err := c.Albums()
	if err != nil {
		t.Fatal(err)
	}
	empty := 0
	for _, a := range all {
		ts, err := c.AlbumTracks(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(ts) == 0 {
			empty++
			if empty < 5 {
				t.Errorf("album %q by %q has no tracks", a.Name, a.AlbumArtist)
			}
		}
	}
	t.Logf("%d albums, %d unresolvable", len(all), empty)

	if err := c.ReplaceQueue(trackIDs(tracks), true); err != nil {
		t.Fatal(err)
	}
	q, err := c.Queue()
	if err != nil || len(q) != len(tracks) {
		t.Fatalf("Queue: %v (%d)", err, len(q))
	}
	st, err := c.Status()
	if err != nil || st.State != "playing" || st.Track == nil || st.QueueIndex != 0 {
		t.Fatalf("Status: %v %+v", err, st)
	}
	if len(q) > 1 {
		if err := c.MoveQueueID(q[0].QueueID, 1); err != nil {
			t.Fatal(err)
		}
		if err := c.RemoveQueueID(q[0].QueueID); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.SetRepeat("one"); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.Status(); st.Repeat != "one" {
		t.Errorf("repeat = %q", st.Repeat)
	}
	c.SetRepeat("off")

	if data, err := c.Art(tracks[0].ID); err != nil {
		t.Logf("no art for %s: %v", tracks[0].ID, err)
	} else {
		t.Logf("art: %d bytes", len(data))
	}

	res, err := c.Search("the")
	if err != nil || len(res.Tracks) == 0 {
		t.Fatalf("Search: %v %d", err, len(res.Tracks))
	}

	const pl = "mpdtui-test"
	if err := c.AddToPlaylist(pl, tracks[0].ID); err != nil {
		t.Fatal(err)
	}
	pt, err := c.Playlist(pl)
	if err != nil || len(pt) != 1 {
		t.Fatalf("Playlist: %v %d", err, len(pt))
	}
	if err := c.RemoveFromPlaylist(pl, 0); err != nil {
		t.Fatal(err)
	}
	c.cmd("rm", pl)
	c.cmd("stop")
}
