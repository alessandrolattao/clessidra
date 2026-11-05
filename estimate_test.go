package main

import (
	"testing"
	"time"
)

func TestEstimateWindow(t *testing.T) {
	now := time.Date(2026, 11, 5, 11, 20, 0, 0, cet)
	const length = 5 * time.Hour

	tests := []struct {
		name      string
		used      float64
		resetsIn  time.Duration
		want      outcome
		runsOutIn time.Duration
	}{
		{"reset already passed", 40, -time.Minute, notStarted, 0},
		{"limit reached", 100, time.Hour, limitReached, 0},
		{"first 5% of the window", 3, 4*time.Hour + 50*time.Minute, tooEarly, 0},
		{"nothing used", 0, 2 * time.Hour, lastsUntilReset, 0},
		{"slow pace", 17, 2*time.Hour + 40*time.Minute, lastsUntilReset, 0},
		{"fast pace", 80, 2 * time.Hour, runsOut, 45 * time.Minute},
		{"runs out exactly at reset", 50, 150 * time.Minute, lastsUntilReset, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateWindow(tt.used, now.Add(tt.resetsIn), length, now)
			if got.outcome != tt.want || got.runsOutIn != tt.runsOutIn {
				t.Errorf("estimate = %+v, want outcome %d in %v", got, tt.want, tt.runsOutIn)
			}
		})
	}
}
