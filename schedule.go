package main

import (
	"fmt"
	"time"
)

type Schedule struct {
	ID          string
	Name        string
	Selector    string
	Mode        string // relative or fixed
	AnchorMonth int
	AnchorDay   int
	StartDays   int
	EndDays     int
	StartDate   string // MM-DD when Mode is fixed
	EndDate     string // MM-DD when Mode is fixed
}

func (s Schedule) Validate() error {
	if s.ID == "" || s.Name == "" || s.Selector == "" {
		return fmt.Errorf("schedule requires id, name, and selector")
	}
	if s.Mode == "" {
		s.Mode = "relative"
	}
	if s.Mode != "relative" && s.Mode != "fixed" {
		return fmt.Errorf("schedule mode must be relative or fixed")
	}
	if _, err := time.Parse("2006-01-02", fmt.Sprintf("2001-%02d-%02d", s.AnchorMonth, s.AnchorDay)); err != nil {
		return fmt.Errorf("schedule anchor: %w", err)
	}
	if s.Mode == "fixed" {
		for label, value := range map[string]string{"start": s.StartDate, "end": s.EndDate} {
			if _, err := time.Parse("01-02", value); err != nil {
				return fmt.Errorf("schedule %s date: %w", label, err)
			}
		}
	}
	return nil
}

func (s Schedule) Automation() (map[string]any, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var active string
	if s.Mode == "fixed" {
		startMonth, startDay := monthDay(s.StartDate)
		endMonth, endDay := monthDay(s.EndDate)
		start := fmt.Sprintf("now().replace(month=%d, day=%d, hour=0, minute=0, second=0, microsecond=0)", startMonth, startDay)
		end := fmt.Sprintf("now().replace(month=%d, day=%d, hour=0, minute=0, second=0, microsecond=0)", endMonth, endDay)
		if startMonth > endMonth || (startMonth == endMonth && startDay > endDay) {
			active = fmt.Sprintf("{{ now() >= %s or now() <= %s }}", start, end)
		} else {
			active = fmt.Sprintf("{{ %s <= now() <= %s }}", start, end)
		}
	} else {
		anchor := fmt.Sprintf("now().replace(month=%d, day=%d, hour=0, minute=0, second=0, microsecond=0)", s.AnchorMonth, s.AnchorDay)
		active = fmt.Sprintf("{{ %s + timedelta(days=%d) <= now() <= %s + timedelta(days=%d) }}", anchor, s.StartDays, anchor, s.EndDays)
	}
	choose := map[string]any{
		"conditions": []any{map[string]any{"condition": "template", "value_template": active}},
		"sequence": []any{map[string]any{
			"action": "input_select.select_option",
			"target": map[string]any{"entity_id": "input_select.holiday"},
			"data":   map[string]any{"option": s.Selector},
		}},
	}
	return map[string]any{
		"id":    s.ID,
		"alias": s.Name,
		"mode":  "single",
		"triggers": []any{map[string]any{
			"trigger":        "template",
			"value_template": active,
		}},
		"actions": []any{map[string]any{
			"choose": []any{choose},
			"default": []any{map[string]any{
				"action": "input_select.select_option",
				"target": map[string]any{"entity_id": "input_select.holiday"},
				"data":   map[string]any{"option": "Other"},
			}},
		}},
	}, nil
}

func monthDay(value string) (int, int) {
	date, _ := time.Parse("01-02", value)
	return int(date.Month()), date.Day()
}
