package main

import (
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/bubbles/key"
)

// KeyConfig lists every configurable keyboard shortcut in mpdtui. Each
// field is a list of key names as reported by bubbletea's tea.KeyMsg.String()
// (e.g. "j", "ctrl+d", "shift+tab", "esc"); write "space" for the space bar.
// A TOML file overriding a subset of these is merged over Default(), so
// anything left unset keeps its built-in binding.
type KeyConfig struct {
	Tab1          []string `toml:"tab1"`
	Tab2          []string `toml:"tab2"`
	Tab3          []string `toml:"tab3"`
	Tab4          []string `toml:"tab4"`
	Tab5          []string `toml:"tab5"`
	NextTab       []string `toml:"next_tab"`
	PrevTab       []string `toml:"prev_tab"`
	Back          []string `toml:"back"`
	Into          []string `toml:"into"`
	Quit          []string `toml:"quit"`
	ForceQuit     []string `toml:"force_quit"`
	SelectItem    []string `toml:"select_item"`
	PlayPause     []string `toml:"play_pause"`
	Next          []string `toml:"next"`
	Prev          []string `toml:"prev"`
	VolUp         []string `toml:"vol_up"`
	VolDown       []string `toml:"vol_down"`
	Mute          []string `toml:"mute"`
	Shuffle       []string `toml:"shuffle"`
	Repeat        []string `toml:"repeat"`
	Enqueue       []string `toml:"enqueue"`
	AddToPlaylist []string `toml:"add_to_playlist"`
	Remove        []string `toml:"remove"`
	ClearQueue    []string `toml:"clear_queue"`
	MoveDown      []string `toml:"move_down"`
	MoveUp        []string `toml:"move_up"`
	ToggleCover   []string `toml:"toggle_cover"`
	Help          []string `toml:"help"`
	Search        []string `toml:"search"`
	UpdateDB      []string `toml:"update_db"`
	ToggleTabBar  []string `toml:"toggle_tab_bar"`
	CycleTheme    []string `toml:"cycle_theme"`

	// List-navigation shortcuts, wired into every screen's list.Model.
	CursorUp    []string `toml:"cursor_up"`
	CursorDown  []string `toml:"cursor_down"`
	PrevPage    []string `toml:"prev_page"`
	NextPage    []string `toml:"next_page"`
	GoToStart   []string `toml:"go_to_start"`
	GoToEnd     []string `toml:"go_to_end"`
	Filter      []string `toml:"filter"`
	ClearFilter []string `toml:"clear_filter"`
}

// DefaultKeyConfig returns mpdtui's built-in shortcuts.
func DefaultKeyConfig() KeyConfig {
	return KeyConfig{
		Tab1:    []string{"1"},
		Tab2:    []string{"2"},
		Tab3:    []string{"3"},
		Tab4:    []string{"4"},
		Tab5:    []string{"5"},
		NextTab: []string{"tab"},
		PrevTab: []string{"shift+tab"},

		Back:      []string{"esc", "backspace", "h"},
		Into:      []string{"l"},
		Quit:      []string{"q"},
		ForceQuit: []string{"ctrl+c"},

		SelectItem: []string{"enter"},

		PlayPause: []string{"space"},
		Next:      []string{"n"},
		Prev:      []string{"p"},
		VolUp:     []string{"+", "="},
		VolDown:   []string{"-", "_"},
		Mute:      []string{"m"},
		Shuffle:   []string{"s"},
		Repeat:    []string{"r"},

		Enqueue:       []string{"a"},
		AddToPlaylist: []string{"A"},
		Remove:        []string{"d"},
		ClearQueue:    []string{"D"},
		MoveDown:      []string{"J"},
		MoveUp:        []string{"K"},

		ToggleCover: []string{"c"},

		Help:         []string{"?"},
		Search:       []string{"ctrl+k"},
		UpdateDB:     []string{"U"},
		ToggleTabBar: []string{"ctrl+b"},
		CycleTheme:   []string{"ctrl+t"},

		// Lowercase h/l are reserved above for tree navigation (back/into),
		// so paging uses uppercase H/L plus the arrow/page/ctrl keys.
		CursorUp:    []string{"up", "k"},
		CursorDown:  []string{"down", "j"},
		PrevPage:    []string{"left", "H", "pgup", "ctrl+u"},
		NextPage:    []string{"right", "L", "pgdown", "ctrl+d"},
		GoToStart:   []string{"g", "home"},
		GoToEnd:     []string{"G", "end"},
		Filter:      []string{"/"},
		ClearFilter: []string{"esc"},
	}
}

// LoadKeyConfig reads a TOML file of shortcut overrides, merging it over
// DefaultKeyConfig. An empty path just returns the defaults.
func LoadKeyConfig(path string) (KeyConfig, error) {
	cfg := DefaultKeyConfig()
	if path == "" {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("loading key config %s: %w", path, err)
	}
	return cfg, nil
}

// normalizeKeyName maps a human-friendly name from the config file to the
// literal string bubbletea reports from tea.KeyMsg.String().
func normalizeKeyName(k string) string {
	if k == "space" {
		return " "
	}
	return k
}

// keyLabel is the flip side of normalizeKeyName, used to build a readable
// help-screen label out of the configured key names.
func keyLabel(k string) string {
	if k == " " {
		return "space"
	}
	return k
}

// bindKeys builds a key.Binding from configured key names. If label is
// empty, it's derived by joining the (display form of the) configured keys.
func bindKeys(cfgKeys []string, label, desc string) key.Binding {
	realKeys := make([]string, len(cfgKeys))
	for i, k := range cfgKeys {
		realKeys[i] = normalizeKeyName(k)
	}
	if label == "" {
		for i, k := range cfgKeys {
			if i > 0 {
				label += "/"
			}
			label += keyLabel(k)
		}
	}
	return key.NewBinding(key.WithKeys(realKeys...), key.WithHelp(label, desc))
}
