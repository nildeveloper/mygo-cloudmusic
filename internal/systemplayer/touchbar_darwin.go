//go:build darwin

package systemplayer

import (
	"fmt"

	"github.com/ebitengine/purego/objc"
)

const (
	touchBarVisibilityKey = "visible"

	nsUserInterfaceLayoutOrientationHorizontal int64   = 0
	nsLayoutAttributeCenterY                   int64   = 10
	nsBezelStyleRounded                        uint64  = 1
	nsLayoutPriorityDefaultHigh                float32 = 750

	touchHeight        float64 = 30
	touchLyricWidth    float64 = 320
	touchMinLyricWidth float64 = 64
	touchButtonWidth   float64 = 32
	touchIconWidth     float64 = 24
	touchIconSize      float64 = 20
	touchSpacing       float64 = 8
)

type touchBar struct {
	window, bar, previousBar objc.ID
	item, observer           objc.ID
	text                     *nativeText
	controls                 playbackControls
}

func newTouchBar(n *nativePlayer) (*touchBar, error) {
	w := objc.ID(n.host.WindowHandle)
	if w == 0 || !objc.Send[bool](w, sel("respondsToSelector:"), sel("setTouchBar:")) || objc.GetClass("NSTouchBar") == 0 {
		return nil, nativeError("Touch Bar binding")
	}
	iconImage := objc.ID(0)
	if len(n.host.Icon) > 0 {
		white, err := whiteIconPNG(n.host.Icon)
		if err != nil {
			return nil, fmt.Errorf("system player: Touch Bar icon: %w", err)
		}
		iconImage = appIcon(white, touchIconSize)
		// Use the image's white pixels, not NSImageView's template appearance.
		send(iconImage, "setTemplate:", false)
	} else {
		iconImage = appIcon(nil, touchIconSize)
	}
	t := &touchBar{window: w}
	t.bar = send(alloc("NSTouchBar"), "init")
	if t.bar == 0 {
		return nil, nativeError("Touch Bar")
	}
	identifier := str("dev.egoist.cloudmusic.lyrics")
	item := send(alloc("NSCustomTouchBarItem"), "initWithIdentifier:", identifier)
	if item == 0 {
		release(t.bar)
		return nil, nativeError("Touch Bar item")
	}
	t.text = newNativeText(touchLyricWidth, touchHeight, 16)
	send(t.text.label, "setTextColor:", send(objc.ID(objc.GetClass("NSColor")), "whiteColor"))
	row := send(alloc("NSStackView"), "initWithFrame:", frame(0, 0, touchIconWidth+3*touchButtonWidth+4*touchSpacing+touchLyricWidth, touchHeight))
	send(row, "setOrientation:", nsUserInterfaceLayoutOrientationHorizontal)
	send(row, "setAlignment:", nsLayoutAttributeCenterY)
	send(row, "setSpacing:", touchSpacing)
	// A compact, noninteractive app mark precedes the playback buttons.
	icon := send(alloc("NSImageView"), "initWithFrame:", frame(0, 0, touchIconWidth, touchHeight))
	send(icon, "setImage:", iconImage)
	send(icon, "setAccessibilityLabel:", str(n.host.Name))
	send(icon, "setTranslatesAutoresizingMaskIntoConstraints:", false)
	send(send(send(icon, "widthAnchor"), "constraintEqualToConstant:", touchIconWidth), "setActive:", true)
	send(send(send(icon, "heightAnchor"), "constraintEqualToConstant:", touchHeight), "setActive:", true)
	send(row, "addArrangedSubview:", icon)
	release(icon)
	button := func(symbol, label string, fn func()) objc.ID {
		b := newNativeButton(n, symbol, label, fn)
		send(b, "setBezelStyle:", nsBezelStyleRounded)
		send(b, "setTranslatesAutoresizingMaskIntoConstraints:", false)
		send(send(send(b, "widthAnchor"), "constraintEqualToConstant:", touchButtonWidth), "setActive:", true)
		send(send(send(b, "heightAnchor"), "constraintEqualToConstant:", touchHeight), "setActive:", true)
		send(row, "addArrangedSubview:", b)
		release(b)
		return b
	}
	t.controls = newPlaybackControls(button, n.actions)
	send(t.text.clip, "setTranslatesAutoresizingMaskIntoConstraints:", false)
	send(row, "addArrangedSubview:", t.text.clip)
	send(item, "setView:", row)
	send(item, "setCustomizationLabel:", str("播放控制与歌词"))
	release(row)
	// Template items avoid replacing MyGo's window delegate or response chain.
	send(t.bar, "setTemplateItems:", send(objc.ID(objc.GetClass("NSSet")), "setWithObject:", item))
	send(t.bar, "setDefaultItemIdentifiers:", send(objc.ID(objc.GetClass("NSArray")), "arrayWithObject:", identifier))
	// Let the lyrics compress before the buttons when the Control Strip takes
	// more room. The native text reads its actual laid-out width when scrolling.
	preferred := send(send(t.text.clip, "widthAnchor"), "constraintEqualToConstant:", touchLyricWidth)
	send(preferred, "setPriority:", nsLayoutPriorityDefaultHigh)
	send(preferred, "setActive:", true)
	send(send(send(t.text.clip, "widthAnchor"), "constraintGreaterThanOrEqualToConstant:", touchMinLyricWidth), "setActive:", true)
	send(send(send(t.text.clip, "heightAnchor"), "constraintEqualToConstant:", touchHeight), "setActive:", true)
	t.item = item
	return t, nil
}

func (t *touchBar) attach(observer objc.ID) {
	t.observer = observer
	send(t.item, "addObserver:forKeyPath:options:context:", observer, str(touchBarVisibilityKey), uint64(0), uintptr(0))
	t.previousBar = send(t.window, "touchBar")
	if t.previousBar != 0 {
		send(t.previousBar, "retain")
	}
	send(t.window, "setTouchBar:", t.bar)
}

func (t *touchBar) visible() bool {
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	return objc.Send[bool](t.item, sel("isVisible")) && objc.Send[bool](app, sel("isActive")) && objc.Send[bool](t.window, sel("isKeyWindow")) && !objc.Send[bool](t.window, sel("isMiniaturized"))
}

func (t *touchBar) update(s State) {
	t.text.update(s)
	t.controls.update(s)
}

func (t *touchBar) close() {
	send(t.item, "removeObserver:forKeyPath:", t.observer, str(touchBarVisibilityKey))
	t.controls.close()
	// Do not overwrite a binding installed by someone else in the meantime.
	if send(t.window, "touchBar") == t.bar {
		send(t.window, "setTouchBar:", t.previousBar)
	}
	t.text.close()
	release(t.bar)
	release(t.item)
	release(t.previousBar)
}
