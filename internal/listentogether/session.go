// Package listentogether coordinates one official two-person room. Its mutable
// state and callbacks belong to the UI thread; only API calls run on workers.
package listentogether

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
)

type API interface {
	Status(context.Context) (netease.ListenStatus, error)
	Create(context.Context) (netease.ListenRoom, error)
	Join(context.Context, string, int64) error
	Snapshot(context.Context, string) (netease.ListenSnapshot, error)
	Heartbeat(context.Context, string, int64, bool, int64) (netease.ListenHeartbeat, error)
	Play(context.Context, string, netease.ListenCommand) error
	Queue(context.Context, string, int64, int64, []int64) error
	End(context.Context, string) error
	Tracks(context.Context, []int64) ([]netease.Track, error)
}

type Playback struct {
	Instance uint64 // changes on a new song or a fresh GOTO, not a resync
	Queue    []netease.Track
	Order    []int
	Index    int  // index in Queue
	Ready    bool // local samples have a usable decoder position
	Playing  bool
	Position time.Duration
	Sampled  time.Time
	Mode     string
}

func (p Playback) Song() int64 {
	if p.Index >= 0 && p.Index < len(p.Queue) {
		return p.Queue[p.Index].ID
	}
	return 0
}
func (p Playback) At(now time.Time) time.Duration {
	pos := p.Position
	if p.Playing && !p.Sampled.IsZero() {
		pos += max(0, now.Sub(p.Sampled))
	}
	return max(0, pos)
}

type State struct {
	Room                                            netease.ListenRoom
	Connecting, Busy, Reconnecting, Leaving, Closed bool
	Syncing                                         bool // explicit user request, not background polling
	Error                                           string
}

type Action struct {
	Kind       string // PLAY, PAUSE, PROGRESS, NEXT, PREV, GOTO, REPLACE, ENDED
	Song       int64
	Position   time.Duration
	Queue      []netease.Track
	trackEpoch uint64 // playback instance when a track-specific action was submitted
}

type Session struct {
	State        State
	api          API
	uid          int64
	post         func(func())
	sample       func() Playback
	changed      func(*Playback)
	ctx          context.Context
	cancel       context.CancelFunc
	timer        *time.Timer
	beatTimer    *time.Timer
	memberTimer  *time.Timer
	checking     bool
	watchMembers bool
	beating      bool
	beatSong     int64 // room song before metadata/audio is ready
	epoch        uint64
	trackEpoch   uint64
	seq, version int64
	playback     Playback
	last         *netease.ListenCommand
	pending      *netease.ListenCommand // accepted write awaiting a fresh snapshot
	confirmAt    time.Time
	lastBeat     time.Time
	lastRead     time.Time
	serverTime   int64
	clockAt      time.Time
	clockAligned bool
	failures     int
	alignMisses  int
	commands     []Action
}

var errPlaybackPending = errors.New("正在等待房间播放状态")
var errCommandPending = errors.New("正在等待操作确认")

// A successful write can precede its readable snapshot. Wait without replaying
// the write, dropping queued controls or presenting a normal operation as an outage.
func (s *Session) waitConfirmation() {
	if time.Since(s.confirmAt) >= 8*time.Second {
		s.pending = nil
		s.fail(fmt.Errorf("暂未确认播放操作，请重新同步后重试"), time.Second)
		return
	}
	s.changed(nil)
	s.schedule(250 * time.Millisecond)
}

func confirmsCommand(before, sent, next *netease.ListenCommand) bool {
	if next == nil || !validCommand(*next) {
		return false
	}
	if before != nil && (sameCommand(*before, *next) || olderCommand(before, *next)) {
		return false
	}
	return sent == nil || next.User != sent.User || next.ClientSeq >= sent.ClientSeq
}

func New(api API, uid int64, post func(func()), sample func() Playback, changed func(*Playback)) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{
		api: api, uid: uid, post: post, sample: sample, changed: changed,
		ctx: ctx, cancel: cancel,
		seq: time.Now().UnixMilli(), version: time.Now().UnixMilli(),
	}
}

