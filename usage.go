package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// usageURL reports the plan limits of a claude.ai login. Anthropic does not document it.
	usageURL   = "https://api.anthropic.com/api/oauth/usage"
	betaHeader = "oauth-2025-04-20"
)

// window is one rate-limit window: how much of it is used, in percent, and when it resets.
type window struct {
	Utilization *float64   `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

type usage struct {
	FiveHour *window `json:"five_hour"`
	SevenDay *window `json:"seven_day"`
}

func fetchUsage(ctx context.Context, client *http.Client, url, token string) (usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return usage{}, fmt.Errorf("build the usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("User-Agent", "clessidra")

	resp, err := client.Do(req)
	if err != nil {
		return usage{}, fmt.Errorf("call the usage API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return usage{}, errors.New("the usage API rejected the token, open Claude Code with this account to renew it")
	case http.StatusTooManyRequests:
		return usage{}, errors.New("the usage API is rate limiting this account, try again in a minute")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return usage{}, fmt.Errorf("the usage API answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var u usage
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return usage{}, fmt.Errorf("decode the usage API answer: %w", err)
	}
	return u, nil
}
