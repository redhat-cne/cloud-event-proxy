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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"k8s.io/utils/pointer"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	log "github.com/sirupsen/logrus"
)

var (
	sectionHead        = regexp.MustCompile(`\[([^\[\]]*)\]`)
	ptpConfigFileRegEx = regexp.MustCompile(`ptp4l.[0-9]*.config`)
	profileRegEx       = regexp.MustCompile(`profile: \s*([\w-_]+)`)
)

const (
	// DefaultUpdateInterval for watching ptpconfigmap update
	DefaultUpdateInterval = 60
	// DefaultProfilePath  default ptp profile path
	DefaultProfilePath       = "/etc/linuxptp"
	maxOffsetThreshold int64 = 100 // in nano secs
	minOffsetThreshold int64 = -100
	holdoverTimeout    int64 = 5
	ignorePtp4lSection       = "global"
)

// PtpProfile ... ptp profile
type PtpProfile struct {
	Name              *string            `json:"name"`
	Interface         *string            `json:"interface"`
	PtpClockThreshold *PtpClockThreshold `json:"ptpClockThreshold,omitempty"`
	Ptp4lOpts         *string            `json:"ptp4lOpts,omitempty"`
	Phc2sysOpts       *string            `json:"phc2sysOpts,omitempty"`
	TS2PhcOpts        *string            `json:"ts2PhcOpts,omitempty"`
	Ptp4lConf         *string            `json:"ptp4lConf,omitempty"`
	TS2PhcConf        *string            `json:"ts2PhcConf,omitempty"`
	Interfaces        []*string
}

// PtpClockThreshold ... ptp clock threshold values
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

// PtpProcessOpts ... prp process options
type PtpProcessOpts struct {
	// ptp4lOpts are set
	Ptp4lOpts *string `json:"Ptp4lOpts,omitempty"`
	// Phc2Opts options are set
	Phc2Opts *string `json:"Phc2Opts,omitempty"`
	// TS2PhcOpts
	TS2PhcOpts *string `json:"TS2PhcOpts,omitempty"`
}

// Ptp4lEnabled check if ptp4l is enabled
func (ptpOpts *PtpProcessOpts) Ptp4lEnabled() bool {
	return ptpOpts.Ptp4lOpts != nil && *ptpOpts.Ptp4lOpts != ""
}

// Phc2SysEnabled check if phc2sys is enabled
func (ptpOpts *PtpProcessOpts) Phc2SysEnabled() bool {
	return ptpOpts.Phc2Opts != nil && *ptpOpts.Phc2Opts != ""
}

// TS2PhcEnabled check if ts2phc is enabled
func (ptpOpts *PtpProcessOpts) TS2PhcEnabled() bool {
	return ptpOpts.TS2PhcOpts != nil && *ptpOpts.TS2PhcOpts != ""
}

// GetPTPProfileName  ... get profile name from ptpconfig
func GetPTPProfileName(ptpConfig string) string {
	log.Infof("ptpConfig %s", ptpConfig)
	matches := profileRegEx.FindStringSubmatch(ptpConfig)
	// a regular expression
	if len(matches) > 1 {
		return matches[1]
	}
	log.Errorf("did not find matching profile name for reg ex %s and ptp config %s", "profile: \\s*([a-zA-Z0-9]+)", ptpConfig)
	return ""
}

// SafeClose ... handle close channel
func (pt *PtpClockThreshold) SafeClose() (justClosed bool) {
	defer func() {
		if recover() != nil {
			// The return result can be altered
			// in a defer function call.
			justClosed = false
		}
	}()
	select {
	case <-pt.Close:
	default:
		close(pt.Close) // close any holdover go routines
	}
	return true // <=> justClosed = true; return
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
	PtpProcessOpts         map[string]*PtpProcessOpts
}

// NewLinuxPTPConfUpdate -- profile updater
func NewLinuxPTPConfUpdate() *LinuxPTPConfigMapUpdate {
	ptpProfileUpdate := &LinuxPTPConfigMapUpdate{
		lock:           sync.RWMutex{},
		UpdateCh:       make(chan bool),
		profilePath:    DefaultProfilePath,
		intervalUpdate: DefaultUpdateInterval,
		EventThreshold: make(map[string]*PtpClockThreshold),
		PtpProcessOpts: make(map[string]*PtpProcessOpts),
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
	if p.Interface != nil && *p.Interface != "" {
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

// DeletePTPThreshold ... delete threshold for profile
func (l *LinuxPTPConfigMapUpdate) DeletePTPThreshold(name string) {
	if t, found := l.EventThreshold[name]; found {
		l.lock.Lock()
		closeHoldover(t)
		delete(l.EventThreshold, name)
		l.lock.Unlock()
	}
}

// DeleteAllPTPThreshold ... delete all threshold per config
func (l *LinuxPTPConfigMapUpdate) DeleteAllPTPThreshold() {
	for k, t := range l.EventThreshold {
		l.lock.Lock()
		closeHoldover(t)
		delete(l.EventThreshold, k)
		l.lock.Unlock()
	}
}

func closeHoldover(t *PtpClockThreshold) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("restored from delete ptp threshold")
		}
	}()
	select {
	case <-t.Close:
	default:
		close(t.Close) // close any holdover go routines
	}
}

