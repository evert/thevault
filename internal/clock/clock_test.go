package clock

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, Zone)
	if err != nil {
		panic(err)
	}
	return t
}

func TestLatestBoundary(t *testing.T) {
	cases := []struct{ now, want string }{
		{"2026-10-29 02:59", "2026-10-28 03:00"},
		{"2026-10-29 03:00", "2026-10-29 03:00"},
		{"2026-10-29 23:00", "2026-10-29 03:00"},
		// Spring forward: 2:00 jumps to 3:00 on 2026-03-08.
		{"2026-03-08 01:59", "2026-03-07 03:00"},
		{"2026-03-08 03:30", "2026-03-08 03:00"},
		// Fall back: 1:00-2:00 repeats on 2026-11-01.
		{"2026-11-01 02:30", "2026-10-31 03:00"},
		{"2026-11-01 03:00", "2026-11-01 03:00"},
	}
	for _, c := range cases {
		got := LatestBoundary(at(c.now))
		if !got.Equal(at(c.want)) {
			t.Errorf("LatestBoundary(%s) = %s, want %s", c.now, got.In(Zone), c.want)
		}
	}
}

func TestLatestBoundaryFromUTC(t *testing.T) {
	// 07:30 UTC on Oct 29 is 03:30 EDT.
	got := LatestBoundary(time.Date(2026, 10, 29, 7, 30, 0, 0, time.UTC))
	if !got.Equal(at("2026-10-29 03:00")) {
		t.Fatalf("got %s", got.In(Zone))
	}
	// 06:59 UTC is 02:59 EDT, so still the previous day.
	got = LatestBoundary(time.Date(2026, 10, 29, 6, 59, 0, 0, time.UTC))
	if !got.Equal(at("2026-10-28 03:00")) {
		t.Fatalf("got %s", got.In(Zone))
	}
}

func TestBoundaryDaysAcrossDST(t *testing.T) {
	// Days around DST are 23h or 25h long; boundaries must stay at 03:00 local.
	b := at("2026-10-31 03:00")
	n := NextBoundary(b)
	if !n.Equal(at("2026-11-01 03:00")) || n.Sub(b) != 25*time.Hour {
		t.Fatalf("next = %s (%s)", n.In(Zone), n.Sub(b))
	}
	b = at("2026-03-07 03:00")
	n = NextBoundary(b)
	if !n.Equal(at("2026-03-08 03:00")) || n.Sub(b) != 23*time.Hour {
		t.Fatalf("next = %s (%s)", n.In(Zone), n.Sub(b))
	}
}

func TestMissed(t *testing.T) {
	last := at("2026-10-26 03:00")
	got := Missed(last, at("2026-10-29 10:00"), 7)
	want := []string{"2026-10-27 03:00", "2026-10-28 03:00", "2026-10-29 03:00"}
	if len(got) != len(want) {
		t.Fatalf("got %d boundaries, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(at(want[i])) {
			t.Errorf("[%d] = %s, want %s", i, got[i].In(Zone), want[i])
		}
	}
	if n := len(Missed(last, at("2026-10-26 23:00"), 7)); n != 0 {
		t.Errorf("same day: got %d, want 0", n)
	}
	if n := len(Missed(last, at("2026-12-26 23:00"), 7)); n != 7 {
		t.Errorf("cap: got %d, want 7", n)
	}
}
