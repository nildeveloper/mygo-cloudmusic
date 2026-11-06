package app

import "github.com/egoist/mygo/ui"

type playbackErrorState struct {
	open    bool
	track   int64
	message string
	pane    int
}

// Details belong to one failed selection and one visible player layout.
func (a *app) updatePlaybackError() {
	var track int64
	if t := a.pl.Current(); t != nil {
		track = t.ID
	}
	pane := -1
	if a.nowPlaying {
		pane = a.npPane
	}
	s := &a.playbackError
	if s.track != track || s.message != a.pl.Err() || s.pane != pane {
		*s = playbackErrorState{track: track, message: a.pl.Err(), pane: pane}
	}
}

// A compact, persistent entry near the progress replaces the window-wide error.
// The explanation and recovery actions appear only when requested.
func (a *app) playbackErrorButton(c *ui.Context, color ui.Color) {
	message := a.pl.Err()
	if message == "" {
		return
	}
	b := ui.ButtonBase(c.Key("playback-error")).Height(16).PaddingX(6).Radius(8).Gap(4).
		AlignItems(ui.Center).Background(color.Alpha(0.08)).TextColor(color.Alpha(0.8)).
		Label("查看播放异常").Tooltip(message)
	if b.Hovered() {
		b.Background(color.Alpha(0.15))
	}
	b.Children(func() {
		ui.Text(c, "!").FontSize(11).FontWeight(700)
		ui.Text(c, "播放异常").FontSize(10.5)
	})
	if b.Clicked() {
		a.playbackError.open = !a.playbackError.open
	}
	if a.nowPlaying {
		a.playbackErrorDetails(c, b)
	}
}

func (a *app) playbackErrorDetails(c *ui.Context, b ui.Element) {
	message := a.pl.Err()
	pop, foreground := popoverColors(a.pal.dark), a.pal.text
	if a.nowPlaying {
		pop, foreground = popoverColors(true), white
	}
	ui.PopoverBase(c.Key("playback-error-details"), b, &a.playbackError.open, func(panel ui.Element) {
		panel.AttachTo(b, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 10, 0).
			Width(300).Padding(18).Gap(12).Radius(16).Background(pop.fill).Border(0.5, pop.edge).
			TextColor(foreground).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.25)).Label("播放异常详情")
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "本机播放遇到问题").FontSize(14).FontWeight(600).Grow(1)
			if iconButton(c.Key("close"), iconX, 14, foreground.Alpha(0.6), "关闭异常详情").Clicked() {
				a.playbackError.open = false
			}
		})
		ui.Text(c, message).FontSize(12).TextColor(foreground.Alpha(0.75)).MaxLines(4).Tooltip(message)
		if a.together.session != nil {
			ui.Text(c, "重试仅恢复本机，不会暂停好友").FontSize(11).TextColor(foreground.Alpha(0.5))
		}
		ui.Row(c).Gap(8).Children(func() {
			if pillButton(c.Key("retry"), nil, "重试播放", a.pal.accent, a.pal.onAccent).
				Height(32).Radius(10).Disabled(!a.canRetryPlayback()).Clicked() {
				a.playbackError.open = false
				a.retryPlayback()
			}
			if pillButton(c.Key("copy"), nil, "复制诊断", foreground.Alpha(0.08), foreground.Alpha(0.75)).
				Height(32).Radius(10).Clicked() {
				a.copyPlaybackDiagnostic()
			}
		})
	})
}

func (a *app) canRetryPlayback() bool {
	if a.pl.Err() == "" || a.pl.Loading() {
		return false
	}
	s := a.together.session
	return s == nil || !(s.State.Connecting || s.State.Leaving || s.State.Closed || s.State.Syncing)
}

func (a *app) retryPlayback() {
	if !a.canRetryPlayback() {
		return
	}
	if s := a.together.session; s != nil {
		s.Retry()
	} else {
		a.pl.TogglePlay()
	}
}

func (a *app) copyPlaybackDiagnostic() {
	if details := a.pl.ErrorDetails(); details != "" {
		if err := copyText(details); err != nil {
			a.toast = "复制失败"
		} else {
			a.toast = "播放诊断已复制"
		}
	}
}
