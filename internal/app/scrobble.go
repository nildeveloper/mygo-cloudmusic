package app

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

type playbackReporter interface {
	Report(context.Context, netease.PlaybackLog) error
}

// scrobbler is one login's ordered, bounded upload queue. Its observation and
// UI state live on the main thread; only the worker touches the captured API.
type scrobbler struct {
	jobs     chan netease.PlaybackLog
	done     chan struct{}
	cancel   context.CancelFunc
	timer    *time.Timer
	instance uint64
	started  bool
	dropped  bool
	ended    bool
	closed   bool
	err      string
}

func newScrobbler(api playbackReporter, result func(error)) *scrobbler {
	ctx, cancel := context.WithCancel(context.Background())
	r := &scrobbler{jobs: make(chan netease.PlaybackLog, 64), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(r.done)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case log, ok := <-r.jobs:
				if !ok || ctx.Err() != nil {
					return
				}
				request, stop := context.WithTimeout(ctx, 8*time.Second)
				err := api.Report(request, log)
				stop()
				// A timeout can mean accepted. No blind retry without a
				// server idempotency key, nor a retry on every Changed.
				result(err)
			}
		}
	}()
	return r
}

func (r *scrobbler) observe(e player.ListeningEvent) {
	if r.closed || e.Track.ID <= 0 || e.Track.ProgramID != 0 || e.Heard <= 0 {
		return
	}
	if r.instance != e.Instance {
		r.instance, r.started, r.ended, r.dropped = e.Instance, false, false, false
	}
	if !r.started {
		r.started = true
		r.dropped = !r.enqueue(netease.PlaybackLog{Song: e.Track.ID, Action: "startplay"})
	}
	if e.Ended && !r.ended {
		r.ended = true
		if r.dropped {
			return // Never send a finish for a start that missed the queue.
		}
		// Count at most one song's duration, rounded down to whole seconds.
		heard := e.Heard
		if length := e.Track.Length(); length > 0 {
			heard = min(heard, length)
		}
		if seconds := int64(heard / time.Second); seconds > 0 {
			r.enqueue(netease.PlaybackLog{Song: e.Track.ID, Action: "play", Seconds: seconds})
		}
	}
}

func (r *scrobbler) enqueue(log netease.PlaybackLog) bool {
	select {
	case r.jobs <- log:
		return true
	default:
		r.err = "部分播放记录未能同步，云端记录可能不完整"
		return false
	}
}

func (r *scrobbler) close() {
	if r.closed {
		return
	}
	r.closed = true
	if r.timer != nil {
		r.timer.Stop()
	}
	close(r.jobs)
	// Account changes and quitting must not leave an old account's worker
	// sending for minutes. Drain briefly; nothing blocks the UI thread.
	go func() {
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
		}
		r.cancel()
	}()
}

func (a *app) detachScrobbler() *scrobbler {
	r := a.scrobble
	if r != nil {
		a.pl.FinishListening()
		a.pl.Listened = nil
		r.close()
		a.scrobble = nil
	}
	return r
}

func (a *app) resetScrobbler(prof *netease.Profile) {
	a.detachScrobbler()
	if prof == nil || a.quitting {
		return
	}
	var r *scrobbler
	r = newScrobbler(netease.NewScrobbleClient(), func(err error) {
		a.post(func() {
			if a.scrobble != r {
				return
			}
			if err != nil {
				r.err = "部分播放记录未能同步，云端记录可能不完整"
				return
			}
			// Invalidate freshness without discarding displayed rows. The
			// server may update later; don't poll just because we uploaded.
			for _, d := range a.records.data {
				if d != nil && !d.loadedAt.IsZero() {
					d.loadedAt = time.Now().Add(-time.Minute)
				}
			}
		})
	})
	a.scrobble = r
	a.pl.Listened = r.observe
	a.syncScrobble()
}

func (a *app) syncScrobble() {
	r := a.scrobble
	if r == nil || r.closed || a.quitting {
		return
	}
	a.pl.PollListening()
	if r.timer != nil {
		r.timer.Stop()
	}
	if a.pl.Playing() && a.pl.Ready() && !a.pl.Loading() {
		r.timer = time.AfterFunc(time.Second, func() {
			a.post(func() {
				if a.scrobble == r {
					a.syncScrobble()
				}
			})
		})
	}
}

// deferScrobbleQuit gives the final report the same bounded drain as a logout,
// then re-enters the normal quit handler. It never waits on the main thread.
func (a *app) deferScrobbleQuit(e *mygo.QuitEvent) bool {
	r := a.detachScrobbler()
	if r == nil {
		return false
	}
	e.PreventDefault()
	go func() {
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
		}
		a.post(func() { mygo.App.Quit() })
	}()
	return true
}
