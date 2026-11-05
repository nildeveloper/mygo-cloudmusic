package systemplayer

// environmentRefresh coalesces native notifications on the UI thread. Defer
// delivery until AppKit finishes changing bindings, and ignore work after Close.
type environmentRefresh struct {
	pending, closed bool
}

func (r *environmentRefresh) request(post func(func()), refresh func()) {
	if r.closed || r.pending {
		return
	}
	r.pending = true
	post(func() {
		if r.closed {
			return
		}
		r.pending = false
		refresh()
	})
}

func (r *environmentRefresh) close() { r.closed = true }
