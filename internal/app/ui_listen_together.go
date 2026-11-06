package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/listentogether"
)

func (a *app) togetherStatus() string {
	s := a.together.session
	if s == nil {
		return "和朋友，听同一首歌"
	}
	switch {
	case s.State.Connecting:
		return "正在加入一起听…"
	case s.State.Leaving:
		return "正在离开一起听…"
	case s.State.Reconnecting:
		return "正在恢复同步…"
	case a.togetherPeer() == nil:
		return "等待好友加入…"
	default:
		return "正在一起听"
	}
}

func (a *app) togetherFaces(c *ui.Context, color ui.Color, size float32) {
	ownURL, ownName, peerURL, peerName := "", "我", "", "邀请好友"
	if a.profile != nil {
		ownURL, ownName = a.profile.AvatarURL, a.profile.Nickname
	}
	if peer := a.togetherPeer(); peer != nil {
		peerURL, peerName = peer.AvatarURL, peer.Nickname
	}
	if r := a.together.report; r != nil && r.data != nil {
		ownURL, ownName = r.data.User.AvatarURL, r.data.User.Nickname
		peerURL, peerName = r.data.Peer.AvatarURL, r.data.Peer.Nickname
	}
	avatar := func(key, url, name string) {
		ui.Box(c.Key(key)).Size(size, size).Radius(size / 2).Background(color.Alpha(0.08)).Center().Tooltip(name).Children(func() {
			if url != "" {
				a.cover(c, url, 120, size, size/2)
			} else {
				ui.Icon(c, iconUser).Size(size*0.45, size*0.45).TextColor(color.Alpha(0.55))
			}
		})
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		avatar("self", ownURL, ownName)
		ui.Box(c).Width(16).Height(1).Background(color.Alpha(0.2))
		ui.Icon(c, iconHeadset).Size(16, 16).TextColor(color.Alpha(0.5))
		ui.Box(c).Width(16).Height(1).Background(color.Alpha(0.2))
		avatar("peer", peerURL, peerName)
	})
}

// togetherPresence belongs to the cover column, not the window's drag strip.
func (a *app) togetherPresence(c *ui.Context, compact bool) {
	if a.together.session == nil {
		return
	}
	b := ui.ButtonBase(c.Key("together-presence")).Height(82).WidthPercent(100).Radius(14).
		Label("一起听房间").Center().TextColor(white).Menu(a.togetherMenu)
	size := float32(40)
	if compact {
		b.Height(64)
		size = 24
	}

	b.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(8).Children(func() {
			a.togetherFaces(c, white, size)
			ui.Text(c, a.togetherStatus()).FontSize(12).TextColor(white.Alpha(0.6)).SingleLine()
		})
	})
}

func (a *app) togetherInvitation() string {
	s, song := a.together.session, a.pl.Current()
	if s == nil || song == nil || a.profile == nil || s.State.Room.RoomID == "" {
		return ""
	}
	if s.State.Connecting || s.State.Leaving || a.togetherPeer() != nil {
		return ""
	}
	return (listentogether.Invitation{Room: s.State.Room.RoomID, Inviter: a.profile.UserID}).URL(song.ID)
}

func (a *app) showTogetherInvitation() {
	link := a.togetherInvitation()
	if link == "" {
		return
	}
	code, err := qr.Encode(link, qr.M)
	if err != nil {
		a.toast = "暂时无法生成邀请二维码，请复制链接邀请"
		return
	}
	a.together.shareLink, a.together.shareCode = link, code
	a.together.open = true
	a.together.session.WatchMembers(true)
}

func (a *app) dismissTogetherInvitation() {
	if s := a.together.session; s != nil {
		s.WatchMembers(false)
	}
	a.together.open, a.together.shareOnJoin = false, false
	a.together.shareCode, a.together.shareLink = nil, ""
}

func (a *app) copyTogetherInvitation() {
	link := a.togetherInvitation()
	if link == "" {
		return
	}
	if a.together.open && a.together.shareCode != nil {
		link = a.together.shareLink // Copy exactly the invitation shown in the QR.
	}
	if err := copyText(link); err != nil {
		a.toast = "复制失败"
	} else {
		a.toast = "邀请链接已复制"
	}
}

