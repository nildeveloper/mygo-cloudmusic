package player

import (
	"time"

	"github.com/egoist/mygo/audio"
	"netease-music-mygo/internal/netease"
)

// ListeningEvent is cumulative audible progress for one playback instance.
// Network reporting belongs to the app, never to the audio or control paths.
type ListeningEvent struct {
	Track    netease.Track
	Instance uint64
	Heard    time.Duration
	Ended    bool
}

type listeningClock struct {
	out      *audio.Player
	instance uint64
	position time.Duration
	at       time.Time
	heard    time.Duration
	finished bool
}

// sample uses the audio player's heard position, bounded by elapsed time.
// Loading, paused time and stalled output do not advance the position.
func (m *listeningClock) sample(out *audio.Player, position time.Duration, at time.Time, running, end bool) bool {
	if m.out != out || m.finished {
		if m.out == out && !running {
			return false // EOF may be published more than once before a new play.
		}
		m.out, m.position, m.at = out, position, at
		m.heard, m.finished = 0, false
		m.instance++
	}
	delta := max(0, min(position-m.position, at.Sub(m.at)))
	m.heard += delta
	m.position, m.at = position, at
	m.finished = end
	return m.heard > 0 && (delta > 0 || end)
}

func (p *Player) observeListening(end bool) {
	if p.Listened == nil || p.out == nil || p.track == nil {
		return
	}
	if p.listening.sample(p.out, p.out.Position(), time.Now(), p.out.Playing(), end) {
		p.Listened(ListeningEvent{Track: *p.track, Instance: p.listening.instance, Heard: p.listening.heard, Ended: end})
	}
}

// PollListening is called while audio plays, including with the window hidden.
func (p *Player) PollListening() { p.observeListening(false) }

// FinishListening settles the current instance on EOF, replacement or shutdown.
func (p *Player) FinishListening() { p.observeListening(true) }

// Every output seek marks a discontinuity. Never credit the skipped interval,
// including a remote synchronization seek or a restored session's position.
func (p *Player) seekOutput(to time.Duration) {
	p.PollListening()
	p.out.Seek(to)
	p.listening.position, p.listening.at = p.out.Position(), time.Now()
}
