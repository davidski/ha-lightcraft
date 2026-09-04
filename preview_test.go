package main

import (
	"context"
	"testing"
)

type previewAPI struct {
	calls []string
	data  []map[string]any
}

func (p *previewAPI) States(context.Context, []string) (map[string]LightState, error) {
	return map[string]LightState{
		"light.one": {EntityID: "light.one", State: "on", Attribute: map[string]any{"brightness": 120, "rgb_color": []any{1, 2, 3}}},
		"light.two": {EntityID: "light.two", State: "off"},
	}, nil
}

func (p *previewAPI) CallService(_ context.Context, domain, service, entity string, data map[string]any) error {
	p.calls = append(p.calls, domain+"."+service+" "+entity)
	p.data = append(p.data, data)
	return nil
}

func TestPreviewRestoresPowerState(t *testing.T) {
	api := &previewAPI{}
	preview := Preview{API: api}
	if err := preview.Capture(context.Background(), []string{"light.one", "light.two"}); err != nil {
		t.Fatal(err)
	}
	if err := preview.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 2 || api.calls[0] != "light.turn_on light.one" || api.calls[1] != "light.turn_off light.two" {
		t.Fatalf("unexpected restore calls: %#v", api.calls)
	}
	if api.data[0] == nil || api.data[0]["friendly_name"] != nil || api.data[0]["brightness"].(int) != 120 {
		t.Fatalf("unexpected restore data: %#v", api.data)
	}
}

func TestPreviewAppliesOnlyLightServiceFields(t *testing.T) {
	api := &previewAPI{}
	preview := Preview{API: api}
	if err := preview.Apply(context.Background(), map[string]map[string]any{
		"light.one": {"state": "on", "brightness": 80, "friendly_name": "ignore"},
	}); err != nil {
		t.Fatal(err)
	}
	if api.calls[0] != "light.turn_on light.one" || api.data[0]["friendly_name"] != nil || api.data[0]["brightness"].(int) != 80 {
		t.Fatalf("unexpected preview call: %#v %#v", api.calls, api.data)
	}
}
