package app

import (
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
	case len(s.State.Room.Users) < 2:
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
	if s := a.together.session; s != nil {
		for _, u := range s.State.Room.Users {
			if a.profile == nil || int64(u.UserID) != a.profile.UserID {
				peerURL, peerName = u.AvatarURL, u.Nickname
				break
			}
		}
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
	if compact {
		b.Height(44).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				a.togetherFaces(c, white, 24)
				ui.Text(c, a.togetherStatus()).FontSize(12).TextColor(white.Alpha(0.6)).SingleLine()
			})
		})
		return
	}
	b.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(8).Children(func() {
			a.togetherFaces(c, white, 40)
			ui.Text(c, a.togetherStatus()).FontSize(12).TextColor(white.Alpha(0.6)).SingleLine()
		})
	})
}

func (a *app) togetherInvitation() string {
	s, song := a.together.session, a.pl.Current()
	if s == nil || song == nil || a.profile == nil || s.State.Room.RoomID == "" {
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
}

func (a *app) copyTogetherInvitation() {
	link := a.togetherInvitation()
	if a.together.open && a.together.shareCode != nil {
		link = a.together.shareLink // Copy exactly the invitation shown in the QR.
	}
	if link == "" {
		return
	}
	if err := copyText(link); err != nil {
		a.toast = "复制失败"
	} else {
		a.toast = "邀请链接已复制"
	}
}

// Background polling never changes whether these user actions are available.
func (a *app) togetherMenu(m *ui.Menu) {
	s := a.together.session
	if s == nil {
		if m.Item("一起听…").Chosen() {
			a.together.open = true
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
	if m.Item("邀请好友…").Disabled(s.State.Leaving || a.togetherInvitation() == "").Chosen() {
		a.showTogetherInvitation()
	}
	if m.Item("复制一起听邀请").Disabled(s.State.Leaving || s.State.Room.RoomID == "" || a.pl.Current() == nil).Chosen() {
		a.copyTogetherInvitation()
	}
	if m.Item("重新同步").Disabled(s.State.Leaving || s.State.Syncing).Chosen() {
		s.Retry()
	}
	if m.Item("离开一起听").Disabled(s.State.Leaving).Chosen() {
		s.Leave()
	}
}

func (a *app) togetherDialog(c *ui.Context) {
	t, p := &a.together, a.pal
	_, height := c.Size()
	ui.DialogBase(c.Key("together-dialog"), &t.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(420).Padding(24).Radius(20).Gap(20).Background(p.bg).Border(1, p.hairline).TextColor(p.text)
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			title := "一起听"
			if t.session != nil && t.shareCode != nil {
				title = "邀请好友一起听"
			}
			ui.Text(c, title).FontSize(20).FontWeight(700).Grow(1)
			if iconButton(c.Key("close"), iconX, 16, p.muted, "关闭面板").Clicked() {
				t.open = false
			}
		})
		ui.Column(c).AlignItems(ui.Center).Gap(12).PaddingY(8).Children(func() {
			a.togetherFaces(c, p.text, 52)
			ui.Text(c, a.togetherStatus()).FontSize(13).TextColor(p.muted)
		})
		if s := t.session; s != nil {
			if code := t.shareCode; code != nil {
				ui.Column(c).AlignItems(ui.Center).Gap(12).Children(func() {
					size := min(float32(224), max(160, height-410))
					ui.Box(c.Key("invitation-qr")).Size(size, size).Radius(12).Background(white).
						Label("一起听邀请二维码").Draw(func(pt *ui.Painter, r ui.Rect) { drawQR(pt, r, code, 18) })
					ui.Text(c, "扫码打开邀请，或复制链接发给好友").FontSize(12).TextColor(p.muted)
				})
				if ui.PrimaryButton(c.Key("copy-invitation"), "复制邀请链接").Clicked() {
					a.copyTogetherInvitation()
				}
				return
			}
			// Normally the room lives in the cover and its menu; this also
			// handles a panel that was already open when the room changed.
			if s.State.Error != "" {
				ui.Text(c, s.State.Error).FontSize(13).TextColor(p.muted)
			}
			b := ui.Button(c.Key("room-actions"), "房间选项")
			b.Menu(a.togetherMenu)
			return
		}
		if a.profile == nil {
			if ui.PrimaryButton(c.Key("login"), "登录网易云").Clicked() {
				t.open = false
				a.openLogin()
			}
			return
		}
		ui.Column(c).Gap(10).Children(func() {
			ui.TextInput(c.Key("invitation"), &t.invite).Label("一起听邀请链接").Placeholder("粘贴好友的网易云一起听邀请").FontSize(13)
			if ui.PrimaryButton(c.Key("join"), "加入邀请").Disabled(t.invite == "").Clicked() {
				a.startTogether("join")
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).Height(1).Background(p.hairline)
			ui.Text(c, "或").FontSize(12).TextColor(p.faint)
			ui.Box(c).Grow(1).Height(1).Background(p.hairline)
		})
		if ui.Button(c.Key("create"), "发起一起听，邀请好友").Clicked() {
			a.startTogether("create")
		}
		ui.Row(c).Justify(ui.Center).Children(func() {
			b := ui.ButtonBase(c.Key("resume")).Padding(6, 10).Radius(8).TextColor(p.muted).Label("恢复当前房间")
			b.Children(func() { ui.Text(c, "恢复当前房间").FontSize(12) })
			if b.Clicked() {
				a.startTogether("resume")
			}
		})
		if t.err != "" {
			ui.Text(c, t.err).FontSize(13).TextColor(p.accent)
		}
	})
}
