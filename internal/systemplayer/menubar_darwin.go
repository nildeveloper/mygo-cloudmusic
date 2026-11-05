//go:build darwin

package systemplayer

import "github.com/ebitengine/purego/objc"

const (
	menuTextHeight float64 = 22
	menuInset      float64 = 4
	menuIconSize   float64 = 18
)

type menuBar struct {
	item, root, menuButton objc.ID
	text                   *nativeText
	controls               playbackControls
}

func newMenuBar(n *nativePlayer) (*menuBar, error) {
	m := &menuBar{}
	bar := send(objc.ID(objc.GetClass("NSStatusBar")), "systemStatusBar")
	m.item = send(bar, "statusItemWithLength:", menuButtonWidth+2*menuInset)
	if m.item == 0 {
		return nil, nativeError("menu bar item")
	}
	send(m.item, "retain")
	send(m.item, "setAutosaveName:", str("cloudmusic-system-player"))
	m.root = send(m.item, "button")
	m.text = newNativeText(menuLyricLimit, menuTextHeight, 13)
	send(m.root, "addSubview:", m.text.clip)
	button := func(symbol, label string, fn func()) objc.ID {
		b := newNativeButton(n, symbol, label, fn)
		send(b, "setBordered:", false)
		send(m.root, "addSubview:", b)
		release(b)
		return b
	}
	m.controls = newPlaybackControls(button, n.actions)
	m.menuButton = button("music.note", n.host.Name+"菜单", n.actions.Menu)
	icon := appIcon(n.host.Icon, menuIconSize)
	send(icon, "setTemplate:", true)
	send(m.menuButton, "setImage:", icon)
	// Preserve the original tray behavior: either click opens its menu.
	bindButton(m.root, n, n.actions.Menu)
	return m, nil
}

func (m *menuBar) update(s State, o Options) {
	m.text.update(s)
	m.text.setWidth(menuLyricWidth(m.text.textWidth))
	height := objc.Send[rect](m.root, sel("bounds")).Size.H
	if height < menuIconSize {
		height = menuTextHeight
	}
	x := menuInset
	showLyrics := o.MenuBarLyrics && s.HasTrack
	send(m.text.clip, "setHidden:", !showLyrics)
	if showLyrics {
		send(m.text.clip, "setFrameOrigin:", point{x, (height - menuTextHeight) / 2})
		x += m.text.width + menuInset
	}
	for _, b := range m.controls.buttons() {
		send(b, "setHidden:", !o.MenuBarControls)
		if o.MenuBarControls {
			send(b, "setFrame:", frame(x, 0, menuButtonWidth, height))
			x += menuButtonWidth
		}
	}
	send(m.menuButton, "setFrame:", frame(x, 0, menuButtonWidth, height))
	send(m.item, "setLength:", x+menuButtonWidth+menuInset)
	m.controls.update(s)
}

func (m *menuBar) close() {
	m.controls.close()
	unbindButton(m.menuButton)
	unbindButton(m.root)
	m.text.close()
	send(send(objc.ID(objc.GetClass("NSStatusBar")), "systemStatusBar"), "removeStatusItem:", m.item)
	release(m.item)
}
