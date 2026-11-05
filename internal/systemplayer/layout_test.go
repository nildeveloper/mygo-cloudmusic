package systemplayer

import "testing"

// Short lyrics used to reserve 220 points and hide the entire status item on small screens.
func TestMenuLyricsShrinkToFit(t *testing.T) {
	short := 45.0
	if width := menuLyricWidth(short); width != short {
		t.Fatalf("short lyric reserved %v points for %v points of text", width, short)
	}
	if width := menuLyricWidth(1000); width > 120 || width <= 0 {
		t.Fatalf("long lyric exceeded its compact viewport: %v", width)
	}
	if width := menuLyricWidth(0); width != 0 {
		t.Fatalf("empty text reserved %v points", width)
	}
}
