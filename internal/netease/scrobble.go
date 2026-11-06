package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// PlaybackLog describes actual local listening; seconds never include seeks.
type PlaybackLog struct {
	Song    int64
	Action  string // startplay or play
	Seconds int64
}

// ScrobbleClient retains the account that listened. Late uploads cannot read
// another login's cookies or write response cookies into its session.
type ScrobbleClient struct{ cookies []*http.Cookie }

func NewScrobbleClient() *ScrobbleClient {
	return &ScrobbleClient{cookies: httpClient.Jar.Cookies(siteURL)}
}

func (c *ScrobbleClient) Report(ctx context.Context, log PlaybackLog) error {
	if log.Song <= 0 || log.Action != "startplay" && log.Action != "play" || log.Action == "play" && log.Seconds <= 0 {
		return fmt.Errorf("netease: invalid playback log")
	}
	info := map[string]any{"id": log.Song, "type": "song", "mainsite": "1", "mainsiteWeb": "1"}
	if log.Action == "play" {
		info["time"], info["end"], info["download"], info["wifi"] = log.Seconds, "playend", 0, 0
	}
	// This player does not retain the originating playlist. Omit optional
	// source fields instead of inventing an album or currently browsed page.
	logs, err := json.Marshal([]any{map[string]any{"action": log.Action, "json": info}})
	if err != nil {
		return err
	}
	var res apiError
	if err := eapiRequestAt(ctx, "/api/feedback/weblog", map[string]any{"logs": string(logs)}, &res, c.cookies, false,
		eapiTarget{domain: "https://clientlog.music.163.com", os: "osx"}); err != nil {
		return err
	}
	if res.Code != 200 {
		return fmt.Errorf("netease: playback log: code %d", res.Code)
	}
	return nil
}
