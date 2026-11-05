//go:build darwin

package systemplayer

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type point struct{ X, Y float64 }
type size struct{ W, H float64 }
type rect struct {
	Origin point
	Size   size
}

const nsUTF8StringEncoding uint64 = 4

func sel(name string) objc.SEL                          { return objc.RegisterName(name) }
func send(id objc.ID, name string, args ...any) objc.ID { return id.Send(sel(name), args...) }
func alloc(name string) objc.ID                         { return send(objc.ID(objc.GetClass(name)), "alloc") }
func release(id objc.ID) {
	if id != 0 {
		send(id, "release")
	}
}
func pool(fn func()) {
	p := send(alloc("NSAutoreleasePool"), "init")
	defer send(p, "drain")
	fn()
}
func str(s string) objc.ID {
	id := send(alloc("NSString"), "initWithBytes:length:encoding:", unsafe.Pointer(unsafe.StringData(s)), uint64(len(s)), nsUTF8StringEncoding)
	runtime.KeepAlive(s)
	return send(id, "autorelease")
}
func frame(x, y, w, h float64) rect { return rect{point{x, y}, size{w, h}} }

type buttonBinding struct{ click, menu func() }

// All maps are accessed on the AppKit thread. Removing entries before releasing
// views ensures a queued native action cannot call an application after Close.
var buttonBindings = map[objc.ID]buttonBinding{}
var targetPlayers = map[objc.ID]*nativePlayer{}
var targetClass objc.Class
var commonModes, displayOptionsChanged objc.ID

func nativeConstant(name string) (objc.ID, error) {
	address, err := purego.Dlsym(purego.RTLD_DEFAULT, name)
	if err != nil {
		return 0, fmt.Errorf("system player: %s: %w", name, err)
	}
	return **(**objc.ID)(unsafe.Pointer(&address)), nil
}

func registerTarget() error {
	if targetClass != 0 {
		return nil
	}
	var err error
	commonModes, err = nativeConstant("kCFRunLoopCommonModes")
	if err != nil {
		return err
	}
	displayOptionsChanged, err = nativeConstant("NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification")
	if err != nil {
		return err
	}
	c, err := objc.RegisterClass("CloudMusicSystemPlayerTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel(activateAction), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) {
			binding := buttonBindings[sender]
			fn := binding.click
			event := send(send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication"), "currentEvent")
			if event != 0 && objc.Send[int64](event, sel("type")) == nsEventTypeRightMouseUp {
				fn = binding.menu
			}
			if fn != nil {
				fn()
			}
		}},
		{Cmd: sel("observeValueForKeyPath:ofObject:change:context:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, _ objc.ID, _ uintptr) {
			if n := targetPlayers[self]; n != nil {
				n.queueEnvironmentRefresh()
			}
		}},
	})
	if err != nil {
		return err
	}
	targetClass = c
	return nil
}

type nativePlayer struct {
	host                               Host
	actions                            Actions
	options                            Options
	state                              State
	target                             objc.ID
	menu                               *menuBar
	touch                              *touchBar
	timer                              objc.ID
	tick                               objc.Block
	environment                        environmentRefresh
	touchVisible                       bool
	workspaceCenter, workspaceObserver objc.ID
	workspaceBlock                     objc.Block
}

func (n *nativePlayer) init(host Host, actions Actions) error {
	if err := registerTarget(); err != nil {
		return err
	}
	n.host, n.actions = host, actions
	pool(func() {
		n.target = send(send(objc.ID(targetClass), "alloc"), "init")
		targetPlayers[n.target] = n
		workspace := send(objc.ID(objc.GetClass("NSWorkspace")), "sharedWorkspace")
		n.workspaceCenter = send(workspace, "notificationCenter")
		// Workspace notifications use their own center. The main queue also
		// protects against a notification posted from a non-AppKit thread.
		n.workspaceBlock = objc.NewBlock(func(_ objc.Block, _ objc.ID) { n.queueEnvironmentRefresh() })
		n.workspaceObserver = send(n.workspaceCenter, "addObserverForName:object:queue:usingBlock:", displayOptionsChanged, workspace,
			send(objc.ID(objc.GetClass("NSOperationQueue")), "mainQueue"), n.workspaceBlock)
		send(n.workspaceObserver, "retain")
	})
	return nil
}

