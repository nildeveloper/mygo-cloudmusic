package player

import (
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// A paused track must publish loaded lyrics, while late results from a previous track stay silent.
func TestLyricsCompletionNotifiesCurrentTrack(t *testing.T) {
	p := &Player{gen: 2}
	calls := 0
	p.Changed = func() {
		calls++
		lines, loaded := p.Lyrics()
		if !loaded || len(lines) != 1 || lines[0].Text != "current" {
			t.Fatal("notified before lyrics were ready")
		}
	}
	p.acceptLyrics(1, []lyrics.Line{{Text: "stale"}})
	if calls != 0 || p.lyricsLoaded {
		t.Fatal("stale load was published")
	}
	p.acceptLyrics(2, []lyrics.Line{{Text: "current"}})
	if calls != 1 || p.Playing() {
		t.Fatal("paused lyric load was not published")
	}
	p.acceptLyrics(1, []lyrics.Line{{Text: "stale"}})
	if calls != 1 || p.lyrics[0].Text != "current" {
		t.Fatal("old load replaced current lyrics")
	}
}

// Repeating a song used to rewind without notifying presentations of the new position.
func TestRepeatOneNotifiesRewind(t *testing.T) {
	p := New(func(func()) {})
	p.Cue([]netease.Track{{ID: 1}})
	out := audio.NewPlayer(silence{})
	out.SetVolume(0)
	defer out.Close()
	p.Attach(out)
	out.Seek(10 * time.Second)
	p.repeat = RepeatOne
	notified := false
	p.Changed = func() {
		notified = true
		if p.Position() >= time.Second {
			t.Fatal("notification preceded rewind")
		}
	}
	p.ended()
	if !notified {
		t.Fatal("repeat did not notify")
	}
}
