package xhttp

import (
	"strings"
	"testing"
	"time"
)

func TestChromeVersionAt(t *testing.T) {
	release := time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		now    time.Time
		sample float64
		want   int
	}{
		{"unix epoch", time.Unix(0, 0), 0, 144},
		{"old clock", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), 0.99, 144},
		{"pre-release", release.AddDate(0, 0, -1), 0, 144},
		{"release", release, 0.99, 144},
		{"first increment", release.AddDate(0, 0, 70), 0, 145},
		{"future with jitter", release.AddDate(0, 0, 210), 0.5, 148},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := chromeVersionAt(tt.now, tt.sample)
			if version != tt.want {
				t.Fatalf("Chrome version = %d; want %d", version, tt.want)
			}
			for _, browser := range []string{"chrome", "edge"} {
				hint := getGreasedChUa(version, browser)
				if !strings.Contains(hint, "Chromium") {
					t.Fatalf("%s: missing Chromium brand in %q", browser, hint)
				}
			}
		})
	}
}
