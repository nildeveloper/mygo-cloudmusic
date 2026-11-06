package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// ListenID accepts the API's mixed numeric/string IDs without floating point.
type ListenID int64

func (id *ListenID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*id = 0
		return nil
	}
	n, err := strconv.ParseInt(strings.Trim(string(b), `"`), 10, 64)
	*id = ListenID(n)
	return err
}

type ListenUser struct {
	UserID    ListenID `json:"userId"`
	Nickname  string   `json:"nickname"`
	AvatarURL string   `json:"avatarUrl"`
}
type ListenRoom struct {
	RoomID    string       `json:"roomId"`
	CreatorID ListenID     `json:"creatorId"`
	Users     []ListenUser `json:"roomUsers"`
}

// Report durations are seconds, unlike playback positions in milliseconds.
// Optional counters stay absent rather than becoming invented zeroes.
type ListenReport struct {
	RoomID         string     `json:"roomId"`
	User           ListenUser `json:"userInfo"`
	Peer           ListenUser `json:"anotherUserInfo"`
	Duration       *int64     `json:"duration"`
	SongCount      *int64     `json:"songCount"`
	TotalDuration  *int64     `json:"totalDuration"`
	TotalSongCount *int64     `json:"totalSongCount"`
}
type ListenStatus struct {
	InRoom   bool        `json:"inRoom"`
	RoomInfo *ListenRoom `json:"roomInfo"`
}
type ListenCommand struct {
	Type      string   `json:"commandType"`
	Status    string   `json:"playStatus"`
	Song      ListenID `json:"targetSongId"`
	Former    ListenID `json:"formerSongId"`
	User      ListenID `json:"userId"`
	Progress  int64    `json:"progress"`
	ClientSeq int64    `json:"clientSeq"`
	ServerSeq int64    `json:"serverSeq"`
	Trigger   string   `json:"triggerType"`
}
type ListenSnapshot struct {
	Command  *ListenCommand `json:"playCommand"`
	Playlist *struct {
		Display *struct {
			IDs []ListenID `json:"result"`
		} `json:"displayList"`
		Random *struct {
			IDs []ListenID `json:"result"`
		} `json:"randomList"`
		Mode    string `json:"playMode"`
		Version []struct {
			User    ListenID `json:"userId"`
			Version int64    `json:"version"`
		} `json:"version"`
	} `json:"playlist"`
}
type ListenHeartbeat struct {
	Result   bool  `json:"result"`
	Time     int64 `json:"time"`
	TimeSpan int   `json:"timeSpan"`
}

// ListenClient binds every request, including cleanup, to the login that
// opened the room. It neither reads a new login nor changes the shared jar.
type ListenClient struct{ cookies []*http.Cookie }

