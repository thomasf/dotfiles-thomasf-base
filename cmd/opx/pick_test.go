package main

import (
	"slices"
	"testing"
	"time"
)

func TestSortItems(t *testing.T) {
	items := []ListItem{{ID: "1", Title: "beta"}, {ID: "2", Title: "Alpha"}, {ID: "3", Title: "zeta"}}
	sortItems(items, "3")
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	if want := []string{"zeta", "Alpha", "beta"}; !slices.Equal(titles, want) {
		t.Errorf("got %q, want %q", titles, want)
	}
}

func TestOTPWait(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) // a period boundary
	for _, tc := range []struct {
		at, wait, valid time.Duration
	}{
		{10 * time.Second, 0, 20 * time.Second},
		{27 * time.Second, 3 * time.Second, 30 * time.Second},
	} {
		wait, valid := otpWait(base.Add(tc.at))
		if wait != tc.wait || valid != tc.valid {
			t.Errorf("at %v: wait %v valid %v, want %v %v", tc.at, wait, valid, tc.wait, tc.valid)
		}
	}
}

func TestLastUseNext(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := lastUse{ItemID: "a", Field: 1, Time: now}
	for _, tc := range []struct {
		name string
		item string
		n    int
		at   time.Time
		want int
	}{
		{"next field", "a", 5, now.Add(time.Minute), 2},
		{"wraps", "a", 2, now.Add(time.Minute), 0},
		{"other item", "b", 5, now.Add(time.Minute), 0},
		{"too late", "a", 5, now.Add(resumeFor), 0},
		{"clock went back", "a", 5, now.Add(-time.Minute), 0},
	} {
		if got := l.next(tc.item, tc.n, tc.at); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
