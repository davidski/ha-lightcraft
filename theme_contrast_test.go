package main

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestThemeContrast enforces the contrast matrix in docs/design-system.md.
func TestThemeContrast(t *testing.T) {
	raw, err := os.ReadFile("web.html")
	if err != nil {
		t.Fatal(err)
	}
	root := regexp.MustCompile(`:root\{[^}]*\}`).Find(raw)
	tokens := map[string]string{}
	for _, m := range regexp.MustCompile(`--([a-z-]+):(#[0-9a-f]{6})`).FindAllSubmatch(root, -1) {
		tokens[string(m[1])] = string(m[2])
	}
	surfaces := []string{"bg", "panel", "inset", "field", "code"}
	checks := []struct {
		fg  []string
		bg  []string
		min float64
	}{
		{[]string{"text", "muted", "accent", "danger", "pending", "selected-text"}, surfaces, 7},
		{[]string{"text", "muted"}, []string{"raised", "hover"}, 7},
		{[]string{"text"}, []string{"raised-strong", "notice", "error"}, 7},
		{[]string{"selected-text", "muted"}, []string{"selected"}, 7},
		{[]string{"accent"}, []string{"accent-soft"}, 7},
		{[]string{"on-accent"}, []string{"accent"}, 7},
		{[]string{"danger"}, []string{"error"}, 7},
		{[]string{"control-line", "danger-line", "accent"}, surfaces, 3},
	}
	for _, c := range checks {
		for _, fg := range c.fg {
			for _, bg := range c.bg {
				ratio := contrastRatio(t, tokens, fg, bg)
				if ratio < c.min {
					t.Errorf("--%s on --%s is %.2f:1, want at least %.1f:1", fg, bg, ratio, c.min)
				}
			}
		}
	}
}

func contrastRatio(t *testing.T, tokens map[string]string, fg, bg string) float64 {
	a, b := luminance(t, tokens, fg), luminance(t, tokens, bg)
	return (math.Max(a, b) + .05) / (math.Min(a, b) + .05)
}

func luminance(t *testing.T, tokens map[string]string, name string) float64 {
	hex, ok := tokens[name]
	if !ok {
		t.Fatalf("missing color token --%s", name)
	}
	var channels [3]float64
	for i := range channels {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= .03928 {
			channels[i] = c / 12.92
		} else {
			channels[i] = math.Pow((c+.055)/1.055, 2.4)
		}
	}
	return .2126*channels[0] + .7152*channels[1] + .0722*channels[2]
}
