// Package clock decides when the daily tick happens: 3:00 AM America/Toronto.
// There is no cron job; every command checks whether a boundary has passed.
package clock

import (
	"fmt"
	"os"
	"time"
	_ "time/tzdata" // don't depend on the server's zoneinfo
)

const TickHour = 3

// Zone is the game's home timezone.
var Zone = mustLoad("America/Toronto")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// Now returns the current time, or THEVAULT_NOW (RFC3339) when set, for testing.
func Now() (time.Time, error) {
	if v := os.Getenv("THEVAULT_NOW"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return time.Time{}, fmt.Errorf("THEVAULT_NOW: %w", err)
		}
		return t, nil
	}
	return time.Now(), nil
}

// LatestBoundary returns the most recent 3:00 AM Toronto time at or before t.
// time.Date normalises the calendar day, so DST changes are handled for us.
func LatestBoundary(t time.Time) time.Time {
	l := t.In(Zone)
	b := time.Date(l.Year(), l.Month(), l.Day(), TickHour, 0, 0, 0, Zone)
	if l.Before(b) {
		b = time.Date(l.Year(), l.Month(), l.Day()-1, TickHour, 0, 0, 0, Zone)
	}
	return b
}

// NextBoundary returns the first 3:00 AM Toronto time strictly after t.
func NextBoundary(t time.Time) time.Time {
	b := LatestBoundary(t)
	return time.Date(b.Year(), b.Month(), b.Day()+1, TickHour, 0, 0, 0, Zone)
}

// Missed returns the boundaries after last and at or before now, oldest
// first, keeping at most limit of the most recent ones.
func Missed(last, now time.Time, limit int) []time.Time {
	var out []time.Time
	for b := LatestBoundary(now); b.After(last); {
		out = append(out, b)
		b = time.Date(b.Year(), b.Month(), b.Day()-1, TickHour, 0, 0, 0, Zone)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Until formats the time left until the next tick, like "5h12m".
func Until(now time.Time) string {
	d := NextBoundary(now).Sub(now).Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", h, m)
}
