package app

import (
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/listentogether"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
	"rsc.io/qr"
)

type togetherPlayer struct {
	session     *listentogether.Session
	personal    player.Session
	hadPersonal bool
	open        bool
	invite      string
	err         string
	shareLink   string
	shareCode   *qr.Code
	shareOnJoin bool
}

func (a *app) togetherSample() listentogether.Playback {
	p := a.pl
	order, at := p.PlayOrder()
	index := -1
	if at >= 0 && at < len(order) {
		index = order[at]
	}
	mode := "ORDER_LOOP"
	if p.Shuffle() {
		mode = "RANDOM"
	} else if p.Repeat() == player.RepeatOne {
		mode = "SINGLE_LOOP"
	}
	ready := p.Ready() && !p.Loading()
	return listentogether.Playback{
		Mode: mode, Queue: slices.Clone(p.Queue()), Order: slices.Clone(order), Index: index,
		Ready: ready, Playing: p.Playing() && ready, Position: p.Position(), Sampled: time.Now(),
	}
}
func (a *app) startTogether(mode string) {
	t := &a.together
	if t.session != nil {
		return
	}
	if a.profile == nil {
		t.open = false
		a.openLogin()
		return
	}
	var invite listentogether.Invitation
	if mode == "join" {
		var err error
		invite, err = listentogether.ParseInvitation(t.invite)
		if err != nil {
			t.err = err.Error()
			return
		}
	}
	seed := a.togetherSample()
	if mode == "create" {
		if seed.Song() == 0 || a.pl.Radio() != "" {
			t.err = "请先播放普通歌曲，再发起一起听"
			return
		}
		for _, tr := range seed.Queue {
			if tr.ProgramID != 0 {
				t.err = "一起听暂不支持播客队列"
				return
			}
		}
	}
	t.personal, t.hadPersonal = a.pl.Session()
	t.err = ""
	t.shareOnJoin = mode == "create"
	a.radioStarting = ""
	var session *listentogether.Session
	session = listentogether.New(netease.NewListenClient(), a.profile.UserID, a.post, a.togetherSample, func(next *listentogether.Playback) {
		a.togetherChanged(session, next)
	})
	t.session = session
	a.pl.Clear() // Invalidate the old radio, loads and seeks before external control.
	a.pl.Control = a.togetherCommand
	t.open = false
	session.Start(mode, invite, seed)
}
func (a *app) togetherChanged(session *listentogether.Session, next *listentogether.Playback) {
	if a.together.session != session {
		return
	}
	if session.State.Closed {
		err, joined := session.State.Error, session.State.Room.RoomID != ""
		a.stopTogether()
		if joined {
			a.toast = "一起听已结束"
		} else if err != "" {
			a.together.err, a.together.open = err, true
		}
		return
	}
	if next != nil {
		a.pl.Apply(player.Playback{
			Queue: next.Queue, Order: next.Order, Index: next.Index, Mode: next.Mode,
			Playing: next.Playing, Position: next.At(time.Now()),
		})
		if a.together.shareOnJoin {
			a.together.shareOnJoin = false
			a.showTogetherInvitation()
		}
	}
}

func (a *app) stopTogether() {
	t := &a.together
	if t.session == nil {
		return
	}
	t.session.Close()
	t.open, t.shareOnJoin = false, false
	t.shareLink, t.shareCode, t.err = "", nil, ""
	a.pl.Control = nil
	if a.quitting {
		a.saveSession()
		a.pl.Clear()
		t.session = nil
		return
	}
	if t.hadPersonal {
		a.pl.Restore(t.personal)
		if t.personal.Radio != "" {
			a.pl.AttachRadio(t.personal.Radio, a.radioFeed(t.personal.Radio))
		}
	} else {
		a.pl.Clear()
	}
	a.saveSession()
	t.session = nil
	t.personal = player.Session{}
	t.hadPersonal = false
}
func (a *app) togetherCommand(c player.Command) bool {
	s := a.together.session
	if s == nil {
		return false
	}
	action := listentogether.Action{}
	switch c.Kind {
	case "toggle":
		action.Kind = "TOGGLE"
	case "playing":
		action.Kind = "PAUSE"
		if c.Playing {
			action.Kind = "PLAY"
		}
	case "seek":
		action.Kind = "PROGRESS"
		action.Position = c.Position
	case "next":
		action.Kind = "NEXT"
	case "prev":
		action.Kind = "PREV"
	case "ended":
		action.Kind = "ENDED"
	case "jump":
		q := a.pl.Queue()
		if c.Index < 0 || c.Index >= len(q) {
			return true
		}
		action.Kind = "GOTO"
		action.Song = q[c.Index].ID
	case "queue":
		if len(c.Queue) == 0 {
			return true
		}
		for _, t := range c.Queue {
			if t.ProgramID != 0 {
				a.toast = "请先结束一起听，再播放播客"
				return true
			}
		}
		action.Kind = "REPLACE"
		action.Queue = c.Queue
		action.Song = c.Queue[min(max(c.Index, 0), len(c.Queue)-1)].ID
	default:
		a.toast = "请先结束一起听，再切换电台或播放模式"
		return true
	}
	s.Submit(action)
	return true
}
func (a *app) togetherButton(c *ui.Context, color ui.Color) {
	if a.together.session != nil {
		color = a.pal.accent
	}
	b := iconButton(c.Key("listen-together"), iconHeadset, 16, color, "一起听")
	if a.together.session != nil {
		b.Menu(a.togetherMenu)
	} else if b.Clicked() {
		a.together.open = true
	}
}
