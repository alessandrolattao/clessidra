package main

import "time"

type outcome int

const (
	notStarted      outcome = iota // nothing used since the last reset
	tooEarly                       // too little of the window has passed to trust the average
	lastsUntilReset                // at this pace the window resets before it is used up
	runsOut                        // at this pace the window is used up before it resets
	limitReached
)

// minElapsedShare is how much of the window must pass before its average rate means anything.
const minElapsedShare = 0.05

type estimate struct {
	outcome   outcome
	runsOutIn time.Duration // set only when outcome is runsOut
}

// estimateWindow extrapolates the average rate since the window started, (used %) / (time elapsed), up to 100%.
func estimateWindow(used float64, resetsAt time.Time, length time.Duration, now time.Time) estimate {
	remaining := resetsAt.Sub(now)
	if remaining <= 0 {
		return estimate{outcome: notStarted}
	}
	if used >= 100 {
		return estimate{outcome: limitReached}
	}
	elapsed := length - remaining
	if elapsed <= 0 || float64(elapsed) < minElapsedShare*float64(length) {
		return estimate{outcome: tooEarly}
	}
	if used <= 0 {
		return estimate{outcome: lastsUntilReset}
	}
	toLimit := time.Duration(float64(elapsed) * (100 - used) / used)
	if toLimit >= remaining {
		return estimate{outcome: lastsUntilReset}
	}
	return estimate{outcome: runsOut, runsOutIn: toLimit}
}
