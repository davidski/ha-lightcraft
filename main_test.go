package main

import (
	"reflect"
	"testing"
)

func TestSSHArgs(t *testing.T) {
	t.Setenv("HOMEASSISTANT_SSH_KEY", "/run/secrets/ha-lightcraft-key")
	want := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "IdentitiesOnly=yes", "-i", "/run/secrets/ha-lightcraft-key"}
	if got := sshArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ssh args = %#v, want %#v", got, want)
	}
}

func TestParseRefs(t *testing.T) {
	refs, err := parseRefs("scripts:halloween_orange,scripts:holiday_lights")
	if err != nil || len(refs) != 2 || refs[0].Kind != Scripts || refs[1].ID != "holiday_lights" {
		t.Fatalf("refs = %#v, err = %v", refs, err)
	}
}

func TestParseEmptyRefs(t *testing.T) {
	refs, err := parseRefs("")
	if err != nil || len(refs) != 0 {
		t.Fatalf("refs = %#v, err = %v", refs, err)
	}
}

func TestDraftDirectories(t *testing.T) {
	proposed, current := draftDirectories("data/holiday")
	if proposed != "data/holiday/proposed" || current != "data/holiday/current" {
		t.Fatalf("data directories = %q, %q", proposed, current)
	}
}
