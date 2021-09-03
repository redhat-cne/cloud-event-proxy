package config

// Copyright 2020 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// config reads ptp configmap (not ptp4l.x.config, but k8s configmap)  and update threshold values for all interfaces

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	log "github.com/sirupsen/logrus"
)

var (
	sectionHead        = regexp.MustCompile(`\[([^\[\]]*)\]`)
	ptpConfigFileRegEx = regexp.MustCompile(`ptp4l.[0-9]*.config`)
)

const (
	// DefaultUpdateInterval for watching ptpconfigmap update
	DefaultUpdateInterval = 60
	// DefaultProfilePath  default ptp profile path
	DefaultProfilePath       = "/etc/linuxptp"
	maxOffsetThreshold int64 = 100 //in nano secs
	minOffsetThreshold int64 = -100
	holdoverTimeout    int64 = 5
	ignorePtp4lSection       = "global"
)

// PtpProfile ...
type PtpProfile struct {
	Name              *string            `json:"name"`
	Interface         *string            `json:"interface"`
	PtpClockThreshold *PtpClockThreshold `json:"ptpClockThreshold,omitempty"`
	Ptp4lOpts         *string            `json:"ptp4lOpts,omitempty"`
	Phc2sysOpts       *string            `json:"phc2sysOpts,omitempty"`
	Ptp4lConf         *string            `json:"ptp4lConf,omitempty"`
	Interfaces        []*string
}

// PtpClockThreshold ...
type PtpClockThreshold struct {
	// clock state to stay in holdover state in secs
	HoldOverTimeout int64 `json:"holdOverTimeout,omitempty"`
	// max offset in nano secs
	MaxOffsetThreshold int64 `json:"maxOffsetThreshold,omitempty"`
	// min offset in nano secs
	MinOffsetThreshold int64 `json:"minOffsetThreshold,omitempty"`
	// Close  any  previous holdover
	Close chan struct{} `json:"close,omitempty"`
}

// LinuxPTPConfigMapUpdate for ptp-conf update
type LinuxPTPConfigMapUpdate struct {
	lock                   sync.RWMutex
	UpdateCh               chan bool
	NodeProfiles           []PtpProfile
	appliedNodeProfileJSON []byte
	profilePath            string
	intervalUpdate         int
	EventThreshold         map[string]*PtpClockThreshold
}

// NewLinuxPTPConfUpdate -- profile updater
func NewLinuxPTPConfUpdate() *LinuxPTPConfigMapUpdate {
	ptpProfileUpdate := &LinuxPTPConfigMapUpdate{
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

// GetInterface ... return interfaces from configmap
func (p *PtpProfile) GetInterface() (interfaces []*string) {
	var singleInterface string
	if p.Interface != nil {
		singleInterface = *p.Interface
		interfaces = append(interfaces, &singleInterface)
	}
	if p.Ptp4lConf != nil {
		matches := sectionHead.FindAllStringSubmatch(*p.Ptp4lConf, -1)
		for _, v := range matches {
			if v[1] != ignorePtp4lSection && v[1] != singleInterface && !ptpConfigFileRegEx.MatchString(v[1]) {
				interfaces = append(interfaces, &v[1])
			}
		}
	}
	return interfaces
}

// SetDefaultPTPThreshold ... creates default record
func (l *LinuxPTPConfigMapUpdate) SetDefaultPTPThreshold(iface string) {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.EventThreshold[iface] = &PtpClockThreshold{
		HoldOverTimeout:    holdoverTimeout,
		MaxOffsetThreshold: maxOffsetThreshold,
		MinOffsetThreshold: minOffsetThreshold,
		Close:              make(chan struct{}),
	}
}

// DeletePTPThreshold ... delete threshold for the interface
func (l *LinuxPTPConfigMapUpdate) DeletePTPThreshold(iface string) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("restored from delete ptp threshold")
		}
	}()
	if t, found := l.EventThreshold[iface]; found {
		l.lock.Lock()
		defer l.lock.Unlock()
		select {
		case <-t.Close:
		default:
			close(t.Close) // close any holdover go routines
		}
		delete(l.EventThreshold, iface)
	}
}

