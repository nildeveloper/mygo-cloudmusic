//go:build !darwin

package systemplayer

type nativePlayer struct{}

func (*nativePlayer) init(Host, Actions) error           { return nil }
func (*nativePlayer) configure(Options) (Options, error) { return Options{}, nil }
func (*nativePlayer) update(State)                       {}
func (*nativePlayer) close()                             {}

func (*nativePlayer) refresh() {}
