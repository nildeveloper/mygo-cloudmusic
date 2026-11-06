package netease

import "fmt"

// RecordPeriod is the period of a listener's personal song ranking.
type RecordPeriod int

const (
	RecordAll  RecordPeriod = 0
	RecordWeek RecordPeriod = 1
)

// SongRecord keeps the server's optional count separate from song metadata.
// A missing count is unknown, and score is never used as a count.
type SongRecord struct {
	Track
	PlayCount *int64
}

// ListeningRecord returns songs in the server's ranking order. Scores are
// relative weights, not play counts; this read does not report local plays.
func ListeningRecord(uid int64, period RecordPeriod) ([]SongRecord, error) {
	if uid <= 0 || period != RecordWeek && period != RecordAll {
		return nil, fmt.Errorf("netease: invalid listening record request")
	}
	// Some responses use the web song shape instead of ar/al/dt. Normalize
	// here rather than changing the track model used by the whole player.
	type song struct {
		Track
		Artists  []Artist `json:"artists"`
		Album    Album    `json:"album"`
		Duration int64    `json:"duration"`
	}
	type entry struct {
		Song      song   `json:"song"`
		PlayCount *int64 `json:"playCount"`
	}
	var res struct {
		apiError
		Week []entry `json:"weekData"`
		All  []entry `json:"allData"`
	}
	if err := eapiPost("/api/v1/play/record", map[string]any{"uid": uid, "type": int(period)}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("netease: listening record: code %d", res.Code)
	}
	rows := res.Week
	if period == RecordAll {
		rows = res.All
	}
	tracks := make([]Track, 0, len(rows))
	records := make([]SongRecord, 0, len(rows))
	for _, row := range rows {
		t := row.Song.Track
		if t.ID <= 0 {
			continue
		}
		if len(t.Artists) == 0 {
			t.Artists = row.Song.Artists
		}
		if t.Album.ID == 0 {
			t.Album = row.Song.Album
		}
		if t.Duration == 0 {
			t.Duration = row.Song.Duration
		}
		tracks = append(tracks, t)
		records = append(records, SongRecord{PlayCount: row.PlayCount})
	}
	detailPrivileges(tracks)
	for i := range tracks {
		records[i].Track = tracks[i]
	}
	return records, nil
}
