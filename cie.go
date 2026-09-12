package main

import "math"

// CIEColor is the authoritative picker value. RGB exists only for display.
type CIEColor struct {
	X float64
	Y float64
}

const (
	ciePlaneWidth  = 40
	ciePlaneHeight = 14 // each row contains two vertical samples
	cieDisplayY    = 1.0
)

func xyYToXYZ(color CIEColor, luminance float64) ([3]float64, bool) {
	if color.X < 0 || color.Y <= 0 || color.X+color.Y > 1 || luminance < 0 {
		return [3]float64{}, false
	}
	return [3]float64{
		color.X * luminance / color.Y,
		luminance,
		(1 - color.X - color.Y) * luminance / color.Y,
	}, true
}

func xyzToSRGB(xyz [3]float64) [3]uint8 {
	linear := [3]float64{
		3.2406*xyz[0] - 1.5372*xyz[1] - 0.4986*xyz[2],
		-0.9689*xyz[0] + 1.8758*xyz[1] + 0.0415*xyz[2],
		0.0557*xyz[0] - 0.2040*xyz[1] + 1.0570*xyz[2],
	}
	// xy colors from real lights frequently sit outside the sRGB gamut.
	// Clip only negative channels, then scale the remaining channels together
	// so hue is preserved instead of turning saturated blue into cyan.
	maxLinear := 0.0
	for i, value := range linear {
		if value < 0 {
			linear[i] = 0
			continue
		}
		if value > maxLinear {
			maxLinear = value
		}
	}
	if maxLinear > 1 {
		for i := range linear {
			linear[i] /= maxLinear
		}
	}
	return [3]uint8{srgbByte(linear[0]), srgbByte(linear[1]), srgbByte(linear[2])}
}

// xyYToSRGB converts CIE xyY to clamped 8-bit sRGB for terminal display.
func xyYToSRGB(color CIEColor, luminance float64) ([3]uint8, bool) {
	xyz, ok := xyYToXYZ(color, luminance)
	if !ok {
		return [3]uint8{}, false
	}
	return xyzToSRGB(xyz), true
}

func srgbByte(value float64) uint8 {
	if value <= 0.0031308 {
		value *= 12.92
	} else {
		value = 1.055*math.Pow(math.Max(0, value), 1/2.4) - 0.055
	}
	return uint8(math.Round(math.Max(0, math.Min(1, value)) * 255))
}

// visibleLocus is a deliberately coarse spectral-locus approximation. It is
// used only to keep the terminal plane legible; the selected xy is unrestricted
// apart from the valid chromaticity triangle.
var visibleLocus = [][2]float64{
	{0.174, 0.005}, {0.150, 0.020}, {0.100, 0.050}, {0.040, 0.180},
	{0.010, 0.400}, {0.004, 0.600}, {0.010, 0.760}, {0.050, 0.820},
	{0.150, 0.760}, {0.300, 0.600}, {0.500, 0.450}, {0.650, 0.350},
	{0.735, 0.265},
}

func cieInside(color CIEColor) bool {
	inside := false
	for i, point := range visibleLocus {
		next := visibleLocus[(i+1)%len(visibleLocus)]
		if (point[1] > color.Y) != (next[1] > color.Y) && color.X < (next[0]-point[0])*(color.Y-point[1])/(next[1]-point[1])+point[0] {
			inside = !inside
		}
	}
	return inside
}
