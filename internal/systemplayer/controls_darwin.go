//go:build darwin

package systemplayer

import "github.com/ebitengine/purego/objc"

const (
	activateAction = "activate:"

	nsImageOnly             int64  = 1
	nsEventTypeLeftMouseUp  int64  = 2
	nsEventTypeRightMouseUp int64  = 4
	mouseUpMask             uint64 = 1<<nsEventTypeLeftMouseUp | 1<<nsEventTypeRightMouseUp
)

// playbackControls shares button behavior; each presentation owns their layout
// and retains the buttons in its container.
type playbackControls struct {
	previous, toggle, next objc.ID
	playing                bool // the toggle starts with the play symbol
}

func newPlaybackControls(button func(string, string, func()) objc.ID, actions Actions) playbackControls {
	return playbackControls{
		previous: button("backward.end.fill", "上一首", actions.Previous),
		toggle:   button("play.fill", "播放", actions.Toggle),
		next:     button("forward.end.fill", "下一首", actions.Next),
	}
}

func (c *playbackControls) buttons() [3]objc.ID {
	return [3]objc.ID{c.previous, c.toggle, c.next}
}

func (c *playbackControls) update(s State) {
	if c.playing != s.Playing {
		symbol, label := "play.fill", "播放"
		if s.Playing {
			symbol, label = "pause.fill", "暂停"
		}
		setSymbol(c.toggle, symbol, label)
	}
	send(c.previous, "setEnabled:", s.CanSkip)
	send(c.next, "setEnabled:", s.CanSkip)
	send(c.toggle, "setEnabled:", s.CanToggle)
	c.playing = s.Playing
}

func (c *playbackControls) close() {
	for _, b := range c.buttons() {
		unbindButton(b)
	}
}

// newNativeButton returns an owned button. The caller lays it out, adds it to
// a container, and releases its ownership.
func newNativeButton(n *nativePlayer, symbol, label string, fn func()) objc.ID {
	b := send(alloc("NSButton"), "initWithFrame:", rect{})
	send(b, "setTitle:", str(""))
	send(b, "setImagePosition:", nsImageOnly)
	setSymbol(b, symbol, label)
	bindButton(b, n, fn)
	return b
}

func bindButton(b objc.ID, n *nativePlayer, click func()) {
	send(b, "setTarget:", n.target)
	send(b, "setAction:", sel(activateAction))
	send(b, "sendActionOn:", mouseUpMask)
	buttonBindings[b] = buttonBinding{click: click, menu: n.actions.Menu}
}

func unbindButton(b objc.ID) {
	delete(buttonBindings, b)
	send(b, "setTarget:", objc.ID(0))
}

func setSymbol(button objc.ID, symbol, label string) {
	img := send(objc.ID(objc.GetClass("NSImage")), "imageWithSystemSymbolName:accessibilityDescription:", str(symbol), str(label))
	send(button, "setImage:", img)
	send(button, "setToolTip:", str(label))
	send(button, "setAccessibilityLabel:", str(label))
}