// Start explicitly creates, joins, or resumes a room. Joining never publishes
// the listener's old queue; existing membership must be resumed explicitly.
func (s *Session) Start(mode string, inv Invitation, seed Playback) {
	s.State.Connecting, s.State.Busy = true, true
	s.changed(nil)
	go func() {
		var room netease.ListenRoom
		status, err := s.api.Status(s.ctx)
		if err == nil {
			switch {
			case mode == "resume":
				if !status.InRoom || status.RoomInfo == nil {
					err = fmt.Errorf("当前没有可恢复的一起听房间")
				} else {
					room = *status.RoomInfo
				}
			case status.InRoom:
				err = fmt.Errorf("账号已在一起听，请选择恢复当前房间")
			case mode == "create":
				room, err = s.api.Create(s.ctx)
				if err == nil && room.CreatorID == 0 {
					room.CreatorID = netease.ListenID(s.uid)
				}
			case mode == "join":
				err = s.api.Join(s.ctx, inv.Room, inv.Inviter)
				if err == nil {
					room.RoomID = inv.Room
					status, err = s.api.Status(s.ctx)
					if err == nil {
						if !status.InRoom || status.RoomInfo == nil || status.RoomInfo.RoomID != inv.Room {
							err = fmt.Errorf("未确认加入目标房间")
						} else {
							room = *status.RoomInfo
						}
					}
				}
			default:
				err = fmt.Errorf("未知一起听操作")
			}
		}
		if err == nil && s.ctx.Err() != nil {
			err = s.ctx.Err()
		}
		if err == nil && mode == "create" {
			ids := trackIDs(seed.Queue)
			err = s.api.Queue(s.ctx, room.RoomID, s.uid, s.version, ids)
			if err == nil {
				cmd := netease.ListenCommand{
					Type: "GOTO", Status: "PAUSE", Song: netease.ListenID(seed.Song()), Former: -1,
					Progress: seed.Position.Milliseconds(), ClientSeq: s.seq,
				}
				if seed.Playing {
					cmd.Status = "PLAY"
				}
				err = s.api.Play(s.ctx, room.RoomID, cmd)
			}
		}
		s.post(func() {
			if s.ctx.Err() != nil {
				if room.RoomID != "" && mode != "resume" {
					s.cleanup(room.RoomID)
				}
				return
			}
			s.State.Connecting, s.State.Busy = false, false
			if err != nil {
				// Roll back partial startup rather than keeping an uninitialized
				// room alive. A lost write response is not safe to blindly retry.
				s.State.Error = err.Error()
				if room.RoomID != "" && mode != "resume" {
					s.cleanup(room.RoomID)
				}
				s.Close()
				s.changed(nil)
				return
			}
			s.State.Room = room
			if mode == "create" {
				s.beatSong = seed.Song()
			}
			s.playback = Playback{Index: -1}
			s.scheduleBeat(0)
			s.poll()
		})
	}()
}

func trackIDs(tracks []netease.Track) []int64 {
	ids := make([]int64, len(tracks))
	for i, t := range tracks {
		ids[i] = t.ID
	}
	return uniqueIDs(ids)
}
func terminal(err error) bool {
	var e *netease.ResponseError
	return errors.As(err, &e) && (e.Code == 301 || e.Code == 488)
}

func (s *Session) fail(err error, delay time.Duration) {
	s.commands = nil
	s.State.Syncing = false
	s.State.Error = err.Error()
	s.State.Reconnecting = true
	if terminal(err) {
		s.State.Closed = true
		s.cancel()
		s.stopTimers()
	}
	s.changed(nil)
	s.schedule(delay)
}

func (s *Session) cleanup(room string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_ = s.api.End(ctx, room)
	}()
}

