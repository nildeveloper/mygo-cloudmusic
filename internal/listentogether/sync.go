package listentogether

import (
	"context"
	"fmt"
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
)

func validCommand(c netease.ListenCommand) bool {
	// Incoming commands carry an absolute playback snapshot. Official clients
	// may use additional command names; never interpret those as local actions.
	return c.Song > 0 && c.Progress >= 0 && (c.Status == "PLAY" || c.Status == "PAUSE")
}

func needsAlignment(local, expected Playback, now time.Time) bool {
	if !local.Ready || expected.Song() == 0 {
		return false
	}
	if local.Song() != expected.Song() {
		return true
	}
	// A follower at EOF waits for the creator's next command, not another
	// replay of the old song. Give asynchronous seeks/loads time to settle.
	if now.Sub(expected.Sampled) < 3*time.Second || expected.At(now) >= expected.Queue[expected.Index].Length()-time.Second {
		return false
	}
	return local.Playing != expected.Playing || absDuration(local.Position-expected.At(now)) > 2*time.Second
}

func absDuration(d time.Duration) time.Duration { return max(d, -d) }
func sameCommand(a, b netease.ListenCommand) bool {
	// Keep serverSeq: it is stable in the verified API and distinguishes a
	// legitimate replay after a peer restarts its client sequence.
	return a.User == b.User && a.ClientSeq == b.ClientSeq && a.ServerSeq == b.ServerSeq && a.Type == b.Type && a.Status == b.Status && a.Song == b.Song && a.Progress == b.Progress
}

// A queue replacement and its GOTO are separate writes. Until both arrive,
// retain the current song and compare the same normalized lists we apply.
func withCurrentSong(ids []netease.ListenID, current netease.ListenID) []int64 {
	list := make([]int64, len(ids)+1)
	for i, id := range ids {
		list[i] = int64(id)
	}
	list[len(ids)] = int64(current)
	return uniqueIDs(list)
}

// Room commands identify songs, not occurrences. Compact owned ID lists in
// first-seen order; history rows and the personal queue retain their duplicates.
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	return slices.DeleteFunc(ids, func(id int64) bool {
		duplicate := seen[id]
		seen[id] = true
		return duplicate
	})
}

func queueChanged(p Playback, s netease.ListenSnapshot) bool {
	if s.Playlist == nil || s.Playlist.Display == nil {
		return false
	}
	ids := withCurrentSong(s.Playlist.Display.IDs, s.Command.Song)
	if len(ids) != len(p.Queue) || p.Mode != s.Playlist.Mode {
		return true
	}
	for i, id := range ids {
		if id != p.Queue[i].ID {
			return true
		}
	}
	if s.Playlist.Mode == "RANDOM" && s.Playlist.Random != nil {
		order := withCurrentSong(s.Playlist.Random.IDs, s.Command.Song)
		if len(order) != len(p.Order) {
			return true
		}
		for i, id := range order {
			if p.Order[i] < 0 || p.Order[i] >= len(p.Queue) || p.Queue[p.Order[i]].ID != id {
				return true
			}
		}
	}
	return false
}

func resolve(ctx context.Context, api API, old Playback, previous *netease.ListenCommand, snap netease.ListenSnapshot, server int64, at time.Time) (Playback, error) {
	c := snap.Command
	p := Playback{
		Queue: slices.Clone(old.Queue), Index: -1, Mode: old.Mode,
		Playing: c.Status == "PLAY", Position: time.Duration(c.Progress) * time.Millisecond, Sampled: time.Now(),
	}
	ids := trackIDs(p.Queue)
	if snap.Playlist != nil && snap.Playlist.Display != nil {
		ids = withCurrentSong(snap.Playlist.Display.IDs, c.Song)
		p.Mode = snap.Playlist.Mode
	}
	if !slices.Contains(ids, int64(c.Song)) {
		ids = append(ids, int64(c.Song))
	}
	if len(ids) > 5000 {
		return p, fmt.Errorf("一起听队列过长")
	}
	known := map[int64]netease.Track{}
	for _, t := range old.Queue {
		if t.Duration > 0 {
			known[t.ID] = t
		}
	}
	var missing []int64
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		tracks, err := api.Tracks(ctx, missing)
		if err != nil {
			return p, err
		}
		for _, t := range tracks {
			known[t.ID] = t
		}
	}
	p.Queue = make([]netease.Track, len(ids))
	p.Order = make([]int, len(ids))
	for i, id := range ids {
		t, ok := known[id]
		if !ok {
			if id == int64(c.Song) {
				return p, fmt.Errorf("房间当前歌曲暂时不可用")
			}
			t = netease.Track{ID: id, Name: "暂不可用的歌曲"}
		}
		p.Queue[i] = t
		p.Order[i] = i
		if id == int64(c.Song) && p.Index < 0 {
			p.Index = i
		}
	}
	if c.Type != "GOTO" && old.Index >= 0 && old.Index < len(ids) && ids[old.Index] == int64(c.Song) {
		p.Index = old.Index
	}
	if p.Mode == "RANDOM" && snap.Playlist == nil {
		p.Order = slices.Clone(old.Order)
		if len(ids) > len(old.Queue) {
			p.Order = append(p.Order, len(old.Queue))
		}
	} else if snap.Playlist != nil && p.Mode == "RANDOM" {
		if snap.Playlist.Random == nil {
			return p, fmt.Errorf("房间随机队列尚未就绪")
		}
		random := withCurrentSong(snap.Playlist.Random.IDs, c.Song)
		if len(random) != len(ids) {
			return p, fmt.Errorf("房间随机队列尚未就绪")
		}
		used := make([]bool, len(ids))
		for i, id := range random {
			found := -1
			for j, tid := range ids {
				if !used[j] && tid == id {
					found = j
					break
				}
			}
			if found < 0 {
				return p, fmt.Errorf("房间随机队列无效")
			}
			used[found] = true
			p.Order[i] = found
		}
	}
	// The server clock avoids trusting the local wall clock. Its relationship
	// to serverSeq is provisional; clamp to the track and never auto-skip here.
	now := time.Now()
	p.Position = commandPosition(*c, server, at, now)
	if (server <= 0 || at.IsZero() || c.ServerSeq <= 0) && previous != nil && sameCommand(*previous, *c) {
		// A repeated command is not a fresh position sample. Until the server
		// clock arrives, retain the monotonic anchor already used for playback.
		p.Position = old.At(now)
	}
	p.Position = min(p.Position, max(0, p.Queue[p.Index].Length()-time.Millisecond))
	p.Sampled = now
	return p, nil
}

func commandPosition(c netease.ListenCommand, server int64, sampled, now time.Time) time.Duration {
	pos := time.Duration(c.Progress) * time.Millisecond
	if c.Status == "PLAY" && server > 0 && !sampled.IsZero() && c.ServerSeq > 0 {
		pos += max(0, time.Duration(server-c.ServerSeq)*time.Millisecond+now.Sub(sampled))
	}
	return max(0, pos)
}

func olderCommand(previous *netease.ListenCommand, next netease.ListenCommand) bool {
	return previous != nil && previous.ServerSeq > 0 && next.ServerSeq > 0 && next.ServerSeq < previous.ServerSeq
}
