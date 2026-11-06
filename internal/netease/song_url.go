package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

// StreamError preserves the distinction between an API failure, an empty
// result and an actual preview. Diagnostic never includes cookies or URLs.
type StreamError struct {
	Song           int64  `json:"song_id"`
	Level          string `json:"requested_quality"`
	Code           int    `json:"api_code"`
	ItemCode       int    `json:"song_code"`
	Reason         string `json:"reason"`
	Authenticated  bool   `json:"login_cookie_present"`
	Attempts       int    `json:"attempts"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	Message        string `json:"server_message,omitempty"`
	Source         string `json:"source,omitempty"`
	RecentRequests int    `json:"requests_last_60s"`
	RetryAfter     int    `json:"retry_after_seconds,omitempty"`
	Shared         bool   `json:"shared_request,omitempty"`
	cause          error
}

func (e *StreamError) Error() string {
	if e.Code == -460 {
		if e.RetryAfter > 0 {
			return fmt.Sprintf("音源服务暂时拒绝请求（-460），请等待约 %d 秒后重试", e.RetryAfter)
		}
		return "音源服务暂时拒绝请求（-460），请稍后重试"
	}
	switch e.Reason {
	case "transport":
		return "获取音源失败，请检查网络后重试"
	case "api", "item":
		code := e.Code
		if e.Reason == "item" {
			code = e.ItemCode
		}
		if code == 301 {
			return "音源服务提示登录失效，请重新登录"
		}
		return fmt.Sprintf("音源请求未成功（%d），请重试", code)
	case "empty":
		return "音源接口返回空结果，请重试"
	case "url":
		return "网易云未返回播放地址，请重试"
	case "trial":
		return "服务端只返回试听音源，无法播放完整歌曲"
	case "song":
		return "音源接口返回的歌曲不匹配，请重试"
	default:
		return "无法获取歌曲音源，请重试"
	}
}
func (e *StreamError) Unwrap() error { return e.cause }
func (e *StreamError) Is(target error) bool {
	return target == ErrUnavailable && (e.Reason == "empty" || e.Reason == "url" || e.Reason == "trial")
}
func (e *StreamError) Diagnostic() string {
	b, _ := json.MarshalIndent(e, "", "  ")
	return "song_url_v1 / eapi\n" + string(b)
}

// SongURL is retained for callers without a playback lifetime.
func SongURL(id int64, level string) (*Stream, error) {
	return SongURLContext(context.Background(), id, level, "direct")
}

func fetchSongURL(ctx context.Context, id int64, level string, cookies []*http.Cookie, started func() (int, error)) (*Stream, error) {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, blocked := started()
		if blocked != nil {
			if failure, ok := blocked.(*StreamError); ok {
				failure.Attempts = attempt - 1
			}
			return nil, blocked
		}
		stream, err := readSongURL(ctx, id, level, cookies)
		failure, ok := err.(*StreamError)
		if ok {
			failure.Attempts, failure.RecentRequests = attempt, count
		}
		// Explicit refusals are never retried. Only empty successful results get
		// one retry, and cancellation interrupts both the wait and HTTP request.
		if !ok || attempt >= 2 || failure.Code != 200 || (failure.ItemCode != 0 && failure.ItemCode != 200) || (failure.Reason != "empty" && failure.Reason != "url") {
			return stream, err
		}
		timer := time.NewTimer(350 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

var diagnosticURL = regexp.MustCompile(`(?i)https?://[^\s"<>]+`)
var diagnosticCredential = regexp.MustCompile(`(?i)(MUSIC_U|NMTID|__csrf|authorization|cookie|token)\s*[:=]\s*[^\s,;]+`)

func streamMessage(message string, cookies []*http.Cookie) string {
	for _, c := range cookies {
		if c.Value != "" {
			message = strings.ReplaceAll(message, c.Value, "[redacted]")
		}
	}
	message = diagnosticURL.ReplaceAllString(message, "[url]")
	message = diagnosticCredential.ReplaceAllString(message, "$1=[redacted]")
	runes := []rune(strings.Join(strings.Fields(message), " "))
	return string(runes[:min(240, len(runes))])
}

func readSongURL(ctx context.Context, id int64, level string, cookies []*http.Cookie) (*Stream, error) {
	failure := &StreamError{Song: id, Level: level}
	for _, c := range cookies {
		if c.Name == "MUSIC_U" && c.Value != "" {
			failure.Authenticated = true
		}
	}
	var res struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
		Data    []struct {
			Stream
			ID            int64           `json:"id"`
			Code          int             `json:"code"`
			URL           string          `json:"url"`
			Peak          *float64        `json:"peak"`
			FreeTrialInfo json.RawMessage `json:"freeTrialInfo"`
		} `json:"data"`
	}
	encode := "mp3"
	if Lossless(level) {
		encode = "flac"
	}
	data := map[string]any{"ids": fmt.Sprintf("[%d]", id), "level": level, "encodeType": encode}
	if err := eapiRequestAt(ctx, "/api/song/enhance/player/url/v1", data, &res, cookies, false, eapiTarget{domain: eapiDomain, status: &failure.HTTPStatus}); err != nil {
		failure.Reason, failure.cause = "transport", err
		if response, ok := err.(*ResponseError); ok {
			failure.Reason, failure.Code = "api", response.Code
			failure.Message = streamMessage(response.Message, cookies)
		}
		return nil, failure
	}
	failure.Code = res.Code
	message := res.Message
	if message == "" {
		message = res.Msg
	}
	failure.Message = streamMessage(message, cookies)
	if res.Code != 200 {
		failure.Reason = "api"
		return nil, failure
	}
	if len(res.Data) == 0 {
		failure.Reason = "empty"
		return nil, failure
	}
	d := res.Data[0]
	failure.ItemCode = d.Code
	if d.ID != 0 && d.ID != id {
		failure.Reason = "song"
		return nil, failure
	}
	if d.Code != 0 && d.Code != 200 {
		failure.Reason = "item"
		return nil, failure
	}
	trial := strings.TrimSpace(string(d.FreeTrialInfo))
	if trial != "" && trial != "null" && trial != `"null"` {
		failure.Reason = "trial"
		return nil, failure
	}
	if d.URL == "" {
		failure.Reason = "url"
		return nil, failure
	}
	s := d.Stream
	s.Type = strings.ToLower(s.Type)
	if s.Type != "" && s.Type != "mp3" && s.Type != "flac" {
		return nil, fmt.Errorf("unsupported format %s", s.Type)
	}
	if d.Peak != nil {
		s.Peak = *d.Peak
	}
	if !slices.Contains(Levels, s.Level) {
		s.Level = level
	}
	s.URL = strings.Replace(d.URL, "http://", "https://", 1)
	return &s, nil
}