// Close cancels local work immediately. The captured API login makes cleanup
// safe even when the application is signing in as another account.
func (s *Session) Close() {
	if s.State.Closed {
		return
	}
	s.State.Closed = true
	s.cancel()
	s.epoch++
	s.stopTimers()
	s.commands = nil
	if s.State.Room.RoomID != "" {
		s.cleanup(s.State.Room.RoomID)
	}
}

// Leave reports an explicit end failure and retains the room so it can be retried.
func (s *Session) Leave() {
	if s.State.Closed || s.State.Leaving {
		return
	}
	s.State.Leaving = true
	s.State.Syncing = false
	s.State.Error = ""
	s.commands = nil
	s.epoch++
	s.stopTimers()
	s.changed(nil)
	s.drain()
}

func (s *Session) endRoom() {
	s.State.Busy = true
	room := s.State.Room.RoomID
	go func() {
		err := s.api.End(s.ctx, room)
		s.post(func() {
			if s.ctx.Err() != nil {
				return
			}
			s.State.Busy, s.State.Leaving = false, false
			if err != nil && !terminal(err) {
				s.fail(err, 3*time.Second)
				s.scheduleBeat(0)
				return
			}
			s.State.Closed = true
			s.cancel()
			s.changed(nil)
		})
	}()
}

func (s *Session) stopTimers() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.beatTimer != nil {
		s.beatTimer.Stop()
		s.beatTimer = nil
	}
	if s.memberTimer != nil {
		s.memberTimer.Stop()
		s.memberTimer = nil
	}
}

func (s *Session) schedule(delay time.Duration) {
	if s.State.Closed || s.ctx.Err() != nil {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(delay, func() {
		s.post(func() {
			if !s.State.Closed && !s.State.Busy {
				s.poll()
			}
		})
	})
}

// Maintenance has its own timer and request budget. Playback reads/writes
// remain serialized, but neither can delay room keepalive or wait for it.
func (s *Session) scheduleBeat(delay time.Duration) {
	if s.State.Closed || s.State.Leaving || s.ctx.Err() != nil || s.State.Room.RoomID == "" {
		return
	}
	if s.beatTimer != nil {
		s.beatTimer.Stop()
	}
	s.beatTimer = time.AfterFunc(delay, func() { s.post(s.heartbeat) })
}

func (s *Session) heartbeat() {
	if s.State.Closed || s.State.Leaving || s.ctx.Err() != nil {
		return
	}
	if delay := 10*time.Second - time.Since(s.lastBeat); delay > 0 {
		s.scheduleBeat(delay)
		return
	}
	s.scheduleBeat(10 * time.Second)
	if s.beating {
		return
	}
	local := s.sample()
	if local.Song() == 0 {
		song := s.playback.Song()
		if song == 0 {
			song = s.beatSong
		}
		if song == 0 {
			s.scheduleBeat(2 * time.Second)
			return
		}
		local = Playback{Queue: []netease.Track{{ID: song}}, Index: 0}
	}
	s.beating = true
	s.lastBeat = time.Now()
	room := s.State.Room.RoomID
	s.members()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
		defer cancel()
		start := time.Now()
		h, err := s.api.Heartbeat(ctx, room, local.Song(), local.Playing, local.Position.Milliseconds())
		clockAt := start.Add(time.Since(start) / 2)
		s.post(func() {
			s.beating = false
			if s.ctx.Err() != nil || s.State.Leaving {
				return
			}
			if terminal(err) {
				s.fail(err, 0)
				return
			}
			if h.Time > 0 {
				s.serverTime, s.clockAt = h.Time, clockAt
			}
			if err != nil {
				s.State.Error = err.Error()
			}
			s.changed(nil)
		})
	}()
}

// WatchMembers accelerates only membership reads while an invitation is shown.
// Heartbeats and playback commands keep their own cadence and request budgets.
func (s *Session) WatchMembers(watch bool) {
	if s.watchMembers == watch {
		return
	}
	s.watchMembers = watch
	if s.memberTimer != nil {
		s.memberTimer.Stop()
		s.memberTimer = nil
	}
	if watch {
		s.members()
	}
}

