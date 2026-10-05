// Command mpdtui is a full-screen, keyboard-driven client for MPD (Music
// Player Daemon). Navigation is vim-style throughout (j/k, h/l, g/G, / to
// filter, dd to remove), with ranger-style Miller columns for browsing the
// library by artist, album, genre, and stored playlist.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// mpdAddress resolves the server address the same way mpc does: MPD_HOST
// may be "host", "password@host", a unix socket path, or an "@abstract"
// socket name; MPD_PORT defaults to 6600.
func mpdAddress(host, port string) (addr, password string) {
	if host == "" {
		host = "localhost"
	}
	// A leading "@" is an abstract socket, not an empty password; otherwise
	// everything before the first "@" is the password ("pw@@abstract" works).
	if i := strings.Index(host, "@"); i > 0 {
		password, host = host[:i], host[i+1:]
	}
	if strings.HasPrefix(host, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			host = home + host[1:]
		}
	}
	if strings.HasPrefix(host, "/") || strings.HasPrefix(host, "@") {
		return host, password
	}
	if port == "" {
		port = "6600"
	}
	return net.JoinHostPort(host, port), password
}

func main() {
	host := os.Getenv("MPD_HOST")
	port := os.Getenv("MPD_PORT")
	keysPath := os.Getenv("MPDTUI_KEYS")
	themeName := os.Getenv("MPDTUI_THEME")
	flag.StringVar(&host, "host", host, "MPD host, [password@]host, or unix socket path (default $MPD_HOST, else localhost)")
	flag.StringVar(&port, "port", port, "MPD port (default $MPD_PORT, else 6600)")
	flag.StringVar(&keysPath, "keys", keysPath, "path to a TOML file of keyboard shortcut overrides (optional)")
	flag.StringVar(&themeName, "theme", themeName, "built-in theme: dracula, nord, gruvbox, catppuccin, solarized, tokyonight, onedark, rosepine, everforest, monokai (omit for the default adaptive palette; ctrl+t picks one live)")
	flag.Parse()

	keyCfg, err := LoadKeyConfig(keysPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mpdtui:", err)
		os.Exit(1)
	}
	applyKeyConfig(keyCfg)

	// The same keybindings file may also carry a [theme] table (see
	// ThemeOverride in theme.go); themeName here is the flag/env override,
	// which wins over that file's [theme].base if both are set.
	pal, resolvedTheme, err := resolveTheme(themeName, keysPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mpdtui:", err)
		os.Exit(1)
	}
	applyPalette(pal)

	addr, password := mpdAddress(host, port)
	client := NewClient(addr, password)

	// A dedicated connection sits in MPD's "idle" and forwards each change
	// notification; when it drops, the footer shows disconnected and it
	// keeps redialing with backoff.
	events := make(chan tea.Msg, 8)
	go func() {
		backoff := time.Second
		for {
			start := time.Now()
			client.Watch(
				func() { events <- mpdConnectedMsg{} },
				func(subsystems []string) { events <- mpdChangedMsg(subsystems) },
			)
			events <- connLostMsg{}
			// A connection that lasted a while was healthy, not a repeat
			// failure — don't let one old drop keep every future retry
			// waiting the full backed-off delay.
			if time.Since(start) > 5*time.Second {
				backoff = time.Second
			}
			time.Sleep(backoff)
			if backoff < 15*time.Second {
				backoff *= 2
			}
		}
	}()

	m := newModel(client, events, resolvedTheme)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "mpdtui:", err)
		os.Exit(1)
	}
}
