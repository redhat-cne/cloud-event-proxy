package utils

import (
	"sync"
)

// Aliases instance of the alias manager
var Aliases = &AliasManager{}

// AliasManager ...
type AliasManager struct {
	lock   sync.RWMutex
	values map[string]string
}

// Clear will remove all aliases
func (a *AliasManager) Clear() {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.values = make(map[string]string)
}

// SetAlias ...
func (a *AliasManager) SetAlias(name, alias string) {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.values == nil {
		a.values = make(map[string]string)
	}
	a.values[name] = alias
}

// GetAlias returns a interface name and returns the alias
func (a *AliasManager) GetAlias(ifname string) string {
	a.lock.RLock()
	defer a.lock.RUnlock()

	if ifname != "" {
		if alias, ok := a.values[ifname]; ok {
			return alias
		}
	}
	return ifname
}

// GetAlias masks interface names for metric reporting
func GetAlias(ifname string) string {
	return Aliases.GetAlias(ifname)
}
