package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nfnt/resize"
)

// artLoadedMsg carries a decoded album-art image back to the model so it
// can be cached by key (Track.ArtKey) and rendered into the Queue screen's
// side pane. img is nil if the track has no art MPD can find.
type artLoadedMsg struct {
	key string
	img image.Image
}

func loadArt(c *Client, key, uri string) tea.Cmd {
	return func() tea.Msg {
		data, err := c.Art(uri)
		if err != nil {
			return artLoadedMsg{key: key}
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return artLoadedMsg{key: key}
		}
		return artLoadedMsg{key: key, img: img}
	}
}

// halfBlockUpper renders the upper half of a terminal cell; combined with a
// foreground/background color pair it lets one row of text cells represent
// two rows of image pixels, which is what makes a low-res terminal-cell
// grid look like an actual picture instead of a blocky mess.
const halfBlockUpper = "▀"

// renderArt scales img to fit within maxW x maxH terminal cells (preserving
// its aspect ratio, and accounting for the fact that the half-block trick
// packs two pixel rows into every one cell of height) and renders it as an
// ANSI-truecolor string. Returns "" if there isn't enough room left for a
// recognizable image.
func renderArt(img image.Image, maxW, maxH int) string {
	const minCells = 6
	if img == nil || maxW < minCells || maxH < minCells {
		return ""
	}

	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	// Fit within the box, trying full width first and shrinking to maxH if
	// that made it too tall (standard "fit inside a box" scaling, just with
	// the *2 to convert cell-height to pixel-row count).
	cellsW := maxW
	cellsH := (cellsW * srcH) / (srcW * 2)
	if cellsH > maxH {
		cellsH = maxH
		cellsW = (cellsH * 2 * srcW) / srcH
	}
	if cellsW < minCells || cellsH < 3 {
		return ""
	}

	scaled := resize.Resize(uint(cellsW), uint(cellsH*2), img, resize.Lanczos3)

	var out strings.Builder
	for y := 0; y < cellsH; y++ {
		if y > 0 {
			out.WriteByte('\n')
		}
		for x := 0; x < cellsW; x++ {
			top := colorAt(scaled, x, y*2)
			bottom := colorAt(scaled, x, y*2+1)
			cell := lipgloss.NewStyle().Foreground(top).Background(bottom).Render(halfBlockUpper)
			out.WriteString(cell)
		}
	}
	return out.String()
}

func colorAt(img image.Image, x, y int) lipgloss.Color {
	r, g, b, _ := img.At(x, y).RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
}
