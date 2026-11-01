package mapper

import "sync"

// SyncGuard suppresses echo when applying HA-driven updates to HomeKit.
type SyncGuard struct {
	mu sync.Mutex
	ha bool
}

func (g *SyncGuard) WhileHA(fn func()) {
	g.mu.Lock()
	g.ha = true
	defer func() {
		g.ha = false
		g.mu.Unlock()
	}()
	fn()
}

func (g *SyncGuard) FromHA() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ha
}