func (n *nativePlayer) configure(o Options) (effective Options, err error) {
	pool(func() {
		effective, err = configurePresentations(o, n.configureMenu, n.configureTouch)
		n.options = effective
	})
	return effective, err
}

func (n *nativePlayer) configureMenu(enabled bool) error {
	if enabled && n.menu == nil {
		m, err := newMenuBar(n)
		if err != nil {
			return err
		}
		n.menu = m
	} else if !enabled && n.menu != nil {
		n.menu.close()
		n.menu = nil
	}
	return nil
}

func (n *nativePlayer) configureTouch(enabled bool) error {
	if enabled && n.touch == nil {
		t, err := newTouchBar(n)
		if err != nil {
			return err
		}
		n.touch = t
		t.attach(n.target)
	} else if !enabled && n.touch != nil {
		n.touch.close()
		n.touch = nil
	}
	return nil
}

func (n *nativePlayer) update(s State) {
	pool(func() {
		n.state = s
		if n.menu != nil {
			n.menu.update(s, n.options)
		}
		if n.touch != nil {
			n.touch.update(s)
		}
		n.refresh()
	})
}

func (n *nativePlayer) refresh() {
	pool(func() {
		n.animate()
		if n.touchVisible != (n.touch != nil && n.touch.visible()) {
			n.queueEnvironmentRefresh()
		}
	})
}

func postNative(fn func()) {
	pool(func() {
		block := objc.NewBlock(func(_ objc.Block) { fn() })
		send(send(objc.ID(objc.GetClass("NSOperationQueue")), "mainQueue"), "addOperationWithBlock:", block)
		block.Release() // the queued operation owns its copy
	})
}

func (n *nativePlayer) queueEnvironmentRefresh() {
	n.environment.request(postNative, func() {
		pool(func() {
			// Runs even when both the lyric sampler and animation are stopped.
			n.animate()
			visible := n.touch != nil && n.touch.visible()
			if visible != n.touchVisible {
				n.touchVisible = visible
				if fn := n.actions.TouchBarVisibilityChanged; fn != nil {
					fn(visible)
				}
			}
		})
	})
}

func (n *nativePlayer) animate() {
	now := time.Now()
	reduced := objc.Send[bool](send(objc.ID(objc.GetClass("NSWorkspace")), "sharedWorkspace"), sel("accessibilityDisplayShouldReduceMotion"))
	run := n.state.Playing && !n.state.Loading && !reduced
	needed := false
	if n.menu != nil {
		needed = n.menu.text.step(now, run) || needed
	}
	if n.touch != nil {
		needed = n.touch.text.step(now, run && n.touch.visible()) || needed
	}
	if needed && n.timer == 0 {
		// Common modes keep the marquee smooth while a menu tracks the mouse.
		n.tick = objc.NewBlock(func(_ objc.Block, _ objc.ID) { pool(n.animate) })
		n.timer = send(objc.ID(objc.GetClass("NSTimer")), "timerWithTimeInterval:repeats:block:", float64(1.0/30), true, n.tick)
		send(n.timer, "retain")
		send(send(objc.ID(objc.GetClass("NSRunLoop")), "mainRunLoop"), "addTimer:forMode:", n.timer, commonModes)
	} else if !needed {
		n.stopAnimation()
	}
}

func (n *nativePlayer) stopAnimation() {
	if n.timer == 0 {
		return
	}
	send(n.timer, "invalidate")
	release(n.timer)
	n.timer = 0
	n.tick.Release()
}

func (n *nativePlayer) close() {
	pool(func() {
		n.environment.close()
		send(n.workspaceCenter, "removeObserver:", n.workspaceObserver)
		release(n.workspaceObserver)
		n.workspaceBlock.Release()
		delete(targetPlayers, n.target)
		n.stopAnimation()
		if n.menu != nil {
			n.menu.close()
			n.menu = nil
		}
		if n.touch != nil {
			n.touch.close()
			n.touch = nil
		}
		release(n.target)
		n.target = 0
		n.actions = Actions{}
	})
}

