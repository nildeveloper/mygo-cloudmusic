package netease

import (
	"context"
	"crypto/sha256"
	"net/http"
	"sync"
	"time"
)

type streamKey struct {
	login [32]byte
	song  int64
	level string
}

type streamFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	stream  *Stream
	err     error
}

type streamBudget struct {
	hits    []time.Time
	until   time.Time
	failure StreamError
}

// Only active requests are shared, never playable URLs or failures. A refusal
// cools down all songs for this login, including new selections and room syncs.
type streamRequests struct {
	mu      sync.Mutex
	flights map[streamKey]*streamFlight
	budgets map[[32]byte]*streamBudget
}

var audioRequests streamRequests

func streamLogin(cookies []*http.Cookie) [32]byte {
	identity := "anonymous"
	for _, name := range []string{"NMTID", "MUSIC_U"} {
		for _, c := range cookies {
			if c.Name == name && c.Value != "" {
				identity = name + "=" + c.Value
			}
		}
	}
	return sha256.Sum256([]byte(identity))
}

// SongURLContext cancels a shared request only when its last caller leaves.
// The source is a fixed application label, never arbitrary request contents.
func SongURLContext(ctx context.Context, id int64, level, source string) (*Stream, error) {
	return audioRequests.get(ctx, id, level, source, httpClient.Jar.Cookies(siteURL), fetchSongURL)
}

type streamFetch func(context.Context, int64, string, []*http.Cookie, func() (int, error)) (*Stream, error)

func (r *streamRequests) get(ctx context.Context, id int64, level, source string, cookies []*http.Cookie, fetch streamFetch) (*Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := streamKey{streamLogin(cookies), id, level}
	r.mu.Lock()
	if r.flights == nil {
		r.flights = make(map[streamKey]*streamFlight)
		r.budgets = make(map[[32]byte]*streamBudget)
	}
	now := time.Now()
	for login, b := range r.budgets {
		for len(b.hits) > 0 && now.Sub(b.hits[0]) >= time.Minute {
			b.hits = b.hits[1:]
		}
		if len(b.hits) == 0 && !now.Before(b.until) {
			delete(r.budgets, login)
		}
	}
	if failure := r.blocked(key, source, now); failure != nil {
		r.mu.Unlock()
		return nil, failure
	}

	f := r.flights[key]
	shared := f != nil
	if f == nil {
		requestCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		f = &streamFlight{done: make(chan struct{}), cancel: cancel}
		r.flights[key] = f
		go func() {
			defer cancel()
			stream, err := fetch(requestCtx, id, level, cookies, func() (int, error) {
				r.mu.Lock()
				defer r.mu.Unlock()
				if failure := r.blocked(key, source, time.Now()); failure != nil {
					return 0, failure
				}
				b := r.budgets[key.login]
				if b == nil {
					b = &streamBudget{}
					r.budgets[key.login] = b
				}
				now := time.Now()
				for len(b.hits) > 0 && now.Sub(b.hits[0]) >= time.Minute {
					b.hits = b.hits[1:]
				}
				b.hits = append(b.hits, now)
				return len(b.hits), nil
			})
			r.mu.Lock()
			defer r.mu.Unlock()
			if failure, ok := err.(*StreamError); ok && failure.Code == -460 && failure.Reason != "cooldown" {
				b := r.budgets[key.login]
				if b == nil {
					b = &streamBudget{}
					r.budgets[key.login] = b
				}
				b.until, b.failure = time.Now().Add(30*time.Second), *failure
				copy := *failure
				copy.RetryAfter = 30
				err = &copy
			}
			f.stream, f.err = stream, err
			if r.flights[key] == f {
				delete(r.flights, key)
			}
			close(f.done)
		}()
	}
	f.waiters++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		f.waiters--
		if f.waiters == 0 {
			f.cancel()
			if r.flights[key] == f {
				delete(r.flights, key)
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if failure, ok := f.err.(*StreamError); ok {
			copy := *failure
			copy.Source, copy.Shared = source, shared
			return nil, &copy
		}
		if f.stream != nil {
			copy := *f.stream
			return &copy, f.err
		}
		return nil, f.err
	}
}

// blocked is called with mu held; local denials never extend the cooldown.
func (r *streamRequests) blocked(key streamKey, source string, now time.Time) *StreamError {
	b := r.budgets[key.login]
	if b == nil || !now.Before(b.until) {
		return nil
	}
	failure := b.failure
	failure.Song, failure.Level, failure.Source = key.song, key.level, source
	failure.Reason, failure.Attempts = "cooldown", 0
	failure.RecentRequests = len(b.hits)
	failure.RetryAfter = int((b.until.Sub(now) + time.Second - 1) / time.Second)
	return &failure
}
