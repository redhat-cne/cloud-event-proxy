package config

import (
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultUpdateInterval for watching ptpconfig update
	DefaultUpdateInterval = 45
	// DefaultProfilePath  default ptp profile path
	DefaultProfilePath       = "/etc/linuxptp"
	maxOffsetThreshold int64 = 100 //in nano secs
	minOffsetThreshold int64 = -100
	holdoverTimeout    int64 = 5
)

// PtpProfile ...
type PtpProfile struct {
	Name              *string           `json:"name"`
	Interface         *string           `json:"interface"`
	PtpClockThreshold PtpClockThreshold `json:"ptpClockThreshold,omitempty"`
}

// PtpClockThreshold ...
type PtpClockThreshold struct {
	// clock state to stay in holdover state in secs
	HoldOverTimeout int64 `json:"holdOverTimeout,omitempty"`
	// max offset in nano secs
	MaxOffsetThreshold int64 `json:"maxOffsetThreshold,omitempty"`
	// min offset in nano secs
	MinOffsetThreshold int64 `json:"minOffsetThreshold,omitempty"`
}

// LinuxPTPConfUpdate for ptp-conf update
type LinuxPTPConfUpdate struct {
	lock                   sync.RWMutex
	UpdateCh               chan bool
	NodeProfiles           []PtpProfile
	appliedNodeProfileJSON []byte
	profilePath            string
	intervalUpdate         int
	EventThreshold         map[string]*PtpClockThreshold
}

// NewLinuxPTPConfUpdate -- profile updater
func NewLinuxPTPConfUpdate() *LinuxPTPConfUpdate {
	ptpProfileUpdate := &LinuxPTPConfUpdate{
		lock:           sync.RWMutex{},
		UpdateCh:       make(chan bool),
		profilePath:    DefaultProfilePath,
		intervalUpdate: DefaultUpdateInterval,
		EventThreshold: make(map[string]*PtpClockThreshold),
	}

	if os.Getenv("PTP_PROFILE_PATH") != "" {
		ptpProfileUpdate.profilePath = os.Getenv("PTP_PROFILE_PATH")
	}

	if os.Getenv("CONFIG_UPDATE_INTERVAL") != "" {
		if i := common.GetIntEnv("CONFIG_UPDATE_INTERVAL"); i > 0 {
			ptpProfileUpdate.intervalUpdate = i
		}
	}
	log.Infof("profile path set to  %s", ptpProfileUpdate.profilePath)
	log.Infof("config update interval is set to %d secs ", ptpProfileUpdate.intervalUpdate)
	return ptpProfileUpdate
}

// SetDefaultPTPThreshold ... creates default record
func (l *LinuxPTPConfUpdate) SetDefaultPTPThreshold(iface string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.EventThreshold[iface] = &PtpClockThreshold{
		HoldOverTimeout:    holdoverTimeout,
		MaxOffsetThreshold: maxOffsetThreshold,
		MinOffsetThreshold: minOffsetThreshold,
	}
}

// DeletePTPThreshold ... delete threshold for the interface
func (l *LinuxPTPConfUpdate) DeletePTPThreshold(iface string) {
	if _, found := l.EventThreshold[iface]; found {
		l.lock.Lock()
		defer l.lock.Unlock()
		delete(l.EventThreshold, iface)
	}
}

// UpdatePTPThreshold .. update ptp threshold
func (l *LinuxPTPConfUpdate) UpdatePTPThreshold() {
	for _, profile := range l.NodeProfiles {
		if _, found := l.EventThreshold[*profile.Interface]; !found {
			l.SetDefaultPTPThreshold(*profile.Interface)
		}
		if t, found := l.EventThreshold[*profile.Interface]; found {
			if profile.PtpClockThreshold.MaxOffsetThreshold != 0 {
				t.MaxOffsetThreshold = profile.PtpClockThreshold.MaxOffsetThreshold
			}
			if profile.PtpClockThreshold.MinOffsetThreshold != 0 {
				t.MinOffsetThreshold = profile.PtpClockThreshold.MinOffsetThreshold
			}
			if profile.PtpClockThreshold.HoldOverTimeout != 0 {
				t.HoldOverTimeout = profile.PtpClockThreshold.HoldOverTimeout
			}
			log.Infof("update ptp threshold values for %s\n holdoverTimeout: %d\n maxThreshold: %d\n minThreshold: %d\n",
				*profile.Interface,t.HoldOverTimeout, t.MaxOffsetThreshold, t.MinOffsetThreshold)
		}
	}
}

