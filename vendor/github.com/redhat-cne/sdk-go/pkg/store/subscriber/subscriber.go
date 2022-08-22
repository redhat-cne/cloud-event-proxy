package subscriber

import (
	"sync"

	"github.com/redhat-cne/sdk-go/pkg/subscriber"
)

// Store  defines subscribers connection store struct
type Store struct {
	sync.RWMutex
	// Store stores subscribers in a map
	Store map[string]*subscriber.Subscriber
}

// Set is a wrapper for setting the value of a key in the underlying map
func (ss *Store) Set(clientID string, val subscriber.Subscriber) {
	ss.Lock()
	defer ss.Unlock()
	ss.Store[clientID] = &val
}

// Delete ... delete from store
func (ss *Store) Delete(clientID string) {
	ss.Lock()
	defer ss.Unlock()
	delete(ss.Store, clientID)
}
