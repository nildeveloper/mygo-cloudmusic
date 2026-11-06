package listentogether

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type Invitation struct {
	Room    string
	Inviter int64
}

func ParseInvitation(text string) (Invitation, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, ")") {
		if i := strings.LastIndex(text, "]("); i >= 0 {
			text = text[i+2 : len(text)-1]
		}
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, `\&`, "&"), "&amp;", "&")
	u, err := url.Parse(text)
	invalid := fmt.Errorf("请粘贴网易云双人一起听的完整邀请链接")
	if err != nil || u.Scheme != "https" || u.Host != "st.music.163.com" || u.User != nil {
		return Invitation{}, invalid
	}
	switch u.Path {
	case "/listen-together/share", "/listen-together/share/", "/listen-together/share/index.html":
	default:
		return Invitation{}, invalid
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Invitation{}, invalid
	}
	for _, v := range q["isFLT"] {
		if strings.EqualFold(v, "true") || v == "1" {
			return Invitation{}, invalid
		}
	}
	if len(q["roomId"]) != 1 || len(q["inviterId"]) != 1 {
		return Invitation{}, invalid
	}
	room := q.Get("roomId")
	uid, err := strconv.ParseInt(q.Get("inviterId"), 10, 64)
	if err != nil || uid <= 0 || room == "" || len(room) > 256 || strings.ContainsAny(room, " \n\r\t") {
		return Invitation{}, invalid
	}
	return Invitation{room, uid}, nil
}
func (i Invitation) URL(song int64) string {
	q := url.Values{"roomId": {i.Room}, "inviterId": {strconv.FormatInt(i.Inviter, 10)}, "songId": {strconv.FormatInt(song, 10)}}
	return "https://st.music.163.com/listen-together/share/index.html?" + q.Encode()
}
