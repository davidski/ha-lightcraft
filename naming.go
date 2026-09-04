package main

import (
	"regexp"
	"strings"
)

var nonIdentifier = regexp.MustCompile(`[^a-z0-9]+`)

func SuggestSceneID(holiday, color string) string {
	value := strings.ToLower(strings.TrimSpace(holiday) + " " + strings.TrimSpace(color))
	value = strings.Trim(nonIdentifier.ReplaceAllString(value, "_"), "_")
	if value == "" {
		return "holiday_scene"
	}
	return value
}

func SuggestedRGB(holiday, color string) ([3]int, bool) {
	key := strings.ToLower(strings.TrimSpace(holiday) + ":" + strings.TrimSpace(color))
	value, ok := map[string][3]int{
		"halloween:orange":             {255, 80, 0},
		"halloween:purple":             {150, 0, 255},
		"christmas:red":                {255, 0, 0},
		"christmas:green":              {0, 180, 30},
		"christmas:white":              {255, 255, 255},
		"easter:pastel pink":           {255, 160, 190},
		"easter:pastel green":          {150, 255, 170},
		"easter:pastel blue":           {150, 210, 255},
		"easter:pastel purple":         {210, 160, 255},
		"easter:pastel yellow":         {255, 245, 150},
		"memorial day:american red":    {220, 0, 30},
		"memorial day:american blue":   {0, 80, 220},
		"memorial day:christmas white": {255, 255, 255},
	}[key]
	return value, ok
}