// nativeText owns its clip view; its label is retained by that view.
type nativeText struct {
	clip, label              objc.ID
	width, height, textWidth float64
	scroll                   marquee
}

func newNativeText(width, height, fontSize float64) *nativeText {
	v := &nativeText{width: width, height: height}
	v.clip = send(alloc("NSView"), "initWithFrame:", frame(0, 0, width, height))
	send(v.clip, "setWantsLayer:", true)
	send(send(v.clip, "layer"), "setMasksToBounds:", true)
	v.label = send(alloc("NSTextField"), "initWithFrame:", frame(0, 0, width, height))
	for _, name := range []string{"setEditable:", "setSelectable:", "setBordered:", "setDrawsBackground:"} {
		send(v.label, name, false)
	}
	send(v.label, "setFont:", send(objc.ID(objc.GetClass("NSFont")), "systemFontOfSize:", fontSize))
	send(v.label, "setTextColor:", send(objc.ID(objc.GetClass("NSColor")), "labelColor"))
	send(send(v.label, "cell"), "setWraps:", false)
	send(send(v.label, "cell"), "setScrollable:", true)
	send(send(v.label, "cell"), "setUsesSingleLineMode:", true)
	send(v.clip, "addSubview:", v.label)
	release(v.label)
	return v
}

func (v *nativeText) update(s State) {
	if !v.scroll.set(s.TrackID, s.LyricIndex, s.LyricText) {
		return
	}
	send(v.label, "setStringValue:", str(s.LyricText))
	send(v.label, "setToolTip:", str(s.LyricText))
	send(v.label, "sizeToFit")
	measured := objc.Send[rect](v.label, sel("frame"))
	v.textWidth = measured.Size.W
	send(v.label, "setFrame:", frame(0, (v.height-measured.Size.H)/2, max(v.width, v.textWidth), measured.Size.H))
}

// setWidth is used by the menu bar, whose text must not reserve its maximum
// width for short lines. Touch Bar instead lays out the clip with constraints.
func (v *nativeText) setWidth(width float64) {
	if width == v.width {
		return
	}
	v.width = width
	f := objc.Send[rect](v.clip, sel("frame"))
	f.Size.W = width
	send(v.clip, "setFrame:", f)
	f = objc.Send[rect](v.label, sel("frame"))
	f.Size.W = max(width, v.textWidth)
	send(v.label, "setFrame:", f)
}

func (v *nativeText) step(now time.Time, running bool) bool {
	// The Touch Bar can compress the text while preserving its controls.
	v.width = objc.Send[rect](v.clip, sel("bounds")).Size.W
	window := send(v.clip, "window")
	visible := window != 0 && objc.Send[bool](window, sel("isVisible")) && !objc.Send[bool](v.clip, sel("isHiddenOrHasHiddenAncestor"))
	overflow := max(0, v.textWidth-v.width)
	running = running && visible && overflow > 0
	x := v.scroll.step(now, overflow, running)
	f := objc.Send[rect](v.label, sel("frame"))
	if f.Origin.X != -x {
		send(v.label, "setFrameOrigin:", point{-x, f.Origin.Y})
	}
	return running
}

func (v *nativeText) close() {
	send(v.clip, "removeFromSuperview")
	release(v.clip)
}

func nativeError(what string) error { return fmt.Errorf("system player: cannot create %s", what) }

// appIcon decodes and sizes an autoreleased image; each presentation sets its appearance.
// Controls retain it; the caller must have an autorelease pool in scope.
func appIcon(png []byte, points float64) objc.ID {
	var img objc.ID
	if len(png) > 0 {
		data := send(objc.ID(objc.GetClass("NSData")), "dataWithBytes:length:", unsafe.Pointer(&png[0]), uint64(len(png)))
		runtime.KeepAlive(png)
		img = send(alloc("NSImage"), "initWithData:", data)
	}
	if img == 0 {
		// Copy the system image so callers can configure it independently.
		img = send(send(objc.ID(objc.GetClass("NSImage")), "imageWithSystemSymbolName:accessibilityDescription:", str("music.note"), str("音乐")), "copy")
	}
	send(img, "setSize:", size{points, points})
	return send(img, "autorelease")
}