// Polling activity alone never disables these user actions.
func (a *app) togetherMenu(m *ui.Menu) {
	s := a.together.session
	if s == nil {
		if m.Item("一起听…").Chosen() {
			a.openTogether()
		}
		return
	}
	m.Item(a.togetherStatus()).Disabled(true)
	if s.State.Error != "" {
		m.Item(s.State.Error).Disabled(true)
	}
	m.Separator()
	if s.State.Connecting {
		if m.Item("取消连接").Chosen() {
			a.stopTogether()
		}
		return
	}
	if a.pl.Err() != "" {
		if m.Item("重试本机播放").Disabled(!a.canRetryPlayback()).Chosen() {
			a.retryPlayback()
		}
		if m.Item("复制播放诊断").Chosen() {
			a.copyPlaybackDiagnostic()
		}
		m.Separator()
	}
	canInvite := a.togetherInvitation() != ""
	if m.Item("邀请好友…").Disabled(!canInvite).Chosen() {
		a.showTogetherInvitation()
	}
	if m.Item("复制一起听邀请").Disabled(!canInvite).Chosen() {
		a.copyTogetherInvitation()
	}
	if m.Item("重新同步").Disabled(s.State.Leaving || s.State.Syncing).Chosen() {
		s.Retry()
	}
	if m.Item("离开一起听").Disabled(s.State.Leaving).Chosen() {
		s.Leave()
	}
}

// Both room setup and the ending report share one modal frame.
func (a *app) togetherModal(c *ui.Context, key string, body func()) {
	p := a.pal
	width, _ := c.Size()
	ui.DialogBase(c.Key(key), &a.together.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(min(400, width-40)).Padding(28).Radius(24).Gap(20).
			Background(p.bg).Border(1, p.hairline).TextColor(p.text).Shadow(0, 24, 60, -12, p.cardShade)
		body()
	})
}

func (a *app) togetherDialog(c *ui.Context) {
	t, p := &a.together, a.pal
	if r := t.report; r != nil && r.data != nil {
		a.togetherReportDialog(c)
		return
	}
	_, height := c.Size()
	wasOpen := t.open
	a.togetherModal(c, "together-dialog", func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			title := "一起听"
			if t.session != nil && t.shareCode != nil {
				title = "邀请好友"
			} else if t.session == nil && t.joining {
				title = "加入好友"
				if iconButton(c.Key("back"), iconBack, 15, p.muted, "返回一起听").Clicked() {
					t.joining, t.err = false, ""
				}
			}
			ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(p.muted).Grow(1)
			if iconButton(c.Key("close"), iconX, 16, p.muted, "关闭面板").Clicked() {
				t.open = false
			}
		})
		if s := t.session; s != nil {
			if code := t.shareCode; code != nil {
				ui.Column(c).AlignItems(ui.Center).Gap(16).Children(func() {
					ui.Text(c, "打开网易云，扫码加入").FontSize(14).TextColor(p.muted)
					size := min(float32(224), max(160, height-310))
					ui.Box(c.Key("invitation-qr")).Size(size, size).Radius(16).Background(white).Border(1, p.hairline).
						Label("一起听邀请二维码").Draw(func(pt *ui.Painter, r ui.Rect) { drawQR(pt, r, code, 18) })
					a.togetherFaces(c, p.text, 32)
					ui.Text(c, "好友加入后，这里会自动收起").FontSize(12).TextColor(p.muted)
				})
				if pillButton(c.Key("copy-invitation"), nil, "复制邀请链接", p.accent, p.onAccent).
					Height(44).Radius(12).Justify(ui.Center).Clicked() {
					a.copyTogetherInvitation()
				}
				return
			}
			ui.Column(c).AlignItems(ui.Center).Gap(12).Children(func() {
				a.togetherFaces(c, p.text, 44)
				ui.Text(c, a.togetherStatus()).FontSize(13).TextColor(p.muted)
			})
			if s.State.Error != "" {
				ui.Text(c, s.State.Error).FontSize(13).TextColor(p.muted)
			}
			b := ui.Button(c.Key("room-actions"), "房间选项")
			b.Menu(a.togetherMenu)
			return
		}
		a.togetherSetup(c)
		if t.err != "" {
			ui.Text(c, t.err).FontSize(13).TextColor(p.accent)
		}
	})
	if wasOpen && !t.open && t.shareCode != nil {
		a.dismissTogetherInvitation()
	}
}

