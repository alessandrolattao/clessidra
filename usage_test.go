package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sampleUsage is a trimmed answer of the real endpoint, extra fields included.
const sampleUsage = `{
  "five_hour": {"utilization": 17.0, "resets_at": "2026-11-05T12:59:59.733798+00:00", "limit_dollars": null},
  "seven_day": {"utilization": 12.0, "resets_at": "2026-11-10T15:59:59.733820+00:00", "limit_dollars": null},
  "seven_day_opus": null,
  "limits": [{"kind": "session", "group": "session", "percent": 17}]
}`

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want the bearer token", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != betaHeader {
			t.Errorf("anthropic-beta = %q, want %q", got, betaHeader)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchUsage(t *testing.T) {
	srv := serve(t, http.StatusOK, sampleUsage)

	u, err := fetchUsage(context.Background(), srv.Client(), srv.URL, "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.FiveHour == nil || *u.FiveHour.Utilization != 17 {
		t.Errorf("five_hour = %+v, want 17%%", u.FiveHour)
	}
	if want := time.Date(2026, 11, 5, 12, 59, 59, 733798000, time.UTC); !u.FiveHour.ResetsAt.Equal(want) {
		t.Errorf("five_hour resets_at = %v, want %v", u.FiveHour.ResetsAt, want)
	}
	if u.SevenDay == nil || *u.SevenDay.Utilization != 12 {
		t.Errorf("seven_day = %+v, want 12%%", u.SevenDay)
	}
}

func TestFetchUsageErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"token rejected", http.StatusUnauthorized, `{}`, "rejected the token, open Claude Code"},
		{"rate limited", http.StatusTooManyRequests, `{}`, "rate limiting this account"},
		{"server error", http.StatusInternalServerError, "boom\n", "answered 500 Internal Server Error: boom"},
		{"not json", http.StatusOK, "not json", "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, tt.status, tt.body)
			_, err := fetchUsage(context.Background(), srv.Client(), srv.URL, "tok")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
