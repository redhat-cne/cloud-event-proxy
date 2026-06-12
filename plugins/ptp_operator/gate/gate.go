package gate

import (
	"sync"
	"time"

	"github.com/golang/glog"
)

type Gate struct {
	lock sync.RWMutex
	c    chan struct{}
	once func()
}

// Reset creates a new blocking gate. Concurrent callers that invoke
// Wait will block until Open is called. Uses TryLock so only one
// Reset can be active at a time; subsequent calls are no-ops.
func (g *Gate) Reset() {
	if !g.lock.TryLock() {
		return
	}
	defer g.lock.Unlock()
	g.c = make(chan struct{})
	g.once = sync.OnceFunc(func() {
		close(g.c)
	})
	glog.Info("reset (blocked)")
}

// Open unblocks all goroutines waiting on the gate.
// Safe to call multiple times (uses sync.Once).
func (g *Gate) Open() {
	g.lock.RLock()
	defer g.lock.RUnlock()
	if g.once != nil {
		g.once()
	}
}

// Wait blocks until the gate opens or a safety timeout expires.
func (g *Gate) Wait(timeout time.Duration) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	if g.c == nil {
		return
	}
	select {
	case <-g.c:
		glog.V(4).Info("gate opened, proceeding")
	case <-time.After(timeout):
		glog.Warningf("gate timeout after %s, proceeding anyway", timeout)
	}
}