// UpdatePTPProcessOptions ..
func (l *LinuxPTPConfigMapUpdate) UpdatePTPProcessOptions() {
	l.PtpProcessOpts = make(map[string]*PtpProcessOpts)
	for _, profile := range l.NodeProfiles {
		l.PtpProcessOpts[*profile.Name] = &PtpProcessOpts{
			Ptp4lOpts:  profile.Ptp4lOpts,
			Phc2Opts:   profile.Phc2sysOpts,
			TS2PhcOpts: profile.TS2PhcOpts}
		// ts2phcOpts are empty by default
		if profile.TS2PhcConf != nil && (profile.TS2PhcOpts == nil || *profile.TS2PhcOpts == "") {
			l.PtpProcessOpts[*profile.Name].TS2PhcOpts = pointer.StringPtr("-m")
		}
	}
}

// UpdatePTPThreshold ... update ptp threshold
func (l *LinuxPTPConfigMapUpdate) UpdatePTPThreshold() {
	var maxOffsetTh, minOffsetTh, holdOverTh int64
	for _, profile := range l.NodeProfiles {
		holdOverTh = holdoverTimeout
		maxOffsetTh = maxOffsetThreshold
		minOffsetTh = minOffsetThreshold
		if profile.PtpClockThreshold != nil {
			if profile.PtpClockThreshold.MaxOffsetThreshold > 0 { // has to be greater than 0 nano secs
				maxOffsetTh = profile.PtpClockThreshold.MaxOffsetThreshold
			} else {
				log.Infof("maxOffsetThreshold %d has to be > 0, now set to default %d", profile.PtpClockThreshold.MaxOffsetThreshold, maxOffsetTh)
			}
			if profile.PtpClockThreshold.MinOffsetThreshold > maxOffsetTh {
				minOffsetTh = maxOffsetTh - 1 // make it one nano secs less than max
				log.Infof("minOffsetThreshold %d has to be < %d (maxOffsetThreshold), minOffsetThreshold now set to one less than maxOffsetThreshold %d",
					profile.PtpClockThreshold.MinOffsetThreshold, maxOffsetTh, minOffsetTh)
			} else {
				minOffsetTh = profile.PtpClockThreshold.MinOffsetThreshold
			}
			if profile.PtpClockThreshold.HoldOverTimeout > 0 { // secs can't be negative or zero
				holdOverTh = profile.PtpClockThreshold.HoldOverTimeout
			} else {
				log.Infof("invalid holdOverTimeout %d in secs, setting to default %d holdOverTimeout", profile.PtpClockThreshold.HoldOverTimeout, holdOverTh)
			}
		}

		l.EventThreshold[*profile.Name] = &PtpClockThreshold{
			HoldOverTimeout:    holdOverTh,
			MaxOffsetThreshold: maxOffsetTh,
			MinOffsetThreshold: minOffsetTh,
			Close:              make(chan struct{}),
		}
		log.Infof("update ptp threshold values for %s\n holdoverTimeout: %d\n maxThreshold: %d\n minThreshold: %d\n",
			*profile.Name, holdOverTh, maxOffsetTh, minOffsetTh)
	}
}

// UpdateConfig ... update profile
func (l *LinuxPTPConfigMapUpdate) UpdateConfig(nodeProfilesJSON []byte) error {
	if bytes.Equal(l.appliedNodeProfileJSON, nodeProfilesJSON) {
		return nil
	}
	log.Info("updating PROFILE")
	if nodeProfiles, ok := tryToLoadConfig(nodeProfilesJSON); ok {
		log.Info("load ptp profiles")
		l.appliedNodeProfileJSON = nodeProfilesJSON
		l.NodeProfiles = nodeProfiles
		for index, np := range l.NodeProfiles {
			// duplicate node profiles
			l.NodeProfiles[index].Interfaces = np.GetInterface()
			l.NodeProfiles[index].PtpClockThreshold = np.PtpClockThreshold
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
	l.updatePtpConfig(nodeName)
	tickerPull := time.NewTicker(time.Duration(l.intervalUpdate) * time.Second)
	defer tickerPull.Stop()
	for {
		select {
		case <-tickerPull.C:
			l.updatePtpConfig(nodeName)
		case <-closeCh:
			log.Info("signal received, shutting down")
			return
		}
	}
}

func (l *LinuxPTPConfigMapUpdate) updatePtpConfig(nodeName string) {
	nodeProfile := filepath.Join(l.profilePath, nodeName)
	if _, err := os.Stat(nodeProfile); err != nil {
		if os.IsNotExist(err) {
			log.Infof("ptp profile %s doesn't exist for node: %v , error %s", nodeProfile, nodeName, err.Error())
			l.UpdateCh <- true // if profile doesn't exist let the caller know
			return
		}
		log.Errorf("error finding node profile %v: %v", nodeName, err)
		return
	}
	nodeProfilesJSON, err := os.ReadFile(nodeProfile)
	if err != nil {
		log.Errorf("error reading node profile: %v", nodeProfile)
		return
	}
	err = l.UpdateConfig(nodeProfilesJSON)
	if err != nil {
		log.Errorf("error updating the node configuration using the profiles loaded: %v", err)
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
