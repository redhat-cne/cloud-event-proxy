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

type negativeCache struct {
	sync.RWMutex // might be overkill but this is a read heavy map
	seen         map[string]time.Time
}

func (nc *negativeCache) recentlyMissed(ifname string) bool {
	nc.RLock()
	defer nc.RUnlock()
	deadline, ok := nc.seen[ifname]
	return ok && time.Now().Before(deadline)
}

func (nc *negativeCache) record(ifname string) {
	nc.Lock()
	defer nc.Unlock()
	nc.seen[ifname] = time.Now().Add(missTTL)
}

func (nc *negativeCache) remove(ifname string) {
	nc.Lock()
	defer nc.Unlock()
	delete(nc.seen, ifname)
}

var (
	storeInstance = store{aliases: make(map[string]string)}

	syncGate gate.Gate

	missCache = negativeCache{seen: make(map[string]time.Time)}
	missTTL   = 30 * time.Second
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
// On a cache miss it attempts to sync from linuxptp-daemon before
// falling back to the raw name. Recently missed names are returned
// immediately without retrying the daemon.
func GetAlias(ifname string) string {
	if v, ok := fetchAlias(ifname); ok {
		return v
	}

	if missCache.recentlyMissed(ifname) {
		return ifname
	}

	syncGate.Wait(500 * time.Millisecond)
	if v, ok := fetchAlias(ifname); ok {
		return v
	}

	return handleMiss(ifname)
}

func fetchAlias(ifname string) (string, bool) {
	storeInstance.RLock()
	defer storeInstance.RUnlock()
	v, ok := storeInstance.aliases[ifname]
	return v, ok
}

// handleMiss syncs aliases from linuxptp-daemon and returns
// the alias for ifname, falling back to the raw name if unreachable.
// On a miss the interface name is recorded in the negative cache so
// subsequent lookups skip the daemon for missTTL.
func handleMiss(ifname string) string {
	if _, err := SyncFromDaemon(); err != nil {
		log.Debugf("alias sync on cache miss failed: %v", err)
	}
	if v, ok := fetchAlias(ifname); ok {
		return v
	}
	missCache.record(ifname)
	return ifname
}

// SetAlias records an alias for the given interface name and clears
// any negative-cache entry so the alias is used on the next lookup.
func SetAlias(ifname, alias string) {
	storeInstance.Lock()
	defer storeInstance.Unlock()
	storeInstance.aliases[ifname] = alias
	missCache.remove(ifname)
}

// Debug logs every registered alias through the supplied logger function.
func Debug(logF func(string, ...any)) {
	storeInstance.RLock()
	defer storeInstance.RUnlock()
	for ifName, a := range storeInstance.aliases {
		logF("DEBUG: ifname: '%s' alias: '%s'\n", ifName, a)
	}
}
