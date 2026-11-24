// Command clessidra shows how much of the Claude 5-hour and 7-day limits an account has used,
// and when it runs out at the current pace.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type windowSpec struct {
	label  string
	length time.Duration
	pick   func(usage) *window
}

var windows = []windowSpec{
	{label: "5h", length: 5 * time.Hour, pick: func(u usage) *window { return u.FiveHour }},
	{label: "7d", length: 7 * 24 * time.Hour, pick: func(u usage) *window { return u.SevenDay }},
}

type app struct {
	getenv   func(string) string
	homeDir  func() (string, error)
	now      func() time.Time
	client   *http.Client
	usageURL string
	stdout   io.Writer
	stderr   io.Writer
	color    bool // the text report may use colors: stdout is a terminal that wants them
}

func main() {
	a := app{
		getenv:   os.Getenv,
		homeDir:  os.UserHomeDir,
		now:      time.Now,
		client:   &http.Client{Timeout: 10 * time.Second},
		usageURL: usageURL,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		color:    wantsColor(os.Stdout, os.Getenv),
	}
	os.Exit(a.run(context.Background(), os.Args[1:]))
}

// wantsColor is true for a terminal, unless NO_COLOR is set (no-color.org) or the terminal is dumb.
func wantsColor(f *os.File, getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a app) run(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("clessidra", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configDir := flags.String("config-dir", "", "Claude Code config directory (default $CLAUDE_CONFIG_DIR, then ~/.claude)")
	asJSON := flags.Bool("json", false, "print the report as JSON, for scripts")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(a.stderr, "usage: clessidra [--config-dir DIR] [--json]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(a.stderr, "clessidra: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return 2
	}

	dir, err := a.resolveConfigDir(*configDir)
	if err != nil {
		return a.fail(err)
	}
	now := a.now()
	token, err := readToken(dir, now)
	if err != nil {
		return a.fail(err)
	}
	u, err := fetchUsage(ctx, a.client, a.usageURL, token)
	if err != nil {
		return a.fail(err)
	}
	rs := reports(u, now)
	if *asJSON {
		err = writeJSON(a.stdout, dir, rs)
	} else {
		err = writeReport(a.stdout, dir, rs, now, a.color)
	}
	if err != nil {
		return a.fail(err)
	}
	return 0
}

func (a app) fail(err error) int {
	_, _ = fmt.Fprintf(a.stderr, "clessidra: %v\n", err)
	return 1
}

// resolveConfigDir picks the account the way Claude Code does: the flag, then CLAUDE_CONFIG_DIR, then ~/.claude.
func (a app) resolveConfigDir(flagValue string) (string, error) {
	dir := flagValue
	if dir == "" {
		dir = a.getenv("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		home, err := a.homeDir()
		if err != nil {
			return "", fmt.Errorf("find the home directory: %w", err)
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Abs(dir)
}

// report is one window as clessidra sees it: the text line and the JSON both say only this.
type report struct {
	Window  string   `json:"window"`
	Outcome string   `json:"outcome"` // no_data, not_started, too_early, lasts, runs_out, limit_reached
	Used    *float64 `json:"used"`    // percent of the window used, as the API gives it
	// ResetsAt is when the window starts again from 0.
	ResetsAt *time.Time `json:"resets_at"`
	// RunsOutAt and ShortByMinutes are set when the window runs out before it resets: when, and for how long
	// the account then waits for the reset.
	RunsOutAt      *time.Time `json:"runs_out_at"`
	ShortByMinutes *int       `json:"short_by_minutes"`
}

// assess turns one window of the usage API into a report.
func assess(label string, w *window, length time.Duration, now time.Time) report {
	r := report{Window: label, Outcome: "no_data"}
	if w == nil || w.Utilization == nil {
		return r
	}
	used := *w.Utilization
	r.Used = &used
	if w.ResetsAt == nil {
		r.Outcome = "not_started"
		return r
	}
	est := estimateWindow(used, *w.ResetsAt, length, now)
	if est.outcome == notStarted {
		zero := 0.0
		r.Used = &zero
		r.Outcome = "not_started"
		return r
	}
	resets := w.ResetsAt.In(now.Location())
	r.ResetsAt = &resets
	switch est.outcome {
	case tooEarly:
		r.Outcome = "too_early"
	case limitReached:
		r.Outcome = "limit_reached"
	case runsOut:
		r.Outcome = "runs_out"
		out := now.Add(est.runsOutIn)
		short := int(resets.Sub(out).Round(time.Minute) / time.Minute)
		r.RunsOutAt = &out
		r.ShortByMinutes = &short
	default:
		r.Outcome = "lasts"
	}
	return r
}

func reports(u usage, now time.Time) []report {
	out := make([]report, 0, len(windows))
	for _, spec := range windows {
		out = append(out, assess(spec.label, spec.pick(u), spec.length, now))
	}
	return out
}

func writeReport(w io.Writer, dir string, rs []report, now time.Time, color bool) error {
	if _, err := fmt.Fprintf(w, "account  %s\n", dir); err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := fmt.Fprintln(w, r.line(now, color)); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(w io.Writer, dir string, rs []report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Account string   `json:"account"`
		Windows []report `json:"windows"`
	}{dir, rs})
}

const barCells = 10

// line renders one window: its label, a bar of how much is used, the percent, then when it resets and
// whether it lasts. The bar and the percent have a fixed width, so the lines stay aligned with colors too.
//
//	5h  ████████░░   80%  out 12:45 · reset 14:00 · 1h 15m short
func (r report) line(now time.Time, color bool) string {
	if r.Used == nil {
		return fmt.Sprintf("%-4sno data", r.Window)
	}
	used := math.Max(0, math.Min(100, math.Floor(*r.Used)))
	filled := int(used / (100 / barCells))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barCells-filled)
	var tail string
	tone := plain
	switch r.Outcome {
	case "not_started":
		tail = "not started"
	case "too_early":
		tail = "reset " + formatTime(*r.ResetsAt, now) + " · too early"
	case "limit_reached":
		tail = "reset " + formatTime(*r.ResetsAt, now) + " · limit reached"
		tone = bad
	case "runs_out":
		tail = fmt.Sprintf("out %s · reset %s · %s short", formatTime(*r.RunsOutAt, now), formatTime(*r.ResetsAt, now),
			formatDuration(time.Duration(*r.ShortByMinutes)*time.Minute))
		tone = bad
	default:
		tail = "reset " + formatTime(*r.ResetsAt, now) + " · lasts"
		tone = good
	}
	// The percent rounds down, so 99.6% never shows as a full 100%.
	return fmt.Sprintf("%-4s%s  %3d%%  %s", r.Window, paint(bar, tone, color), int(used), paint(tail, tone, color))
}

type tone int

const (
	plain tone = iota
	good       // the window lasts until it resets
	bad        // the window runs out first, or already did
)

// paint colors s for a terminal: green when the window lasts, red when it does not, as it is otherwise.
func paint(s string, t tone, color bool) string {
	if !color {
		return s
	}
	switch t {
	case good:
		return "\x1b[32m" + s + "\x1b[0m"
	case bad:
		return "\x1b[31m" + s + "\x1b[0m"
	default:
		return s
	}
}

// formatTime shows t to the minute in now's time zone, with the day only when it is not today.
func formatTime(t, now time.Time) string {
	t = t.In(now.Location()).Round(time.Minute)
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04")
	}
	return t.Format("Mon 2 Jan 15:04")
}

// formatDuration keeps the two largest units: "2d 5h", "4h 21m", "35m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "<1m"
	}
	days := int(d / (24 * time.Hour))
	hours := int(d % (24 * time.Hour) / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
