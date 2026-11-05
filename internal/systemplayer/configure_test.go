package systemplayer

import (
	"errors"
	"testing"
)

// A failed presentation must neither destroy an existing peer nor prevent its creation or explicit disable.
func TestConfigurePresentationsIsolatesFailures(t *testing.T) {
	failed := errors.New("native creation failed")
	for _, failedMenu := range []bool{false, true} {
		for _, existingPeer := range []bool{false, true} {
			t.Run(map[bool]string{false: "touch", true: "menu"}[failedMenu]+map[bool]string{false: "/new-peer", true: "/existing-peer"}[existingPeer], func(t *testing.T) {
				menuID, touchID, nextID := 0, 0, 10
				if existingPeer {
					if failedMenu {
						touchID = 1
					} else {
						menuID = 1
					}
				}
				fail := true
				configure := func(id *int, isMenu bool) func(bool) error {
					return func(enabled bool) error {
						if !enabled {
							*id = 0
							return nil
						}
						if *id != 0 {
							return nil
						}
						if fail && isMenu == failedMenu {
							return failed
						}
						nextID++
						*id = nextID
						return nil
					}
				}
				menu, touch := configure(&menuID, true), configure(&touchID, false)
				requested := Options{MenuBarVisible: true, MenuBarControls: true, MenuBarLyrics: true, TouchBarLyrics: true}
				got, err := configurePresentations(requested, menu, touch)
				if !errors.Is(err, failed) {
					t.Fatalf("error = %v", err)
				}
				peerID := menuID
				if failedMenu {
					peerID = touchID
					if got.menuBar() || !got.TouchBarLyrics || menuID != 0 {
						t.Fatalf("effective options: %+v", got)
					}
				} else if !got.menuBar() || got.TouchBarLyrics || touchID != 0 {
					t.Fatalf("effective options: %+v", got)
				}
				if peerID == 0 || existingPeer && peerID != 1 {
					t.Fatal("successful peer was lost or replaced")
				}

				// Retrying the failed configuration creates only the missing resource.
				fail = false
				got, err = configurePresentations(requested, menu, touch)
				if err != nil || got != requested || menuID == 0 || touchID == 0 {
					t.Fatalf("retry: %+v, %v", got, err)
				}
				if failedMenu && touchID != peerID || !failedMenu && menuID != peerID {
					t.Fatal("retry replaced the peer")
				}

				// Explicitly disabling the working peer must still apply if the other fails.
				fail = true
				if failedMenu {
					menuID = 0
					requested.TouchBarLyrics = false
				} else {
					touchID = 0
					requested.MenuBarVisible = false
				}
				_, err = configurePresentations(requested, menu, touch)
				if !errors.Is(err, failed) || menuID != 0 || touchID != 0 {
					t.Fatal("failure skipped an explicit disable")
				}
			})
		}
	}
}
