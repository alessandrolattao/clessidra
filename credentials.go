package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// credentialsFile is where Claude Code keeps its claude.ai login on Linux.
const credentialsFile = ".credentials.json"

type credentials struct {
	ClaudeAiOauth *struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// readToken returns the claude.ai access token Claude Code stored in dir. It never refreshes or writes the file.
func readToken(dir string, now time.Time) (string, error) {
	path := filepath.Join(dir, credentialsFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no Claude Code login in %s: %s not found", dir, credentialsFile)
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	var c credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if c.ClaudeAiOauth == nil || c.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("no claude.ai login in %s", path)
	}

	if ms := c.ClaudeAiOauth.ExpiresAt; ms > 0 && !now.Before(time.UnixMilli(ms)) {
		return "", fmt.Errorf("token in %s expired at %s, open Claude Code with this account to renew it",
			dir, formatTime(time.UnixMilli(ms), now))
	}
	return c.ClaudeAiOauth.AccessToken, nil
}
