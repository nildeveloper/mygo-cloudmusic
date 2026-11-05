package systemplayer

import (
	"testing"
	"time"
)

// Pausing freezes a long line and resuming never includes time spent paused.
func TestMarqueePauseAndResume(t *testing.T) {
	var m marquee
	m.set(1, 0, "a long line")
	start := time.Unix(1000, 0)
	m.step(start, 100, true)
	x := m.step(start.Add(2*time.Second), 100, true)
	if x != 28 {
		t.Fatalf("offset %v, want 28", x)
	}
	if got := m.step(start.Add(2*time.Second), 100, false); got != x {
		t.Fatal("pause moved the text")
	}
	if got := m.step(start.Add(time.Hour), 100, false); got != x {
		t.Fatal("paused time advanced the text")
	}
	if got := m.step(start.Add(time.Hour), 100, true); got != x {
		t.Fatal("resume jumped forward")
	}
	if got := m.step(start.Add(time.Hour+time.Second), 100, true); got != 56 {
		t.Fatalf("resumed at %v", got)
	}
}

// Repeated lyrics reset on line or track changes, but ordinary state updates do not.
func TestMarqueeLineIdentity(t *testing.T) {
	var m marquee
	start := time.Unix(1000, 0)
	m.set(1, 0, "same words")
	m.step(start, 100, true)
	m.step(start.Add(2*time.Second), 100, true)
	if m.set(1, 0, "same words") {
		t.Fatal("same line was reset")
	}
	if !m.set(1, 1, "same words") || m.phase != 0 {
		t.Fatal("new line did not reset")
	}
	if !m.set(2, 1, "same words") {
		t.Fatal("new track did not reset")
	}
}

// The marquee holds both ends and never scrolls a line which fits its viewport.
func TestMarqueeEndsAndShortLines(t *testing.T) {
	start := time.Unix(1000, 0)
	var m marquee
	m.step(start, 28, true)
	for _, tc := range []struct {
		at   time.Duration
		want float64
	}{
		{500 * time.Millisecond, 0}, {1500 * time.Millisecond, 14},
		{2500 * time.Millisecond, 28}, {3 * time.Second, 0},
	} {
		if got := m.step(start.Add(tc.at), 28, true); got != tc.want {
			t.Fatalf("at %v: %v, want %v", tc.at, got, tc.want)
		}
	}
	if got := m.step(start.Add(4*time.Second), 0, false); got != 0 {
		t.Fatal("short text moved")
	}
}
