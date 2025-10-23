package metrics

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/alias"
	log "github.com/sirupsen/logrus"
)

const portAliasesEndpoint = "http://localhost:8081/port-aliases"

// SyncAliasesFromDaemon fetches aliases from linuxptp-daemon and populates the local alias store
func SyncAliasesFromDaemon() error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(portAliasesEndpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var aliases map[string]string
	if err = json.NewDecoder(resp.Body).Decode(&aliases); err != nil {
		return err
	}

	for ifName, aliasValue := range aliases {
		alias.SetAlias(ifName, aliasValue)
	}
	log.Infof("Synced %d port aliases from linuxptp-daemon", len(aliases))
	return nil
}