// UpdateConfig ... update profile
func (l *LinuxPTPConfUpdate) UpdateConfig(nodeProfilesJSON []byte) error {
	if string(l.appliedNodeProfileJSON) == string(nodeProfilesJSON) {
		return nil
	}

	if nodeProfiles, ok := tryToLoadConfig(nodeProfilesJSON); ok {
		log.Info("load ptp profiles")
		l.appliedNodeProfileJSON = nodeProfilesJSON
		l.NodeProfiles = nodeProfiles
		l.UpdateCh <- true
		return nil
	}

	if nodeProfiles, ok := tryToLoadOldConfig(nodeProfilesJSON); ok {
		// Support empty old config
		// '{"name":null,"interface":null}'
		if nodeProfiles[0].Name == nil || nodeProfiles[0].Interface == nil {
			log.Infof("Skip no profile %+v", nodeProfiles[0])
			return nil
		}

		log.Info("load profiles using old method")
		l.appliedNodeProfileJSON = nodeProfilesJSON
		l.NodeProfiles = nodeProfiles
		l.UpdateCh <- true

		return nil
	}

	return fmt.Errorf("unable to load profile config")
}

// Try to load the multiple policy config
func tryToLoadConfig(nodeProfilesJSON []byte) ([]PtpProfile, bool) {
	var ptpConfig []PtpProfile
	err := json.Unmarshal(nodeProfilesJSON, &ptpConfig)
	if err != nil {
		return nil, false
	}

	return ptpConfig, true
}

// For backward compatibility we also try to load the one policy scenario
func tryToLoadOldConfig(nodeProfilesJSON []byte) ([]PtpProfile, bool) {
	ptpConfig := &PtpProfile{}
	err := json.Unmarshal(nodeProfilesJSON, ptpConfig)
	if err != nil {
		return nil, false
	}

	return []PtpProfile{*ptpConfig}, true
}

// WatchConfigUpdate watch for ptp config update
func (l *LinuxPTPConfUpdate) WatchConfigUpdate(nodeName string, closeCh chan struct{}) {
	tickerPull := time.NewTicker(time.Duration(l.intervalUpdate) * time.Second)
	defer tickerPull.Stop()
	for {
		select {
		case <-tickerPull.C:
			log.Infof("ticker pull for ptp profile updates")
			nodeProfile := filepath.Join(l.profilePath, nodeName)
			if _, err := os.Stat(nodeProfile); err != nil {
				if os.IsNotExist(err) {
					log.Infof("ptp profile doesn't exist for node: %v", nodeName)
					l.UpdateCh <- true // if profile doesn't exist let the caller know
					continue
				} else {
					log.Errorf("error stating node profile %v: %v", nodeName, err)
					continue
				}
			}
			nodeProfilesJSON, err := ioutil.ReadFile(nodeProfile)
			if err != nil {
				log.Errorf("error reading node profile: %v", nodeProfile)
				continue
			}
			err = l.UpdateConfig(nodeProfilesJSON)
			if err != nil {
				log.Errorf("error updating the node configuration using the profiles loaded: %v", err)
			}
		case sig := <-closeCh:
			log.Info("signal received, shutting down", sig)
			return
		}
	}
}

// GetDefaultThreshold ... get default threshold
func GetDefaultThreshold() PtpClockThreshold {
	return PtpClockThreshold{
		HoldOverTimeout:    holdoverTimeout,
		MaxOffsetThreshold: maxOffsetThreshold,
		MinOffsetThreshold: minOffsetThreshold,
	}
}
