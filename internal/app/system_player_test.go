package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/systemplayer"
)

// Lyric snapshots follow seeks in either direction and replace missing lyrics with the current title.
func TestSystemPlayerLyricsFollowPosition(t *testing.T) {
	a := newTestApp()
	if s := a.systemPlayerState(); s.HasTrack || s.CanToggle {
		t.Fatal("empty player enabled controls")
	}
	a.pl.Cue([]netease.Track{{ID: 7, Name: "song", Duration: 60000}})
	a.pl.SetLyrics([]lyrics.Line{{At: time.Second, Text: "one"}, {At: 3 * time.Second, Text: "two"}})
	for _, tc := range []struct {
		at    time.Duration
		index int
		text  string
	}{
		{0, -1, "song"}, {time.Second, 0, "one"}, {4 * time.Second, 1, "two"}, {2 * time.Second, 0, "one"},
	} {
		a.pl.Hold(tc.at)
		s := a.systemPlayerState()
		if s.LyricIndex != tc.index || s.LyricText != tc.text {
			t.Fatalf("at %v: %+v", tc.at, s)
		}
	}
	a.pl.Cue([]netease.Track{{ID: 8, Name: "next"}})
	a.pl.SetLyrics(nil)
	if s := a.systemPlayerState(); s.LyricText != "next" || s.TrackID != 8 {
		t.Fatalf("stale lyrics: %+v", s)
	}
}

// Two presentations share one timer; cancellation also invalidates a callback already queued on the main thread.
func TestSystemPlayerTimerLifecycle(t *testing.T) {
	var pending []func()
	cancels := 0
	original := systemPlayerAfter
	t.Cleanup(func() { systemPlayerAfter = original })
	systemPlayerAfter = func(_ time.Duration, fn func()) func() { pending = append(pending, fn); return func() { cancels++ } }
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 7}})
	a.pl.SetLyrics([]lyrics.Line{{Text: "one"}})
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	a.pl.Attach(out)
	t.Cleanup(out.Close)
	s := &systemPlayer{a: a, touchVisible: true, options: systemplayer.Options{MenuBarVisible: true, MenuBarLyrics: true, TouchBarLyrics: true}}
	s.sync()
	s.sync()
	if len(pending) != 1 {
		t.Fatalf("scheduled %d timers", len(pending))
	}
	pending[0]()
	if len(pending) != 2 {
		t.Fatal("playing did not continue sampling")
	}
	s.options = systemplayer.Options{}
	s.sync()
	if cancels != 1 {
		t.Fatal("disabling lyrics did not cancel timer")
	}
	pending[1]()
	if len(pending) != 2 {
		t.Fatal("stale callback restarted timer")
	}
	s.options = systemplayer.Options{MenuBarVisible: true, MenuBarLyrics: true}
	s.sync()
	if len(pending) != 3 {
		t.Fatal("enabling lyrics did not restart sampling")
	}
	s.close()
	s.close()
	pending[2]()
	if len(pending) != 3 || cancels != 2 {
		t.Fatal("closed module kept sampling")
	}
}

// A hidden Touch Bar used to keep sampling; native visibility now cancels stale work and restarts immediately when shown.
func TestSystemPlayerTouchBarVisibility(t *testing.T) {
	var pending []func()
	cancels := 0
	original := systemPlayerAfter
	t.Cleanup(func() { systemPlayerAfter = original })
	systemPlayerAfter = func(_ time.Duration, fn func()) func() {
		pending = append(pending, fn)
		return func() { cancels++ }
	}
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 7}})
	a.pl.SetLyrics([]lyrics.Line{{Text: "one"}})
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	a.pl.Attach(out)
	t.Cleanup(out.Close)
	s := &systemPlayer{a: a, options: systemplayer.Options{TouchBarLyrics: true}}
	s.sync()
	if len(pending) != 0 {
		t.Fatal("hidden Touch Bar sampled lyrics")
	}
	s.touchBarVisibilityChanged(true)
	s.touchBarVisibilityChanged(true)
	if len(pending) != 1 {
		t.Fatal("showing the item did not schedule exactly one sample")
	}
	s.touchBarVisibilityChanged(false)
	if cancels != 1 {
		t.Fatal("hiding the item did not cancel sampling")
	}
	s.touchBarVisibilityChanged(true)
	pending[0]()
	if len(pending) != 2 {
		t.Fatal("an old visibility generation restarted sampling")
	}
	// Hiding Touch Bar must not interrupt lyrics still shown in the menu bar.
	s.options.MenuBarVisible, s.options.MenuBarLyrics = true, true
	s.touchBarVisibilityChanged(false)
	if cancels != 1 || len(pending) != 2 {
		t.Fatal("Touch Bar interrupted the menu bar sampler")
	}
	s.close()
	pending[1]()
	s.touchBarVisibilityChanged(true)
	if cancels != 2 || len(pending) != 2 || s.touchVisible {
		t.Fatal("closed module accepted native visibility work")
	}
}

// Pausing and loading without an audio player never keep a lyric timer alive.
func TestSystemPlayerTimerRests(t *testing.T) {
	count := 0
	original := systemPlayerAfter
	t.Cleanup(func() { systemPlayerAfter = original })
	systemPlayerAfter = func(_ time.Duration, _ func()) func() { count++; return func() {} }
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 7}})
	a.pl.SetLyrics([]lyrics.Line{{Text: "one"}})
	s := &systemPlayer{a: a, options: systemplayer.Options{MenuBarVisible: true, MenuBarLyrics: true}}
	a.pl.Hold(0)
	s.sync()
	if count != 0 {
		t.Fatal("sampled a player without audio")
	}
	out := audio.NewPlayer(&silence{})
	t.Cleanup(out.Close)
	a.pl.Attach(out)
	a.pl.TogglePlay()
	s.sync()
	if count != 0 {
		t.Fatal("sampled while paused")
	}
}
