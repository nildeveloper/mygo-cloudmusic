// Package systemplayer presents playback in the macOS menu bar and Touch Bar.
// It owns native views, not playback. All methods and callbacks run on the UI thread.
package systemplayer

import "errors"

// State is a value snapshot; the application remains the source of truth.
type State struct {
	TrackID                    int64
	HasTrack, Playing, Loading bool
	CanToggle, CanSkip         bool
	LyricIndex                 int
	LyricText                  string
}

type Options struct {
	MenuBarVisible, MenuBarControls, MenuBarLyrics, TouchBarLyrics bool
}

func (o Options) menuBar() bool { return o.MenuBarVisible && (o.MenuBarControls || o.MenuBarLyrics) }

type Actions struct {
	Previous, Toggle, Next, Menu func()
	// TouchBarVisibilityChanged runs on the UI thread after native visibility changes.
	TouchBarVisibilityChanged func(bool)
}

type Host struct {
	WindowHandle uintptr
	Name         string
	Icon         []byte
}

// Player owns both presentations. Close is idempotent.
type Player struct {
	native     nativePlayer
	state      State
	options    Options
	configured bool
	closed     bool
}

// New binds the host; Configure is the only entry point for enabling presentations.
func New(host Host, actions Actions) (*Player, error) {
	p := &Player{}
	if err := p.native.init(host, actions); err != nil {
		return nil, err
	}
	return p, nil
}

// Configure returns the effective options. Failure of one presentation leaves
// the other available; callers must use the returned options even on error.
func (p *Player) Configure(options Options) (Options, error) {
	if p.closed {
		return Options{}, nil
	}
	if p.configured && p.options == options {
		return p.options, nil
	}
	effective, err := p.native.configure(options)
	p.options = effective
	p.configured = true
	p.native.update(p.state)
	return effective, err
}

// configurePresentations applies each independent resource, including explicit
// disables, even if the other resource fails to initialize.
func configurePresentations(o Options, menu, touch func(bool) error) (Options, error) {
	menuErr := menu(o.menuBar())
	if menuErr != nil {
		o.MenuBarControls, o.MenuBarLyrics = false, false
	}
	touchErr := touch(o.TouchBarLyrics)
	if touchErr != nil {
		o.TouchBarLyrics = false
	}
	return o, errors.Join(menuErr, touchErr)
}

// Update changes content only when needed, but always rechecks native visibility
// and animation after focus or window changes, even if playback is unchanged.
func (p *Player) Update(state State) {
	if p.closed {
		return
	}
	if p.state == state {
		p.native.refresh()
		return
	}
	p.state = state
	p.native.update(state)
}

func (p *Player) Close() {
	if p == nil || p.closed {
		return
	}
	p.closed = true
	p.native.close()
}
