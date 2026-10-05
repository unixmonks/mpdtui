package main

// itemsLoadedMsg carries a fully-loaded item set for a screen (everything
// except the Queue, which has its own message type below).
type itemsLoadedMsg struct {
	screenID int
	items    []item
	err      error
}

type queueLoadedMsg struct {
	screenID int
	items    []item
	err      error
}

// playerStatusMsg carries a fresh status poll (see statusTickMsg) and drives
// the persistent footer.
type playerStatusMsg struct {
	status Status
	err    error
}

// statusTickMsg fires once a second to re-poll MPD's status, which is what
// keeps the footer's elapsed time moving — MPD's idle only reports state
// changes, not progress.
type statusTickMsg struct{}

// mpdChangedMsg is one batch of subsystem names ("player", "playlist",
// "stored_playlist", "mixer", ...) reported by MPD's idle command.
type mpdChangedMsg []string

// mpdConnectedMsg fires each time the idle watcher (re)connects, so state
// that may have changed while disconnected gets reloaded.
type mpdConnectedMsg struct{}

// connLostMsg marks the footer's connection indicator as disconnected; it
// flips back once a status poll succeeds again.
type connLostMsg struct{}

// actionResultMsg reports the outcome of a fire-and-forget action (enqueue,
// remove, playlist edit, transport control) so it can be surfaced as a
// transient status message on the active screen's list. Queue and status
// refreshes aren't requested here: MPD's idle notifications trigger them.
type actionResultMsg struct {
	text string
	err  error
}
