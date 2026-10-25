package main

import (
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{"7d": "2026-10-18", "2w": "2026-10-11", "2026-10-01": "2026-10-01", "": "0001-01-01"} {
		got, err := parseWhen(in, now)
		if err != nil || got.Format(time.DateOnly) != want {
			t.Errorf("parseWhen(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	if _, err := parseWhen("včera", now); err == nil {
		t.Error("expected error")
	}
}

func TestStateFile(t *testing.T) {
	for in, want := range map[string]string{"": "state.json", "jmk-daily": "state-jmk-daily.json", "Vysočina/1": "state-Vyso-ina-1.json"} {
		if got := stateFile(in); got != want {
			t.Errorf("stateFile(%q) = %q, want %q", in, got, want)
		}
	}
}
