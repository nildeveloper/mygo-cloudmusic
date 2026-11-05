//go:build darwin && nativeui

package systemplayer

import (
	"fmt"
	"os"
	"testing"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run with: go test -tags nativeui ./internal/systemplayer -count=5
// These tests need a logged-in macOS desktop; they exercise actual AppKit controls.
func TestMain(m *testing.M) {
	done := make(chan int, 1)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		go func() { done <- m.Run(); mygo.App.Quit() }()
	})
	if err := mygo.App.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(<-done)
}

// The app icon must open its menu, and long lyrics must not reserve an oversized status item.
func TestNativeMenuEntryAndCompactLyrics(t *testing.T) {
	mygo.RunOnMain(func() {
		pool(func() {
			menus := 0
			p, err := New(Host{Name: "System player test"}, Actions{Menu: func() { menus++ }})
			if err != nil {
				t.Error(err)
				return
			}
			defer p.Close()
			_, err = p.Configure(Options{MenuBarVisible: true, MenuBarControls: true, MenuBarLyrics: true})
			if err != nil {
				t.Error(err)
				return
			}
			p.Update(State{HasTrack: true, LyricText: "这是一句需要横向滚动而不应挤掉应用图标的歌词"})
			m := p.native.menu
			if length := objc.Send[float64](m.item, sel("length")); length > 228 || length <= 0 {
				t.Errorf("status item length %v", length)
			}
			send(m.menuButton, "performClick:", objc.ID(0))
			if menus != 1 {
				t.Errorf("left click opened the menu %d times", menus)
			}
			p.Update(State{HasTrack: true, LyricText: "短句"})
			if length := objc.Send[float64](m.item, sel("length")); length >= 228 {
				t.Errorf("short lyric did not give space back: %v", length)
			}
		})
	})
}

// Both presentations honor enabled state, share playback actions, and detach on close.
func TestNativePlaybackControlsAndUnbind(t *testing.T) {
	mygo.RunOnMain(func() {
		pool(func() {
			w := mygo.NewWindow(mygo.WindowOptions{Title: "Touch Bar test", Width: 400, Height: 300, Hidden: true, Content: ui.View(func(*ui.Context) {})})
			defer w.Close()
			window := objc.ID(w.NativeHandle())
			before := send(window, "touchBar")
			counts := [3]int{}
			p, err := New(Host{WindowHandle: w.NativeHandle()}, Actions{
				Previous: func() { counts[0]++ }, Toggle: func() { counts[1]++ }, Next: func() { counts[2]++ },
			})
			if err != nil {
				t.Error(err)
				return
			}
			defer p.Close()
			_, err = p.Configure(Options{MenuBarVisible: true, MenuBarControls: true, TouchBarLyrics: true})
			if err != nil {
				t.Error(err)
				return
			}
			bar := p.native.touch
			var buttons []objc.ID
			for _, controls := range []*playbackControls{&p.native.menu.controls, &bar.controls} {
				for _, b := range controls.buttons() {
					// Keep the buttons alive to exercise late clicks after Close.
					send(b, "retain")
					defer release(b)
					buttons = append(buttons, b)
				}
			}
			clickAll := func() {
				for _, b := range buttons {
					send(b, "performClick:", objc.ID(0))
				}
			}
			clickAll()
			if counts != [3]int{} {
				t.Errorf("empty player accepted playback actions: %v", counts)
			}
			p.Update(State{HasTrack: true, CanToggle: true, CanSkip: true, LyricText: "歌词"})
			clickAll()
			if counts != [3]int{2, 2, 2} {
				t.Errorf("playback actions: %v", counts)
			}
			if send(window, "touchBar") != bar.bar {
				t.Error("Touch Bar was not attached")
			}
			p.Close()
			clickAll()
			if counts != [3]int{2, 2, 2} {
				t.Errorf("closed controls accepted playback actions: %v", counts)
			}
			if send(window, "touchBar") != before {
				t.Error("old Touch Bar was not restored")
			}
		})
	})
}
