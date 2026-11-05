package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T, env map[string]string, usageServer *httptest.Server, now time.Time) (app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := app{
		getenv:  func(k string) string { return env[k] },
		homeDir: func() (string, error) { return "/home/someone", nil },
		now:     func() time.Time { return now },
		client:  http.DefaultClient,
		stdout:  &stdout,
		stderr:  &stderr,
	}
	if usageServer != nil {
		a.client = usageServer.Client()
		a.usageURL = usageServer.URL
	}
	return a, &stdout, &stderr
}

func TestResolveConfigDir(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{"flag wins", "/home/someone/.claude-work", "/home/someone/.claude-other", "/home/someone/.claude-work"},
		{"CLAUDE_CONFIG_DIR without flag", "", "/home/someone/.claude-other", "/home/someone/.claude-other"},
		{"default", "", "", "/home/someone/.claude"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, _ := testApp(t, map[string]string{"CLAUDE_CONFIG_DIR": tt.env}, nil, time.Now())
			got, err := a.resolveConfigDir(tt.flag)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("dir = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveConfigDirRelative(t *testing.T) {
	a, _, _ := testApp(t, nil, nil, time.Now())
	got, err := a.resolveConfigDir(".claude-work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != ".claude-work" {
		t.Errorf("dir = %q, want an absolute path to .claude-work", got)
	}
}

func TestResolveConfigDirNoHome(t *testing.T) {
	a, _, _ := testApp(t, nil, nil, time.Now())
	a.homeDir = func() (string, error) { return "", errors.New("no HOME") }
	if _, err := a.resolveConfigDir(""); err == nil {
		t.Fatal("want an error without a home directory")
	}
}

func TestRun(t *testing.T) {
	now := time.Date(2026, 11, 5, 11, 20, 0, 0, cet)
	dir := writeCredentials(t, fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, now.Add(6*time.Hour).UnixMilli()))
	a, stdout, stderr := testApp(t, map[string]string{"CLAUDE_CONFIG_DIR": dir}, serve(t, http.StatusOK, sampleUsage), now)

	if code := a.run(context.Background(), nil); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
	want := fmt.Sprintf("%-9s%s\n", "account", dir) +
		fmt.Sprintf("%-9s%-5s%-25s%s\n", "5h", "17%", "resets 14:00", "lasts until reset") +
		fmt.Sprintf("%-9s%-5s%-25s%s\n", "7d", "12%", "resets Tue 10 Nov 17:00", "lasts until reset")
	if got := stdout.String(); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunExpiredToken(t *testing.T) {
	now := time.Date(2026, 11, 5, 11, 20, 0, 0, cet)
	dir := writeCredentials(t, fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, time.Date(2026, 11, 5, 0, 5, 0, 0, cet).UnixMilli()))
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the usage API must not be called with an expired token")
	}))
	t.Cleanup(srv.Close)
	a, stdout, stderr := testApp(t, nil, srv, now)

	if code := a.run(context.Background(), []string{"--config-dir", dir}); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	want := "clessidra: token in " + dir + " expired at 00:05, open Claude Code with this account to renew it\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestRunArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"help", []string{"-h"}, 0},
		{"unknown flag", []string{"--nope"}, 2},
		{"stray argument", []string{"extra"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, stderr := testApp(t, nil, nil, time.Now())
			if code := a.run(context.Background(), tt.args); code != tt.code {
				t.Errorf("exit code %d, want %d", code, tt.code)
			}
			if !strings.Contains(stderr.String(), "usage: clessidra [--config-dir DIR]") {
				t.Errorf("stderr = %q, want the usage", stderr)
			}
		})
	}
}

func TestDescribeWindow(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, cet)
	at := func(h, m int) *time.Time { v := time.Date(2026, 11, 5, h, m, 0, 0, cet); return &v }
	pct := func(v float64) *float64 { return &v }

	tests := []struct {
		name string
		w    *window
		want string
	}{
		{"missing window", nil, "no data"},
		{"missing utilization", &window{ResetsAt: at(14, 0)}, "no data"},
		{"no reset yet", &window{Utilization: pct(0)}, "0%\tnot started"},
		{"reset passed", &window{Utilization: pct(40), ResetsAt: at(11, 0)}, "0%\tnot started"},
		{"too early", &window{Utilization: pct(2), ResetsAt: at(16, 55)}, "2%\tresets 16:55\ttoo early to estimate"},
		{"lasts", &window{Utilization: pct(17), ResetsAt: at(14, 0)}, "17%\tresets 14:00\tlasts until reset"},
		{"runs out", &window{Utilization: pct(80), ResetsAt: at(14, 0)}, "80%\tresets 14:00\truns out in 45m (12:45)"},
		{"limit reached", &window{Utilization: pct(100), ResetsAt: at(14, 0)}, "100%\tresets 14:00\tlimit reached"},
		{"rounds down", &window{Utilization: pct(99.6), ResetsAt: at(14, 0)}, "99%\tresets 14:00\truns out in 1m (12:01)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeWindow(tt.w, 5*time.Hour, now); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	now := time.Date(2026, 11, 5, 11, 20, 0, 0, cet)
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"today, rounded to the minute", time.Date(2026, 11, 5, 12, 59, 59, 733798000, time.UTC), "14:00"},
		{"another day", time.Date(2026, 11, 10, 15, 59, 59, 733820000, time.UTC), "Tue 10 Nov 17:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTime(tt.t, now); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{20 * time.Second, "<1m"},
		{35 * time.Minute, "35m"},
		{2 * time.Hour, "2h"},
		{4*time.Hour + 21*time.Minute, "4h 21m"},
		{48 * time.Hour, "2d"},
		{53*time.Hour + 10*time.Minute, "2d 5h"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
