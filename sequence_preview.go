package main

import (
	"fmt"
	"math"
	"strings"
)

func sameXY(a, b []float64) bool {
	return len(a) == 2 && len(b) == 2 && math.Abs(a[0]-b[0]) < 0.000001 && math.Abs(a[1]-b[1]) < 0.000001
}

func nearXY(a, b []float64) bool {
	return len(a) == 2 && len(b) == 2 && math.Abs(a[0]-b[0]) < 0.005 && math.Abs(a[1]-b[1]) < 0.005
}

func currentCatalogStep(bundle Bundle, step ColorStep) ColorStep {
	colors := colorDefinitions(bundle)
	for _, color := range colors {
		if strings.EqualFold(strings.TrimSpace(step.Name), strings.TrimSpace(color.Name)) {
			step.XY = []float64{color.X, color.Y}
			return step
		}
	}
	for _, color := range colors {
		if nearXY(step.XY, []float64{color.X, color.Y}) {
			step.XY = []float64{color.X, color.Y}
			return step
		}
	}
	return step
}

type sequencePreviewTickMsg struct{}

func sequencePreviewTargetIDs(bundle Bundle, sequenceID string) []string {
	seen := map[string]bool{}
	for _, assignment := range lightingAssignments(bundle) {
		if assignment.Sequence != sequenceID {
			continue
		}
		for _, target := range assignment.Targets {
			seen[target] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return sortedStrings(ids)
}

func colorStepRGB(step ColorStep) ([3]int, bool) {
	if len(step.XY) != 2 {
		return [3]int{}, false
	}
	rgb, _, ok := displayRGB(map[string]any{"xy_color": []any{step.XY[0], step.XY[1]}})
	return rgb, ok
}

func previewRGBHex(step ColorStep) string {
	rgb, ok := colorStepRGB(step)
	if !ok {
		return "#000000"
	}
	return fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
}

func previewBrightness(brightness int) string {
	if brightness < 1 {
		brightness = 1
	}
	if brightness > 255 {
		brightness = 255
	}
	return fmt.Sprintf("%d%%", (brightness*100+127)/255)
}

func brightnessPercentValue(brightness int) int {
	if brightness < 0 {
		brightness = 0
	}
	if brightness > 255 {
		brightness = 255
	}
	return int(math.Round(float64(brightness) * 100 / 255))
}

func brightnessFromPercent(percent float64) int {
	return int(math.Round(percent * 255 / 100))
}
