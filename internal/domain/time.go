// Package domain holds the pure value types for the ATC facility scheduling
// engine: time-of-day arithmetic, qualifications, facilities, rule sets, shift
// templates, lines, controllers, and leave. Everything here is data with no I/O
// and no dependency on the rest of the application, so the coverage, validate,
// and materialize packages can be tested in isolation.
package domain

import (
	"fmt"
	"time"
)

// TimeOfDay is minutes since midnight, 0..1439. It is deliberately NOT a
// time.Time: a facility's "0545" is a wall-clock offset within an operating
// day, not an instant, and mixing the two invites time-zone and DST bugs.
type TimeOfDay int

// MinutesPerDay is the exclusive upper bound for a valid TimeOfDay.
const MinutesPerDay = 24 * 60

// ParseTimeOfDay parses "HHMM" (e.g. "0545" -> 345) or "HH:MM" (e.g. "05:45").
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	var hh, mm int
	switch len(s) {
	case 4: // HHMM
		if _, err := fmt.Sscanf(s, "%02d%02d", &hh, &mm); err != nil {
			return 0, fmt.Errorf("parse time of day %q: %w", s, err)
		}
	case 5: // HH:MM
		if _, err := fmt.Sscanf(s, "%02d:%02d", &hh, &mm); err != nil {
			return 0, fmt.Errorf("parse time of day %q: %w", s, err)
		}
	default:
		return 0, fmt.Errorf("parse time of day %q: expected HHMM or HH:MM", s)
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("parse time of day %q: hour/minute out of range", s)
	}
	return TimeOfDay(hh*60 + mm), nil
}

// MustParseTimeOfDay is ParseTimeOfDay that panics on error, for fixtures/tests.
func MustParseTimeOfDay(s string) TimeOfDay {
	t, err := ParseTimeOfDay(s)
	if err != nil {
		panic(err)
	}
	return t
}

// String renders as "HHMM" (zero-padded), the inverse of ParseTimeOfDay.
func (t TimeOfDay) String() string {
	return fmt.Sprintf("%02d%02d", int(t)/60, int(t)%60)
}

// Clock renders as "HH:MM" for human display.
func (t TimeOfDay) Clock() string {
	return fmt.Sprintf("%02d:%02d", int(t)/60, int(t)%60)
}

// Add returns t shifted by d, truncated to whole minutes. It does NOT wrap at
// midnight: a shift that runs past 2400 yields a value >= MinutesPerDay, which
// callers validating "no overnight" invariants can detect.
func (t TimeOfDay) Add(d time.Duration) TimeOfDay {
	return t + TimeOfDay(d/time.Minute)
}

// Sub returns the signed duration from other to t.
func (t TimeOfDay) Sub(other TimeOfDay) time.Duration {
	return time.Duration(t-other) * time.Minute
}

// Date is a calendar date with no time-zone concerns.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Weekday returns the day of week; Sunday == 0 lines up with Line.Days indexing.
func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

// AddDays returns the date n days later (n may be negative), normalized.
func (d Date) AddDays(n int) Date {
	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// String renders as YYYY-MM-DD.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}