func (a *app) togetherSetup(c *ui.Context) {
	t, p := &a.together, a.pal
	title, subtitle := "好音乐，一起听", "邀请一位好友，同步播放与切歌"
	if t.joining {
		title, subtitle = "加入好友的一起听", "粘贴好友发来的网易云邀请链接"
	}
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(24).FontWeight(700)
		ui.Text(c, subtitle).FontSize(13).TextColor(p.muted)
	})
	if t.joining {
		in := ui.TextInputBase(c.Key("invitation"), &t.invite).Label("一起听邀请链接").Placeholder("粘贴邀请链接").
			Height(44).PaddingX(14).Radius(12).Background(p.hover).Border(1, p.hairline).FontSize(13).AutoFocus()
		if in.Focused() {
			in.BorderColor(p.accent)
		}
		join := pillButton(c.Key("join"), nil, "加入一起听", p.accent, p.onAccent).
			Height(44).Radius(12).Justify(ui.Center).Disabled(strings.TrimSpace(t.invite) == "")
		if (join.Clicked() || in.Submitted()) && strings.TrimSpace(t.invite) != "" {
			a.startTogether("join")
		}
		return
	}
	track := a.pl.Current()
	canCreate := track != nil && track.ProgramID == 0 && a.pl.Radio() == ""
	ui.Row(c).Padding(12).Gap(12).Radius(14).Background(p.hover).AlignItems(ui.Center).Children(func() {
		if canCreate {
			a.cover(c, track.Album.PicURL, 120, 48, 9)
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, track.Name).FontSize(14).FontWeight(600).SingleLine()
				ui.Text(c, track.ArtistNames()).FontSize(12).TextColor(p.muted).SingleLine()
			})
		} else {
			ui.Icon(c, iconHeadset).Size(24, 24).TextColor(p.muted)
			ui.Text(c, "先选一首普通歌曲，即可发起邀请").FontSize(12).TextColor(p.muted)
		}
	})
	if a.profile == nil {
		if pillButton(c.Key("login"), nil, "登录网易云", p.accent, p.onAccent).Height(44).Radius(12).Justify(ui.Center).Clicked() {
			t.open = false
			a.openLogin()
		}
		return
	}
	if pillButton(c.Key("create"), iconHeadset, "发起一起听", p.accent, p.onAccent).
		Height(44).Radius(12).Justify(ui.Center).Disabled(!canCreate).Clicked() {
		a.startTogether("create")
	}
	ui.Row(c).Justify(ui.Center).Gap(12).Children(func() {
		if pillButton(c.Key("join-mode"), nil, "加入好友", ui.Transparent, p.muted).Height(32).Radius(10).Clicked() {
			t.joining, t.err = true, ""
		}
		ui.Box(c).Width(1).Height(12).Background(p.hairline).AlignSelf(ui.Center)
		if pillButton(c.Key("resume"), nil, "恢复当前房间", ui.Transparent, p.muted).Height(32).Radius(10).Clicked() {
			a.startTogether("resume")
		}
	})
}

func (a *app) togetherReportDialog(c *ui.Context) {
	t, p := &a.together, a.pal
	r := t.report.data
	a.togetherModal(c, "together-report", func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "一起听已结束").FontSize(14).FontWeight(600).TextColor(p.muted).Grow(1)
			if iconButton(c.Key("close"), iconX, 16, p.muted, "关闭总结").Clicked() {
				t.open = false
			}
		})
		ui.Column(c).AlignItems(ui.Center).Gap(10).Children(func() {
			a.togetherFaces(c, p.text, 48)
			name, peer := r.User.Nickname, r.Peer.Nickname
			if name == "" {
				name = "我"
			}
			if peer == "" {
				peer = "好友"
			}
			ui.Text(c, name+" · "+peer).WidthPercent(100).SingleLine().TextAlign(ui.Center).FontSize(13).TextColor(p.muted)
		})
		ui.Column(c).PaddingY(20).Radius(16).Background(p.hover).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "本次陪伴").FontSize(12).TextColor(p.muted)
			ui.Text(c, togetherDuration(*r.Duration)).FontSize(32).FontWeight(700).FontFeatures("tnum").TextAlign(ui.Center)
			ui.Text(c, fmt.Sprintf("一起听过 %d 首歌", *r.SongCount)).FontSize(13).TextColor(p.muted)
		})
		var total []string
		if r.TotalDuration != nil && *r.TotalDuration >= 0 {
			total = append(total, "累计陪伴 "+togetherDuration(*r.TotalDuration))
		}
		if r.TotalSongCount != nil && *r.TotalSongCount >= 0 {
			total = append(total, fmt.Sprintf("听过 %d 首", *r.TotalSongCount))
		}
		if len(total) > 0 {
			ui.Text(c, strings.Join(total, " · ")).FontSize(12).TextColor(p.muted).TextAlign(ui.Center)
		}
		if pillButton(c.Key("done"), nil, "完成", p.accent, p.onAccent).Height(44).Radius(12).Justify(ui.Center).Clicked() {
			t.open = false
		}
	})
	if !t.open {
		a.clearTogetherReport()
	}
}

func togetherDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%d 分钟", seconds/60)
	}
	hours, minutes := seconds/3600, seconds/60%60
	if minutes == 0 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 小时 %d 分", hours, minutes)
}
