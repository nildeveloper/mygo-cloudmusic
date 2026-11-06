package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

type recordTab int

const (
	recordRecent recordTab = iota
	recordWeek
	recordAll
	recordTabCount
)

type recordData struct {
	tracks   []netease.Track
	plays    []netease.Play // same row order as tracks; recent plays only
	loading  bool
	loadedAt time.Time
	err      string
}

type recordState struct {
	data [recordTabCount]*recordData
}

var listeningRecord = netease.ListeningRecord

func (a *app) loadRecords(tab recordTab, refresh bool) {
	if a.profile == nil || tab < recordRecent || tab >= recordTabCount {
		return
	}
	d := a.records.data[tab]
	if d == nil {
		d = &recordData{}
		a.records.data[tab] = d
	}
	if d.loading || !refresh && !d.loadedAt.IsZero() && time.Since(d.loadedAt) < time.Minute {
		return
	}
	d.loading, d.err = true, ""
	uid := a.profile.UserID
	go func() {
		var tracks []netease.Track
		var plays []netease.Play
		var err error
		if tab == recordRecent {
			plays, err = recentPlays(100)
			tracks = make([]netease.Track, len(plays))
			for i, play := range plays {
				tracks[i] = play.Track
			}
		} else {
			period := netease.RecordWeek
			if tab == recordAll {
				period = netease.RecordAll
			}
			tracks, err = listeningRecord(uid, period)
		}
		a.post(func() { a.finishRecords(uid, tab, d, tracks, plays, err) })
	}()
}

func (a *app) finishRecords(uid int64, tab recordTab, d *recordData, tracks []netease.Track, plays []netease.Play, err error) {
	// Object identity also rejects an earlier login of the same account.
	if a.profile == nil || a.profile.UserID != uid || a.records.data[tab] != d {
		return
	}
	d.loading = false
	if err != nil {
		d.err = "加载失败，请检查网络、登录状态或账号的隐私设置后重试"
		return // Keep an already displayed list on refresh failure.
	}
	d.tracks, d.plays, d.loadedAt, d.err = tracks, plays, time.Now(), ""
}

func (a *app) recordsPage(c *ui.Context) {
	tab := a.route.records
	d := a.records.data[tab]
	if d == nil {
		d = &recordData{} // Signed-out presentation; no I/O while building.
	}
	header := func(c *ui.Context) { a.recordsHeader(c, tab, d) }
	if a.profile == nil {
		a.scrollPage(c, func() {
			header(c)
			ui.Column(c).AlignItems(ui.Center).Gap(14).PaddingY(44).Children(func() {
				ui.Text(c, "登录后查看你的网易云听歌记录").FontSize(13).TextColor(a.pal.muted)
				if ui.PrimaryButton(c, "登录网易云").Clicked() {
					a.openLogin()
				}
			})
		})
		return
	}
	if d.loading && d.loadedAt.IsZero() {
		a.trackSkeleton(c, a.pageShimmer(c), true, header)
		return
	}
	if len(d.tracks) == 0 {
		a.scrollPage(c, func() {
			header(c)
			if d.err == "" {
				message := "暂无最近播放记录"
				if tab != recordRecent {
					message = "这个时段还没有听歌排行"
				}
				a.empty(c, message)
			}
		})
		return
	}
	var meta func(*ui.Context, int)
	if tab == recordRecent {
		meta = func(c *ui.Context, i int) {
			play := d.plays[i]
			label, detail := "—", "播放时间未知"
			if !play.At.IsZero() && play.At.UnixMilli() > 0 {
				label = play.At.In(time.Local).Format("01-02 15:04")
				detail = heardAt(play, c.Now()) + " · " + play.At.In(time.Local).Format("2006-01-02 15:04")
			}
			ui.Text(c, label).Width(104).TextAlign(ui.End).FontSize(12).FontFeatures("tnum").TextColor(a.pal.muted).
				Tooltip(detail)
		}
	}
	a.trackRowsWithMeta(c, d.tracks, true, header, meta)
}

func (a *app) recordsHeader(c *ui.Context, tab recordTab, d *recordData) {
	p := a.pal
	ui.Column(c).Padding(0, 12, 20, 12).Gap(16).Children(func() {
		a.heading(c, "听歌记录", "网易云账号记录")
		ui.Row(c).Children(func() {
			selected := int(tab)
			if segmented(c.Key("record-tabs"), &selected, "最近播放", "最近一周排行", "所有时间排行") {
				a.open(route{page: pageRecords, records: recordTab(selected)})
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			canPlay := false
			for i := range d.tracks {
				canPlay = canPlay || d.tracks[i].Playable()
			}
			if pillButton(c.Key("play-records"), iconPlay, "播放全部", p.accent, p.onAccent).Disabled(!canPlay).Clicked() {
				a.pl.PlayQueue(d.tracks, firstPlayable(d.tracks))
			}
			label := "云端记录"
			if !d.loadedAt.IsZero() {
				label = fmt.Sprintf("%d 首", len(d.tracks))
				if tab == recordRecent {
					label = fmt.Sprintf("%d 条记录", len(d.tracks))
				}
			}
			ui.Text(c, label).FontSize(12).TextColor(p.muted).Grow(1)
			if iconButton(c.Key("refresh-records"), iconReload, 17, p.muted, "刷新听歌记录").Disabled(a.profile == nil || d.loading).Clicked() {
				a.loadRecords(tab, true)
			}
		})
		if d.err != "" {
			ui.Column(c).Gap(8).Children(func() {
				ui.Text(c, d.err).FontSize(12).TextColor(p.muted)
				if ui.Button(c.Key("retry-records"), "重试").Disabled(d.loading).Clicked() {
					a.loadRecords(tab, true)
				}
			})
		} else {
			note := "最近 100 条云端歌曲记录 · 时间与设备以网易云记录为准"
			if tab != recordRecent {
				note = "你的个人听歌排行 · 按网易云统计顺序展示"
			}
			ui.Text(c, note).FontSize(12).TextColor(p.faint)
		}
		if r := a.scrobble; r != nil && r.err != "" {
			ui.Text(c, r.err).FontSize(12).TextColor(p.muted)
		}
	})
}