// UpdatePTPThreshold .. update ptp threshold
func (l *LinuxPTPConfigMapUpdate) UpdatePTPThreshold() {
	for _, profile := range l.NodeProfiles {
		for _, iface := range profile.Interfaces {
			if _, found := l.EventThreshold[*iface]; !found {
				l.SetDefaultPTPThreshold(*iface)
			}
			if profile.PtpClockThreshold == nil {
				l.SetDefaultPTPThreshold(*iface)
				continue
			}
			threshold := l.EventThreshold[*iface]
			if profile.PtpClockThreshold.MaxOffsetThreshold > 0 { // has to be greater than 0 nano secs
				threshold.MaxOffsetThreshold = profile.PtpClockThreshold.MaxOffsetThreshold
			} else {
				threshold.MaxOffsetThreshold = maxOffsetThreshold
				log.Infof("maxOffsetThreshold %d has to be > 0, now set to default %d", profile.PtpClockThreshold.MaxOffsetThreshold, maxOffsetThreshold)
			}
			if profile.PtpClockThreshold.MinOffsetThreshold > threshold.MaxOffsetThreshold {
				threshold.MinOffsetThreshold = threshold.MaxOffsetThreshold - 1 // make it one nano secs less than max
				log.Infof("minOffsetThreshold %d has to be < %d (maxOffsetThreshold), minOffsetThreshold now set to one less than maxOffsetThreshold %d",
					profile.PtpClockThreshold.MinOffsetThreshold, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
			} else {
				threshold.MinOffsetThreshold = profile.PtpClockThreshold.MinOffsetThreshold
			}
			if profile.PtpClockThreshold.HoldOverTimeout <= 0 { //secs can't be negative or zero
				threshold.HoldOverTimeout = holdoverTimeout
				log.Infof("invalid holdOverTimeout %d in secs, setting to default %d holdOverTimeout", profile.PtpClockThreshold.HoldOverTimeout, holdoverTimeout)
			} else {
				threshold.HoldOverTimeout = profile.PtpClockThreshold.HoldOverTimeout
			}
			log.Infof("update ptp threshold values for %s\n holdoverTimeout: %d\n maxThreshold: %d\n minThreshold: %d\n",
				*iface, threshold.HoldOverTimeout, threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold)
		}
	}

}

// UpdateConfig ... update profile
func (l *LinuxPTPConfigMapUpdate) UpdateConfig(nodeProfilesJSON []byte) error {
	if string(l.appliedNodeProfileJSON) == string(nodeProfilesJSON) {
		return nil
	}

	if nodeProfiles, ok := tryToLoadConfig(nodeProfilesJSON); ok {
		log.Info("load ptp profiles")
		l.appliedNodeProfileJSON = nodeProfilesJSON
		l.NodeProfiles = nodeProfiles
		for index, np := range l.NodeProfiles {
			//duplicate nodeprofiles
			l.NodeProfiles[index].Interfaces = np.GetInterface()
		}
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
		log.Errorf("error reading nodeprofile %s", err)
		return nil, false
	}

	return ptpConfig, true
}

// For backward compatibility we also try to load the one policy scenario
func tryToLoadOldConfig(nodeProfilesJSON []byte) ([]PtpProfile, bool) {
	ptpConfig := &PtpProfile{}
	err := json.Unmarshal(nodeProfilesJSON, ptpConfig)
	if err != nil {
		log.Errorf("error reading nodeprofile %s", err)
		return nil, false
	}

	return []PtpProfile{*ptpConfig}, true
}

// WatchConfigMapUpdate watch for ptp config update
func (l *LinuxPTPConfigMapUpdate) WatchConfigMapUpdate(nodeName string, closeCh chan struct{}) {
	tickerPull := time.NewTicker(time.Duration(l.intervalUpdate) * time.Second)
	defer tickerPull.Stop()
	for {
		select {
		case <-tickerPull.C:
			log.Info("ticker pull for ptp profile updates")
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
		case <-closeCh:
			log.Info("signal received, shutting down")
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
		Close:              make(chan struct{}),
	}
}
