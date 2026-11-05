// Command clessidra shows how much of the Claude 5-hour and 7-day limits an account has used,
// and when it runs out at the current pace.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"text/tabwriter"
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
	}
	os.Exit(a.run(context.Background(), os.Args[1:]))
}

func (a app) run(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("clessidra", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configDir := flags.String("config-dir", "", "Claude Code config directory (default $CLAUDE_CONFIG_DIR, then ~/.claude)")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(a.stderr, "usage: clessidra [--config-dir DIR]")
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
	if err := writeReport(a.stdout, dir, u, now); err != nil {
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

func writeReport(w io.Writer, dir string, u usage, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "account\t%s\n", dir)
	for _, spec := range windows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", spec.label, describeWindow(spec.pick(u), spec.length, now))
	}
	return tw.Flush()
}

// describeWindow renders the tab-separated cells of one window line.
func describeWindow(w *window, length time.Duration, now time.Time) string {
	if w == nil || w.Utilization == nil {
		return "no data"
	}
	used := *w.Utilization
	if w.ResetsAt == nil {
		return formatPercent(used) + "\tnot started"
	}

	est := estimateWindow(used, *w.ResetsAt, length, now)
	if est.outcome == notStarted {
		return formatPercent(0) + "\tnot started"
	}
	cells := formatPercent(used) + "\tresets " + formatTime(*w.ResetsAt, now) + "\t"
	switch est.outcome {
	case tooEarly:
		return cells + "too early to estimate"
	case limitReached:
		return cells + "limit reached"
	case runsOut:
		return cells + fmt.Sprintf("runs out in %s (%s)", formatDuration(est.runsOutIn), formatTime(now.Add(est.runsOutIn), now))
	default:
		return cells + "lasts until reset"
	}
}

// formatPercent rounds down, so 99.6% never shows as a full 100%.
func formatPercent(used float64) string {
	return fmt.Sprintf("%d%%", int(math.Floor(used)))
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
