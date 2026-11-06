package player

import (
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
)

// Command is local intent. An external session may consume it before any
// playback mutation. Volume and metadata notifications remain local.
type Command struct {
	Kind     string
	Playing  bool
	Position time.Duration
	Index    int
	Queue    []netease.Track
}

func (p *Player) dispatch(c Command) bool { return p.Control != nil && p.Control(c) }

// Playback is a deterministic target from an external controller. Applying it
// never emits another command, including when its audio finishes loading.
type Playback struct {
	Instance uint64 // nonzero external playback identity; unchanged during seeks
	Queue    []netease.Track
	Order    []int
	Index    int
	Playing  bool
	Position time.Duration
	Mode     string
}

func validOrder(order []int, n int) bool {
	if len(order) != n {
		return false
	}
	seen := make([]bool, n)
	for _, i := range order {
		if i < 0 || i >= len(seen) || seen[i] {
			return false
		}
		seen[i] = true
	}
	return true
}

func (p *Player) Apply(s Playback) {
	if s.Index < 0 || s.Index >= len(s.Queue) || !validOrder(s.Order, len(s.Queue)) {
		return
	}
	at := slices.Index(s.Order, s.Index)
	same := p.track != nil && p.track.ID == s.Queue[s.Index].ID
	p.endRadio()
	p.queue = slices.Clone(s.Queue)
	p.order = slices.Clone(s.Order)
	p.at = at
	p.shuffle = s.Mode == "RANDOM"
	p.repeat = RepeatAll
	if s.Mode == "SINGLE_LOOP" {
		p.repeat = RepeatOne
	}
	pos := min(max(0, s.Position), max(0, s.Queue[s.Index].Length()-time.Millisecond))
	if !same || !p.loading && p.out == nil {
		// A new target can retry a failed decoder; unchanged polls never Apply.
		p.loadTrack(at, s.Playing, pos)
		p.instance = s.Instance
		p.pendingAt = time.Now()
		return
	}
	if p.loading {
		p.instance = s.Instance
		p.pending = pos
		p.pendingAt = time.Now()
		p.playing = s.Playing
		p.sync()
		return
	}
	p.playing = s.Playing
	p.seek(pos, s.Instance)
}

// Clear also invalidates already queued decoder/seek callbacks.
func (p *Player) Clear() {
	p.gen++
	p.seekGen++
	p.endRadio()
	p.stop()
	p.queue, p.order, p.track = nil, nil, nil
	p.at = -1
	p.pending = 0
	p.pendingAt = time.Time{}
	p.loading, p.playing = false, false
	p.lyrics = nil
	p.lyricsLoaded = false
	p.err = ""
	p.errDetails = ""
	p.sync()
}

func (p *Player) SetPlaying(playing bool) {
	if p.dispatch(Command{Kind: "playing", Playing: playing}) {
		return
	}
	p.setPlaying(playing)
}
func (p *Player) setPlaying(playing bool) {
	if p.playing != playing {
		p.togglePlay()
	}
}
