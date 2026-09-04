package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func cieRGBHex(color CIEColor) string {
	rgb, ok := xyYToSRGB(color, cieDisplayY)
	if !ok {
		return "#000000"
	}
	return fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
}

func renderCIEPicker(color CIEColor) string {
	var plane strings.Builder
	for row := 0; row < ciePlaneHeight; row++ {
		topY := 0.95 - float64(row*2)*0.95/float64(ciePlaneHeight*2-1)
		bottomY := 0.95 - float64(row*2+1)*0.95/float64(ciePlaneHeight*2-1)
		for col := 0; col < ciePlaneWidth; col++ {
			x := float64(col) * 0.8 / float64(ciePlaneWidth-1)
			top, bottom := CIEColor{X: x, Y: topY}, CIEColor{X: x, Y: bottomY}
			topInside, bottomInside := cieInside(top), cieInside(bottom)
			cursorTop := math.Abs(color.X-x) < 0.8/float64(ciePlaneWidth) && math.Abs(color.Y-topY) < 0.95/float64(ciePlaneHeight*2)
			cursorBottom := math.Abs(color.X-x) < 0.8/float64(ciePlaneWidth) && math.Abs(color.Y-bottomY) < 0.95/float64(ciePlaneHeight*2)
			if !topInside && !bottomInside && !cursorTop && !cursorBottom {
				plane.WriteByte(' ')
				continue
			}
			if cursorTop || cursorBottom {
				plane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(cieRGBHex(color))).Render("●"))
				continue
			}
			fg, bg := "#000000", "#000000"
			if topInside {
				fg = cieRGBHex(top)
			}
			if bottomInside {
				bg = cieRGBHex(bottom)
			}
			plane.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(bg)).Render("▀"))
		}
		plane.WriteByte('\n')
	}
	preview := lipgloss.NewStyle().Background(lipgloss.Color(cieRGBHex(color))).Render("              ")
	content := titleStyle.Render("CIE xy Color") + "\n\n" + plane.String() + "\n" +
		fmt.Sprintf("  x: %.4f     y: %.4f\n\n  Preview: %s\n", color.X, color.Y, preview) +
		footerStyle.Render("←→ x  ↑↓ y  Shift/Ctrl + arrows coarse  Enter accept  Esc cancel")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).Render(content) + "\n"
}
