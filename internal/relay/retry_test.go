package relay

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfterAt(t *testing.T) {
	now := time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "delta seconds", header: "15", want: 15 * time.Second},
		{name: "delta seconds capped", header: "90", want: maxRetryAfter},
		{name: "future HTTP date", header: now.Add(30 * time.Second).Format(http.TimeFormat), want: 30 * time.Second},
		{name: "future HTTP date capped", header: now.Add(2 * time.Minute).Format(http.TimeFormat), want: maxRetryAfter},
		{name: "past HTTP date", header: now.Add(-time.Second).Format(http.TimeFormat), want: 0},
		{name: "invalid", header: "later", want: 0},
		{name: "empty", header: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfterAt(tt.header, now); got != tt.want {
				t.Fatalf("parseRetryAfterAt(%q) = %s, want %s", tt.header, got, tt.want)
			}
		})
	}
}