func NewListenClient() *ListenClient { return &ListenClient{cookies: httpClient.Jar.Cookies(siteURL)} }
func (c *ListenClient) call(ctx context.Context, path string, data map[string]any, out any) error {
	var r struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Msg     string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := eapiRequest(ctx, "/api/listen/together/"+path, data, &r, c.cookies, false); err != nil {
		return err
	}
	if r.Code != 200 {
		msg := r.Message
		if msg == "" {
			msg = r.Msg
		}
		if msg == "" {
			msg = "请求失败"
		}
		return &ResponseError{r.Code, msg}
	}
	if out == nil {
		return nil
	}
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return fmt.Errorf("一起听：响应缺少数据")
	}
	return json.Unmarshal(r.Data, out)
}
func (c *ListenClient) Status(ctx context.Context) (ListenStatus, error) {
	var s ListenStatus
	err := c.call(ctx, "status/get", map[string]any{}, &s)
	return s, err
}
func (c *ListenClient) Create(ctx context.Context) (ListenRoom, error) {
	var r struct {
		Room ListenRoom `json:"roomInfo"`
	}
	err := c.call(ctx, "room/create", map[string]any{"refer": "songplay_more"}, &r)
	if err == nil && r.Room.RoomID == "" {
		err = fmt.Errorf("一起听：未返回房间")
	}
	return r.Room, err
}
func (c *ListenClient) Join(ctx context.Context, room string, inviter int64) error {
	var check struct {
		Joinable bool `json:"joinable"`
	}
	if err := c.call(ctx, "room/check", map[string]any{"roomId": room}, &check); err != nil {
		return err
	}
	if !check.Joinable {
		return fmt.Errorf("这个一起听邀请已失效或房间已满")
	}
	var accepted struct {
		Success *bool `json:"success"`
	}
	if err := c.call(ctx, "play/invitation/accept", map[string]any{"roomId": room, "inviterId": inviter, "refer": "inbox_invite"}, &accepted); err != nil {
		return err
	}
	if accepted.Success != nil && !*accepted.Success {
		return fmt.Errorf("未能加入一起听")
	}
	return nil // The caller verifies membership with Status before playback.
}
func (c *ListenClient) Snapshot(ctx context.Context, room string) (ListenSnapshot, error) {
	var s ListenSnapshot
	err := c.call(ctx, "sync/playlist/get", map[string]any{"roomId": room}, &s)
	if s.Command != nil {
		s.Command.Type = strings.ToUpper(strings.TrimSpace(s.Command.Type))
		s.Command.Status = strings.ToUpper(strings.TrimSpace(s.Command.Status))
	}
	return s, err
}
func (c *ListenClient) Heartbeat(ctx context.Context, room string, song int64, playing bool, pos int64) (ListenHeartbeat, error) {
	status := "PAUSE"
	if playing {
		status = "PLAY"
	}
	var h ListenHeartbeat
	err := c.call(ctx, "heartbeat", map[string]any{"roomId": room, "songId": song, "playStatus": status, "progress": pos}, &h)
	if err == nil && !h.Result {
		err = fmt.Errorf("一起听心跳未被接受")
	}
	return h, err
}
func (c *ListenClient) ack(ctx context.Context, path string, data map[string]any) error {
	var r struct {
		Success *bool `json:"success"`
		Result  *bool `json:"result"`
	}
	if err := c.call(ctx, path, data, &r); err != nil {
		return err
	}
	if r.Success != nil && !*r.Success || r.Result != nil && !*r.Result {
		return fmt.Errorf("一起听操作未被接受")
	}
	return nil
}
func (c *ListenClient) Play(ctx context.Context, room string, cmd ListenCommand) error {
	// IDs are decimal strings in the desktop client's play commands.
	b, err := json.Marshal(map[string]any{
		"commandType": cmd.Type, "playStatus": cmd.Status, "progress": cmd.Progress,
		"targetSongId": strconv.FormatInt(int64(cmd.Song), 10),
		"formerSongId": strconv.FormatInt(int64(cmd.Former), 10), "clientSeq": cmd.ClientSeq,
	})
	if err != nil {
		return err
	}
	return c.ack(ctx, "play/command/report", map[string]any{"roomId": room, "commandInfo": string(b)})
}
func (c *ListenClient) Queue(ctx context.Context, room string, uid, version int64, ids []int64) error {
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = strconv.FormatInt(id, 10)
	}
	b, err := json.Marshal(map[string]any{
		"commandType": "REPLACE", "version": []map[string]int64{{"userId": uid, "version": version}},
		"anchorSongId": "", "anchorPosition": -1,
		"displayList": list, "randomList": list, "playMode": "ORDER_LOOP",
	})
	if err != nil {
		return err
	}
	return c.ack(ctx, "sync/list/command/report", map[string]any{"roomId": room, "playlistParam": string(b)})
}
func (c *ListenClient) End(ctx context.Context, room string) error {
	return c.ack(ctx, "end/v2", map[string]any{"roomId": room})
}

// Report reads the same per-room summary used by NetEase's ending page.
func (c *ListenClient) Report(ctx context.Context, room string) (ListenReport, error) {
	var r ListenReport
	if room == "" {
		return r, fmt.Errorf("一起听：报告缺少房间")
	}
	if err := c.call(ctx, "record/get", map[string]any{"roomId": room}, &r); err != nil {
		return r, err
	}
	if r.RoomID != "" && r.RoomID != room {
		return r, fmt.Errorf("一起听：报告房间不匹配")
	}
	if r.Duration == nil || *r.Duration < 0 || r.SongCount == nil || *r.SongCount < 0 || r.User.UserID <= 0 || r.Peer.UserID <= 0 {
		return r, fmt.Errorf("一起听：报告尚未生成")
	}
	return r, nil
}
func (c *ListenClient) Tracks(ctx context.Context, ids []int64) ([]Track, error) {
	var out []Track
	for len(ids) > 0 {
		batch := ids[:min(len(ids), 400)]
		ids = ids[len(batch):]
		items := make([]map[string]int64, len(batch))
		for i, id := range batch {
			items[i] = map[string]int64{"id": id}
		}
		b, _ := json.Marshal(items)
		var r struct {
			Code       int         `json:"code"`
			Songs      []Track     `json:"songs"`
			Privileges []Privilege `json:"privileges"`
		}
		if err := eapiRequest(ctx, "/api/v3/song/detail", map[string]any{"c": string(b)}, &r, c.cookies, false); err != nil {
			return nil, err
		}
		if r.Code != 200 {
			return nil, &ResponseError{r.Code, "无法加载房间歌曲"}
		}
		withPrivileges(r.Songs, r.Privileges)
		out = append(out, r.Songs...)
	}
	return out, nil
}
