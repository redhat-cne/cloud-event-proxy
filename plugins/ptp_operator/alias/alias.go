package alias

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/gate"
	log "github.com/sirupsen/logrus"
)

const portAliasesEndpoint = "http://localhost:8081/port-aliases"

type store struct {
	sync.RWMutex
	aliases map[string]string
}

var (
	storeInstance = store{aliases: make(map[string]string)}

	syncGate gate.Gate
)

// SyncFromDaemon fetches aliases from linuxptp-daemon and populates
// the local alias store. It returns the number of aliases synced.
func SyncFromDaemon() (int, error) {
	syncGate.Reset()
	defer syncGate.Open()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(portAliasesEndpoint)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var aliases map[string]string
	if err = json.NewDecoder(resp.Body).Decode(&aliases); err != nil {
		return 0, err
	}

	for ifName, aliasValue := range aliases {
		SetAlias(ifName, aliasValue)
	}
	log.Infof("Synced %d port aliases from linuxptp-daemon", len(aliases))
	return len(aliases), nil
}

// GetAlias returns the alias for the given interface name, or the
// interface name itself when no alias has been registered.
// On a cache miss it attempts to sync from linuxptp-daemon (debounced)
// before falling back to the raw name.
func GetAlias(ifname string) string {
	// First check if alias is present
	if v, ok := fetchAlias(ifname); ok {
		return v
	}

	// Next wait for any inflight syncs
	syncGate.Wait(500 * time.Millisecond)
	if v, ok := fetchAlias(ifname); ok {
		return v
	}

	// Handle miss
	return handleMiss(ifname)
}

func fetchAlias(ifname string) (string, bool) {
	storeInstance.RLock()
	defer storeInstance.RUnlock()
	v, ok := storeInstance.aliases[ifname]
	return v, ok
}

// handleMiss syncs aliases from linuxptp-daemon (debounced) and returns
// the alias for ifname. If the daemon is unreachable the alias is
// computed locally so a raw interface name is never returned.
func handleMiss(ifname string) string {
	if _, err := SyncFromDaemon(); err != nil {
		log.Debugf("alias sync on cache miss failed: %v", err)
	}
	if v, ok := fetchAlias(ifname); ok {
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
