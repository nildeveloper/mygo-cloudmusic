package systemplayer

import "testing"

// Native notifications must defer and coalesce delivery without a sampling timer, and queued work must expire on Close.
func TestEnvironmentRefreshDeliveryAndCancellation(t *testing.T) {
	var r environmentRefresh
	var queued []func()
	post := func(fn func()) { queued = append(queued, fn) }
	calls := 0
	refresh := func() { calls++ }
	r.request(post, refresh)
	r.request(post, refresh)
	if len(queued) != 1 || calls != 0 {
		t.Fatal("notification reentered or was not coalesced")
	}
	queued[0]()
	if calls != 1 {
		t.Fatal("native notification did not refresh")
	}
	r.request(post, refresh)
	queued[1]()
	if calls != 2 {
		t.Fatal("later notification required an external sampling timer")
	}
	r.request(post, refresh)
	r.close()
	r.close()
	queued[2]()
	r.request(post, refresh)
	if calls != 2 || len(queued) != 3 {
		t.Fatal("closed presentation received a refresh")
	}
}
