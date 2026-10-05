package main

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
)

// globalKeyMap are the app-wide vim-style bindings handled by the root model
// before (or instead of) forwarding a key to the active screen's list.
type globalKeyMap struct {
	tab1, tab2, tab3, tab4, tab5 key.Binding
	nextTab                      key.Binding
	prevTab                      key.Binding
	back                         key.Binding
	into                         key.Binding
	quit                         key.Binding
	forceQuit                    key.Binding
	selectItem                   key.Binding
	playPause                    key.Binding
	next                         key.Binding
	prev                         key.Binding
	volUp                        key.Binding
	volDown                      key.Binding
	mute                         key.Binding
	shuffle                      key.Binding
	repeat                       key.Binding
	enqueue                      key.Binding
	addToPlaylist                key.Binding
	remove                       key.Binding
	clearQueue                   key.Binding
	moveDown                     key.Binding
	moveUp                       key.Binding
	toggleCover                  key.Binding
	help                         key.Binding
	search                       key.Binding
	updateDB                     key.Binding
	toggleTabBar                 key.Binding
	cycleTheme                   key.Binding
}

// keys and listKeys are populated at startup by applyKeyConfig, from
// DefaultKeyConfig merged with any user overrides (see keyconfig.go). Until
// then they're zero-value bindings that match nothing.
var keys globalKeyMap
var listKeys list.KeyMap

// applyKeyConfig builds keys and listKeys from cfg. It must run before the
// program starts reading input.
func applyKeyConfig(cfg KeyConfig) {
	keys = globalKeyMap{
		tab1: bindKeys(cfg.Tab1, "", "artists"),
		tab2: bindKeys(cfg.Tab2, "", "albums"),
		tab3: bindKeys(cfg.Tab3, "", "genres"),
		tab4: bindKeys(cfg.Tab4, "", "queue"),
		tab5: bindKeys(cfg.Tab5, "", "playlists"),

		nextTab: bindKeys(cfg.NextTab, "", "next menu"),
		prevTab: bindKeys(cfg.PrevTab, "", "prev menu"),

		back:      bindKeys(cfg.Back, "", "back"),
		into:      bindKeys(cfg.Into, "", "into"),
		quit:      bindKeys(cfg.Quit, "", "quit"),
		forceQuit: bindKeys(cfg.ForceQuit, "", "force quit"),

		selectItem: bindKeys(cfg.SelectItem, "", "play"),

		playPause: bindKeys(cfg.PlayPause, "", "play/pause"),
		next:      bindKeys(cfg.Next, "", "next track"),
		prev:      bindKeys(cfg.Prev, "", "prev track"),
		volUp:     bindKeys(cfg.VolUp, "", "vol up"),
		volDown:   bindKeys(cfg.VolDown, "", "vol down"),
		mute:      bindKeys(cfg.Mute, "", "mute"),
		shuffle:   bindKeys(cfg.Shuffle, "", "shuffle"),
		repeat:    bindKeys(cfg.Repeat, "", "repeat"),

		enqueue:       bindKeys(cfg.Enqueue, "", "add to queue"),
		addToPlaylist: bindKeys(cfg.AddToPlaylist, "", "add to playlist"),
		// remove is a double-tap (dd) gesture — the binding itself only
		// needs the single configured key, but the help label calls that out.
		remove:     bindKeys(cfg.Remove, removeLabel(cfg.Remove), "remove"),
		clearQueue: bindKeys(cfg.ClearQueue, "", "clear queue"),
		moveDown:   bindKeys(cfg.MoveDown, "", "move down"),
		moveUp:     bindKeys(cfg.MoveUp, "", "move up"),

		toggleCover: bindKeys(cfg.ToggleCover, "", "toggle cover"),

		help:         bindKeys(cfg.Help, "", "help"),
		search:       bindKeys(cfg.Search, "", "search"),
		updateDB:     bindKeys(cfg.UpdateDB, "", "update database"),
		toggleTabBar: bindKeys(cfg.ToggleTabBar, "", "toggle tab bar"),
		cycleTheme:   bindKeys(cfg.CycleTheme, "", "theme"),
	}

	lm := list.DefaultKeyMap()
	lm.CursorUp = bindKeys(cfg.CursorUp, "", "up")
	lm.CursorDown = bindKeys(cfg.CursorDown, "", "down")
	lm.PrevPage = bindKeys(cfg.PrevPage, "", "prev page")
	lm.NextPage = bindKeys(cfg.NextPage, "", "next page")
	lm.GoToStart = bindKeys(cfg.GoToStart, "", "go to start")
	lm.GoToEnd = bindKeys(cfg.GoToEnd, "", "go to end")
	lm.Filter = bindKeys(cfg.Filter, "", "filter")
	lm.ClearFilter = bindKeys(cfg.ClearFilter, "", "clear filter")
	// We handle quitting ourselves so quit/force-quit don't fall through to
	// the list's own (would-be) tea.Quit, and the help view is off so its
	// toggle keys would otherwise be dead bindings that still eat a keypress.
	lm.Quit = key.Binding{}
	lm.ForceQuit = key.Binding{}
	lm.ShowFullHelp = key.Binding{}
	lm.CloseFullHelp = key.Binding{}
	listKeys = lm

	buildHelpSections()
}

func init() {
	applyKeyConfig(DefaultKeyConfig())
}

// removeLabel spells the remove binding's help text as a double-tap (its
// first configured key pressed twice), matching the dd gesture in model.go.
func removeLabel(cfgKeys []string) string {
	if len(cfgKeys) == 0 {
		return ""
	}
	return keyLabel(cfgKeys[0]) + keyLabel(cfgKeys[0])
}