func (s *Session) members() {
	if s.State.Closed || s.State.Leaving || s.checking || s.State.Room.RoomID == "" || s.ctx.Err() != nil {
		return
	}
	s.checking = true
	if s.memberTimer != nil {
		s.memberTimer.Stop()
		s.memberTimer = nil
	}
	room := s.State.Room.RoomID
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		defer cancel()
		status, err := s.api.Status(ctx)
		if err == nil && (!status.InRoom || status.RoomInfo == nil || status.RoomInfo.RoomID != room) {
			err = &netease.ResponseError{Code: 488, Message: "一起听已结束"}
		}
		s.post(func() {
			s.checking = false
			if s.ctx.Err() != nil || s.State.Leaving {
				return
			}
			if terminal(err) {
				s.fail(err, 0)
				return
			}
			delay := time.Second
			if err != nil {
				s.State.Error = err.Error()
				delay = 3 * time.Second
			} else {
				updated := *status.RoomInfo
				if updated.CreatorID == 0 {
					updated.CreatorID = s.State.Room.CreatorID
				}
				s.State.Room = updated
			}
			s.changed(nil)
			if s.watchMembers && !s.State.Closed && !s.State.Leaving {
				s.memberTimer = time.AfterFunc(delay, func() {
					s.post(func() {
						if s.watchMembers {
							s.members()
						}
					})
				})
			}
		})
	}()
}

func (s *Session) Retry() {
	if s.State.Closed || s.State.Connecting || s.State.Leaving || s.State.Syncing {
		return
	}
	s.State.Reconnecting, s.State.Syncing = true, true
	s.commands = nil
	s.epoch++
	s.changed(nil)
	s.drain()
}

// Playing is the last confirmed room state, independent of this device's output.
func (s *Session) Playing() bool { return s.playback.Playing }

func (s *Session) poll() {
	if s.State.Busy || s.State.Closed || s.State.Room.RoomID == "" {
		return
	}
	s.State.Busy = true
	room := s.State.Room.RoomID
	local := s.sample()
	previous := s.playback
	prior := s.last
	pending := s.pending
	if needsAlignment(local, previous, time.Now()) {
		s.alignMisses++
	} else {
		s.alignMisses = 0
	}
	resync := s.State.Reconnecting || s.alignMisses >= 2 || time.Since(s.lastRead) > 15*time.Second || !s.clockAt.IsZero() && !s.clockAligned
	epoch := s.epoch
	serverTime, clockAt := s.serverTime, s.clockAt
	needBeatSong := s.beatTimer != nil && s.beatSong == 0 && previous.Song() == 0
	go func() {
		snap, err := s.api.Snapshot(s.ctx, room)
		if err == nil && needBeatSong && snap.Command != nil && validCommand(*snap.Command) {
			// Keep the member alive while the initial track metadata loads.
			song := int64(snap.Command.Song)
			s.post(func() {
				if s.ctx.Err() == nil && !s.State.Leaving && epoch == s.epoch {
					s.beatSong = song
					s.heartbeat()
				}
			})
		}
		var next *Playback
		// Commands are not samples: only resync, a new command or a queue
		// change can apply playback. Pending writes still need confirmation.
		if err == nil {
			cmd := snap.Command
			switch {
			case pending != nil && !confirmsCommand(prior, pending, cmd):
				err = errCommandPending
			case cmd == nil || !validCommand(*cmd):
				err = errPlaybackPending
			case olderCommand(prior, *cmd):
				if resync {
					err = errors.New("等待房间最新播放状态")
				}
			case resync || prior == nil || !sameCommand(*prior, *cmd) || queueChanged(previous, snap):
				var p Playback
				p, err = resolve(s.ctx, s.api, previous, prior, snap, serverTime, clockAt)
				if err == nil {
					next = &p
				}
			}
		}
		s.post(func() {
			if s.ctx.Err() != nil {
				return
			}
			s.State.Busy = false
			if s.State.Leaving || epoch != s.epoch {
				s.drain() // A local command superseded this read.
				return
			}
			if err != nil {
				if errors.Is(err, errCommandPending) {
					s.waitConfirmation()
					return
				}
				if errors.Is(err, errPlaybackPending) {
					if s.last != nil && time.Since(s.lastRead) < 8*time.Second {
						s.schedule(500 * time.Millisecond)
						return
					}
					s.fail(err, 2*time.Second)
					return
				}
				s.failures++
				s.fail(err, time.Duration(min(30, 2<<min(s.failures, 4)))*time.Second)
				return
			}
			s.pending = nil
			s.failures = 0
			s.lastRead = time.Now()
			s.State.Error = ""
			s.State.Reconnecting = false
			s.State.Syncing = false
			if next != nil {
				*next = s.accept(*next, *snap.Command)
				s.clockAligned = s.clockAligned || !clockAt.IsZero()
			}
			s.changed(next)
			s.drain()
		})
	}()
}

