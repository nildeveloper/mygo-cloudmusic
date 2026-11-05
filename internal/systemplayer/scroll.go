package systemplayer

import "time"

// marquee advances only while visible and playing. Pausing freezes its phase;
// a new line resets it, including consecutive lines with identical text.
type marquee struct {
	track   int64
	line    int
	text    string
	phase   time.Duration
	last    time.Time
	running bool
}

func (m *marquee) set(track int64, line int, text string) bool {
	if m.track == track && m.line == line && m.text == text {
		return false
	}
	*m = marquee{track: track, line: line, text: text}
	return true
}

func (m *marquee) step(now time.Time, overflow float64, running bool) float64 {
	if m.running && running && !m.last.IsZero() {
		m.phase += max(0, now.Sub(m.last))
	}
	m.last, m.running = now, running
	if overflow <= 0 {
		m.phase = 0
		return 0
	}
	const pause = time.Second
	travel := time.Duration(overflow / 28 * float64(time.Second))
	cycle := 2*pause + travel
	at := m.phase % cycle
	if at <= pause {
		return 0
	}
	if at >= pause+travel {
		return overflow
	}
	return min(overflow, float64(at-pause)/float64(time.Second)*28)
}
