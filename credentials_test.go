package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var cet = time.FixedZone("CET", 3600)

func writeCredentials(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, credentialsFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadToken(t *testing.T) {
	now := time.Date(2026, 11, 5, 11, 20, 0, 0, cet)
	valid := now.Add(time.Hour).UnixMilli()
	expired := time.Date(2026, 11, 5, 0, 5, 0, 0, cet).UnixMilli()

	tests := []struct {
		name    string
		content string
		want    string
		wantErr string
	}{
		{"valid token", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, valid), "tok", ""},
		{"no expiry", `{"claudeAiOauth":{"accessToken":"tok"}}`, "tok", ""},
		{"expired token", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, expired), "", "expired at 00:05, open Claude Code with this account"},
		{"expired yesterday", fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, now.Add(-24*time.Hour).UnixMilli()), "", "expired at Wed 4 Nov 11:20"},
		{"no claude.ai login", `{"mcpOAuth":{}}`, "", "no claude.ai login"},
		{"empty token", `{"claudeAiOauth":{"accessToken":""}}`, "", "no claude.ai login"},
		{"broken json", `{`, "", "parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readToken(writeCredentials(t, tt.content), now)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("token = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadTokenMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := readToken(dir, time.Now())
	if err == nil || !strings.Contains(err.Error(), "no Claude Code login in "+dir) {
		t.Fatalf("err = %v, want a missing login error", err)
	}
}