// Submit records user intent, not Changed notifications. All writes and reads
// are serialized, so a delayed response cannot overwrite a later local write.
func (s *Session) Submit(a Action) {
	if s.State.Closed || s.State.Leaving {
		return
	}
	if s.State.Connecting || s.State.Room.RoomID == "" || s.State.Reconnecting {
		s.State.Error = "正在连接房间，请稍候"
		s.changed(nil)
		return
	}
	if a.Kind == "ENDED" && int64(s.State.Room.CreatorID) != s.uid {
		if !s.State.Busy {
			s.poll()
		}
		return
	}
	if len(s.commands) >= 8 {
		s.State.Error = "操作较快，请等待同步"
		s.changed(nil)
		return
	}
	a.Queue = slices.Clone(a.Queue)
	a.trackEpoch = s.trackEpoch
	if a.Kind == "PROGRESS" && len(s.commands) > 0 && s.commands[len(s.commands)-1].Kind == a.Kind {
		s.commands[len(s.commands)-1] = a
	} else {
		s.commands = append(s.commands, a)
	}
	s.epoch++
	s.drain()
}
func (s *Session) drain() {
	if s.State.Closed || s.State.Busy {
		return
	}
	if s.State.Leaving {
		s.endRoom()
		return
	}
	if s.State.Syncing || s.pending != nil {
		s.poll()
		return
	}
	// Track actions belong to their original playback instance. A late EOF
	// must also yield to a pause confirmed while that event was queued.
	for len(s.commands) > 0 {
		a := s.commands[0]
		stale := (a.Kind == "PROGRESS" || a.Kind == "ENDED") && a.trackEpoch != s.trackEpoch
		paused := a.Kind == "ENDED" && !s.playback.Playing
		if !stale && !paused {
			break
		}
		s.commands = s.commands[1:]
	}
	if len(s.commands) == 0 {
		s.schedule(2 * time.Second)
		return
	}
	a := s.commands[0]
	s.commands = s.commands[1:]
	p := s.playback
	if a.Kind == "TOGGLE" {
		a.Kind = "PLAY"
		if p.Playing {
			a.Kind = "PAUSE"
		}
	}
	if a.Kind == "REPLACE" {
		p.Queue = slices.Clone(a.Queue)
		p.Order = make([]int, len(p.Queue))
		for i := range p.Order {
			p.Order[i] = i
		}
		p.Mode = "ORDER_LOOP"
		p.Index = 0
	}
	if p.Song() == 0 || len(p.Queue) == 0 {
		s.State.Error = "等待有效播放队列"
		s.commands = nil
		s.changed(nil)
		s.schedule(time.Second)
		return
	}
	local := s.sample()
	pos := p.At(time.Now())
	if local.Ready && local.Song() == p.Song() {
		pos = local.Position
	}
	cmd := netease.ListenCommand{
		Type: a.Kind, Song: netease.ListenID(p.Song()), Former: netease.ListenID(s.playback.Song()),
		Status: "PAUSE", Progress: pos.Milliseconds(), User: netease.ListenID(s.uid),
	}
	if p.Playing {
		cmd.Status = "PLAY"
	}
	switch a.Kind {
	case "PLAY", "PAUSE":
		cmd.Status = a.Kind
	case "PROGRESS":
		cmd.Progress = max(0, a.Position.Milliseconds())
	case "NEXT", "PREV", "ENDED":
		i := slices.Index(p.Order, p.Index)
		if i < 0 {
			i = 0
		}
		delta := 1
		if a.Kind == "PREV" {
			delta = -1
		}
		if a.Kind != "ENDED" || p.Mode != "SINGLE_LOOP" {
			i = (i + delta + len(p.Order)) % len(p.Order)
		}
		p.Index = p.Order[i]
		cmd.Type = "GOTO"
		cmd.Song = netease.ListenID(p.Song())
		cmd.Progress = 0
		cmd.Status = "PLAY"
	case "GOTO", "REPLACE":
		i := slices.IndexFunc(p.Queue, func(t netease.Track) bool { return t.ID == a.Song })
		if i < 0 {
			s.State.Error = "歌曲不在房间队列中"
			s.commands = nil
			s.changed(nil)
			s.schedule(time.Second)
			return
		}
		p.Index = i
		cmd.Type = "GOTO"
		cmd.Song = netease.ListenID(a.Song)
		cmd.Progress = 0
		cmd.Status = "PLAY"
	default:
		s.commands = nil
		s.schedule(time.Second)
		return
	}
	s.seq++
	cmd.ClientSeq = s.seq
	s.version++
	room, version := s.State.Room.RoomID, s.version
	serverTime, clockAt := s.serverTime, s.clockAt
	before := s.last
	s.State.Busy = true
	s.changed(nil)
	go func() {
		var err error
		if a.Kind == "REPLACE" {
			err = s.api.Queue(s.ctx, room, s.uid, version, trackIDs(p.Queue))
		}
		if err == nil {
			err = s.api.Play(s.ctx, room, cmd)
		}
		wrote := err == nil
		// Read the acknowledged state: another participant may have raced our write.
		var snap netease.ListenSnapshot
		var next Playback
		if err == nil {
			snap, err = s.api.Snapshot(s.ctx, room)
		}
		if err == nil {
			if !confirmsCommand(before, &cmd, snap.Command) {
				err = errCommandPending
			} else {
				next, err = resolve(s.ctx, s.api, p, before, snap, serverTime, clockAt)
			}
		}
		s.post(func() {
			if s.ctx.Err() != nil {
				return
			}
			s.State.Busy = false
			if wrote && err != nil {
				s.pending, s.confirmAt = &cmd, time.Now()
			}
			if s.State.Leaving || s.State.Syncing {
				s.drain()
				return
			}
			if err != nil {
				if errors.Is(err, errCommandPending) {
					s.waitConfirmation()
					return
				}
				s.fail(err, time.Second)
				return
			}
			s.pending = nil
			s.State.Error = ""
			next = s.accept(next, *snap.Command)
			s.clockAligned = s.clockAligned || !clockAt.IsZero()
			s.changed(&next)
			s.drain()
		})
	}()
}

func (s *Session) accept(p Playback, cmd netease.ListenCommand) Playback {
	fresh := s.last == nil || !sameCommand(*s.last, cmd)
	if fresh && (cmd.Type == "PROGRESS" || cmd.Type == "SEEK") {
		// EOFs from the old position must not override the acknowledged seek.
		// Pending seeks on this song remain valid.
		s.commands = slices.DeleteFunc(s.commands, func(a Action) bool { return a.Kind == "ENDED" })
	}
	// A new GOTO can replay the same song; ordinary pause/seek updates and
	// reapplying the same snapshot must not invalidate subsequent seeks.
	if p.Song() != s.playback.Song() || cmd.Type == "GOTO" && fresh {
		s.trackEpoch++
	}
	p.Instance = s.trackEpoch
	s.playback, s.last = p, &cmd
	return p
}
