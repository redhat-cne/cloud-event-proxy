package alias

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// resetStore clears the alias store between tests by overwriting every
// known key with an empty SetAlias (the package doesn't expose a clear).
// We also set known aliases to ensure a clean slate.
func resetStore(ifaces ...string) {
	for _, iface := range ifaces {
		SetAlias(iface, "")
	}
}

// resetMissCache removes the given interfaces from the negative cache.
func resetMissCache(ifaces ...string) {
	for _, iface := range ifaces {
		missCache.remove(iface)
	}
}

// startMockDaemon starts an HTTP server that serves /port-aliases with the
// given alias map. Returns a cleanup function that shuts down the server.
func startMockDaemon(t *testing.T, aliases map[string]string) func() {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/port-aliases", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aliases)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:8081")
	if !assert.NoError(t, err, "failed to bind mock daemon on :8081") {
		t.FailNow()
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return func() { _ = srv.Close() }
}

func TestSetAlias_And_GetAlias_CacheHit(t *testing.T) {
	SetAlias("ens1f0", "ens1fx")
	defer resetStore("ens1f0")

	assert.Equal(t, "ens1fx", GetAlias("ens1f0"))
}

func TestGetAlias_UnknownWithoutDaemon_ReturnRawName(t *testing.T) {
	// No daemon running on :8081, so sync will fail and we fall back
	// to the raw interface name.
	defer resetMissCache("unknowniface99")
	result := GetAlias("unknowniface99")
	assert.Equal(t, "unknowniface99", result)
}

func TestSyncFromDaemon_PopulatesStore(t *testing.T) {
	aliases := map[string]string{
		"ens3f0": "ens3fx",
		"ens3f1": "ens3fx",
	}
	cleanup := startMockDaemon(t, aliases)
	defer cleanup()
	defer resetStore("ens3f0", "ens3f1")

	n, err := SyncFromDaemon()
	assert.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "ens3fx", GetAlias("ens3f0"))
	assert.Equal(t, "ens3fx", GetAlias("ens3f1"))
}

func TestSyncFromDaemon_NoDaemon_ReturnsError(t *testing.T) {
	n, err := SyncFromDaemon()
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestGetAlias_CacheMiss_SyncsFromDaemon(t *testing.T) {
	aliases := map[string]string{
		"ens5f0": "ens5fx",
	}
	cleanup := startMockDaemon(t, aliases)
	defer cleanup()
	defer resetStore("ens5f0")

	// Don't pre-populate — GetAlias should trigger sync on miss
	result := GetAlias("ens5f0")
	assert.Equal(t, "ens5fx", result)
}

func TestGetAlias_ConcurrentMisses_AllResolve(t *testing.T) {
	aliases := map[string]string{
		"ens6f0": "ens6fx",
		"ens6f1": "ens6fx",
	}
	cleanup := startMockDaemon(t, aliases)
	defer cleanup()
	defer resetStore("ens6f0", "ens6f1")

	const n = 20
	results := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			iface := "ens6f0"
			if idx%2 == 1 {
				iface = "ens6f1"
			}
			results[idx] = GetAlias(iface)
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		assert.Equal(t, "ens6fx", r, "goroutine %d got wrong alias", i)
	}
}

func TestSyncFromDaemon_OverwritesStaleAliases(t *testing.T) {
	SetAlias("ens7f0", "old_alias")
	defer resetStore("ens7f0")

	aliases := map[string]string{
		"ens7f0": "ens7fx",
	}
	cleanup := startMockDaemon(t, aliases)
	defer cleanup()

	n, err := SyncFromDaemon()
	assert.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "ens7fx", GetAlias("ens7f0"))
}

func TestSyncFromDaemon_EmptyResponse(t *testing.T) {
	cleanup := startMockDaemon(t, map[string]string{})
	defer cleanup()

	n, err := SyncFromDaemon()
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestGetAlias_AfterSyncFailure_ReturnsRawName(t *testing.T) {
	// No daemon, no pre-populated alias — should fall back to raw name
	defer resetMissCache("noexist0")
	result := GetAlias("noexist0")
	assert.Equal(t, "noexist0", result)
}

func TestGetAlias_GateCoalescing_InFlightSyncServesWaiters(t *testing.T) {
	// Simulate a slow daemon to verify that concurrent GetAlias callers
	// wait for the in-flight sync rather than each triggering their own.
	mux := http.NewServeMux()
	mux.HandleFunc("/port-aliases", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ens8f0": "ens8fx",
		})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:8081")
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()
	defer resetStore("ens8f0")

	const n = 10
	results := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = GetAlias("ens8f0")
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		assert.Equal(t, "ens8fx", r, "goroutine %d: expected coalesced alias", i)
	}
}

func TestDebug_LogsAliases(t *testing.T) {
	SetAlias("ens9f0", "ens9fx")
	defer resetStore("ens9f0")

	var logged []string
	Debug(func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})
	assert.NotEmpty(t, logged)
	found := false
	for _, line := range logged {
		if strings.Contains(line, "ens9f0") && strings.Contains(line, "ens9fx") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected ens9f0→ens9fx in debug output, got: %v", logged)
}

func TestNegativeCache_SkipsSyncForRecentMiss(t *testing.T) {
	// First call triggers handleMiss (no daemon) and records the miss.
	defer resetMissCache("negcache0")
	result := GetAlias("negcache0")
	assert.Equal(t, "negcache0", result)

	// Start a daemon that serves the alias. If the negative cache works,
	// the second GetAlias should NOT reach the daemon and should return
	// the raw name.
	aliases := map[string]string{"negcache0": "negcache0_alias"}
	cleanup := startMockDaemon(t, aliases)
	defer cleanup()
	defer resetStore("negcache0")

	result = GetAlias("negcache0")
	assert.Equal(t, "negcache0", result, "negative cache should suppress daemon sync")
}

func TestNegativeCache_ExpiresAfterTTL(t *testing.T) {
	origTTL := missTTL
	missTTL = 50 * time.Millisecond
	defer func() { missTTL = origTTL }()

	defer resetMissCache("negttl0")

	// First call: miss, no daemon — records in negative cache
	result := GetAlias("negttl0")
	assert.Equal(t, "negttl0", result)

	// Wait for negative cache to expire
	time.Sleep(60 * time.Millisecond)

	// Start daemon with the alias — now the miss should retry and succeed
	aliases := map[string]string{"negttl0": "negttl0_alias"}
	cleanup := startMockDaemon(t, aliases)
	defer cleanup()
	defer resetStore("negttl0")

	result = GetAlias("negttl0")
	assert.Equal(t, "negttl0_alias", result, "negative cache should have expired, allowing re-sync")
}

func TestNegativeCache_ClearedBySetAlias(t *testing.T) {
	defer resetMissCache("negset0")
	defer resetStore("negset0")

	// Trigger a miss to populate negative cache
	result := GetAlias("negset0")
	assert.Equal(t, "negset0", result)
	assert.True(t, missCache.recentlyMissed("negset0"), "should be in negative cache after miss")

	// SetAlias should clear the negative cache entry
	SetAlias("negset0", "negset0_alias")
	assert.False(t, missCache.recentlyMissed("negset0"), "SetAlias should clear negative cache")
	assert.Equal(t, "negset0_alias", GetAlias("negset0"))
}
