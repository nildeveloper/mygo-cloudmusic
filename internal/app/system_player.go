package app

import (
	"log"
	"runtime"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/systemplayer"
)

// systemPlayer connects application state to the two native presentations.
// Only this object samples playback; neither native view owns a playback clock.
type systemPlayer struct {
	a            *app
	view         *systemplayer.Player
	options      systemplayer.Options
	touchVisible bool
	closed       bool
	cancel       func()
	generation   uint64
	off          []func()
}

func (a *app) initSystemPlayer() {
	if runtime.GOOS != "darwin" {
		return
	}
	s := &systemPlayer{a: a}
	menu := a.playbackMenu(false)
	run := func(fn func()) func() { return func() { fn(); a.redraw() } }
	v, err := systemplayer.New(systemplayer.Host{WindowHandle: a.win.NativeHandle(), Name: mygo.App.Name(), Icon: trayTemplate}, systemplayer.Actions{
		Previous: run(a.pl.Prev), Toggle: run(a.pl.TogglePlay), Next: run(a.pl.Next),
		Menu:                      func() { menu.Popup(nil) },
		TouchBarVisibilityChanged: s.touchBarVisibilityChanged,
	})
	if err != nil {
		log.Println("system player:", err)
		return
	}
	s.view = v
	a.systemPlayer = s
	s.off = []func(){
		mygo.App.OnDidBecomeActive(s.sync),
		mygo.App.OnDidResignActive(s.sync),
		a.win.OnShow(s.sync), a.win.OnHide(s.sync),
		a.win.OnFocus(s.sync), a.win.OnBlur(s.sync),
	}
	s.sync()
}

func (s *systemPlayer) apply() bool {
	if s == nil || s.closed {
		return false
	}
	cfg := s.a.settings
	o := systemplayer.Options{
		MenuBarVisible: showsTray(cfg), MenuBarControls: cfg.MenuBarControls,
		MenuBarLyrics: cfg.MenuBarLyrics, TouchBarLyrics: cfg.TouchBarLyrics,
	}
	effective, err := s.view.Configure(o)
	if err != nil {
		log.Println("system player:", err)
	}
	s.options = effective
	s.sync()
	return effective.MenuBarVisible && (effective.MenuBarControls || effective.MenuBarLyrics)
}

func (s *systemPlayer) touchBarVisibilityChanged(visible bool) {
	if s.closed {
		return
	}
	s.touchVisible = visible
	s.sync()
}

func (a *app) systemPlayerState() systemplayer.State {
	p := a.pl
	s := systemplayer.State{LyricIndex: -1, CanToggle: len(p.Queue()) > 0, CanSkip: len(p.Queue()) > 0}
	t := p.Current()
	if t == nil {
		return s
	}
	s.TrackID, s.HasTrack = t.ID, true
	s.Playing, s.Loading = p.Playing(), p.Loading()
	s.LyricText = t.Name
	lines, _ := p.Lyrics()
	if !s.Loading {
		s.LyricIndex = lyrics.Index(lines, p.Position())
		if s.LyricIndex >= 0 {
			s.LyricText = lines[s.LyricIndex].Text
		}
	}
	return s
}

// systemPlayerAfter runs on the main thread without invalidating any window.
// Tests replace it with a controlled scheduler, including already queued work.
var systemPlayerAfter = func(d time.Duration, fn func()) func() {
	t := time.AfterFunc(d, func() { mygo.RunOnMain(fn) })
	return func() { t.Stop() }
}

func (s *systemPlayer) sync() {
	if s == nil || s.closed {
		return
	}
	if s.view != nil {
		s.view.Update(s.a.systemPlayerState())
	}
	lines, _ := s.a.pl.Lyrics()
	wantsLyrics := s.options.MenuBarVisible && s.options.MenuBarLyrics ||
		s.options.TouchBarLyrics && s.touchVisible
	if !wantsLyrics || !s.a.pl.Ready() || !s.a.pl.Playing() || s.a.pl.Loading() || len(lines) == 0 {
		s.stopTimer()
		return
	}
	if s.cancel != nil {
		return
	}
	gen := s.generation
	s.cancel = systemPlayerAfter(100*time.Millisecond, func() {
		if s.closed || gen != s.generation {
			return
		}
		s.cancel = nil
		s.sync()
	})
}

func (s *systemPlayer) stopTimer() {
	s.generation++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *systemPlayer) close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	s.stopTimer()
	for _, off := range s.off {
		off()
	}
	s.off = nil
	s.view.Close()
}
