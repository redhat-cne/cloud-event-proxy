package alias

import "sync"

type store struct {
	sync.RWMutex
	aliases map[string]string
}

var storeInstance = store{aliases: make(map[string]string)}

// GetAlias returns the alias for the given interface name, or the
// interface name itself when no alias has been registered.
func GetAlias(ifname string) string {
	storeInstance.RLock()
	defer storeInstance.RUnlock()
	if v, ok := storeInstance.aliases[ifname]; ok {
		return v
	}
	return ifname
}

// SetAlias records an alias for the given interface name.
func SetAlias(ifname, alias string) {
	storeInstance.Lock()
	defer storeInstance.Unlock()
	storeInstance.aliases[ifname] = alias
}

// Debug logs every registered alias through the supplied logger function.
func Debug(logF func(string, ...any)) {
	storeInstance.RLock()
	defer storeInstance.RUnlock()
	for ifName, a := range storeInstance.aliases {
		logF("DEBUG: ifname: '%s' alias: '%s'\n", ifName, a)
	}
}
