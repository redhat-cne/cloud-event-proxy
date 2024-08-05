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

package ptp4lconf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	log "github.com/sirupsen/logrus"
)

var (
	ptpConfigFileRegEx        = regexp.MustCompile(`ptp4l.[0-9]*.config`)
	ptpPhc2sysConfigFileRegEx = regexp.MustCompile(`phc2sys.[0-9]*.config`)
	ptpSyncE4lConfigFileRegEx = regexp.MustCompile(`synce4l.[0-9]*.config`)
	sectionHead               = regexp.MustCompile(`\[([^\[\]]*)\]`)
	profileRegEx              = regexp.MustCompile(`profile: \s*([\w-_]+)`)
	fileNameRegEx             = regexp.MustCompile("([^/]+$)")
)

const (
	ptp4lGlobalSection = "global"
)

// PtpConfigUpdate ...  updated ptp config values
type PtpConfigUpdate struct {
	Name      *string `json:"name,omitempty"`
	Ptp4lConf *string `json:"ptp4lConf,omitempty"`
	Profile   *string `json:"profile,omitempty"`
	Removed   bool    `json:"removed,omitempty"`
}

func (p *PtpConfigUpdate) String() string {
	var name, ptp4lConf string
	if p.Name != nil {
		name = *p.Name
	}
	if p.Ptp4lConf != nil {
		ptp4lConf = *p.Ptp4lConf
	}

	return fmt.Sprintf(" name:%s "+
		" ptp4lconf:%s"+
		" removed:%t",
		name,
		ptp4lConf,
		p.Removed)
}

// GetAllInterface ... return interfaces
func (p *PtpConfigUpdate) GetAllInterface() []*string {
	var interfaces []*string
	if p.Ptp4lConf != nil {
		matches := sectionHead.FindAllStringSubmatch(*p.Ptp4lConf, -1)
		for _, v := range matches {
			if v[1] != ptp4lGlobalSection && !checkIfPtP4lConf(v[1]) {
				interfaces = append(interfaces, &v[1])
			}
		}
	}
	return interfaces
}

// PTPInterface ... ptp interfaces from ptpConfig
type PTPInterface struct {
	Name     string
	PortID   int
	PortName string
	Role     types.PtpPortRole
}

// PTP4lConfig ... get filename, profile name and interfaces
type PTP4lConfig struct {
	Name       string
	Profile    string
	Interfaces []*PTPInterface
}

// ByRole ... get interface name by ptp port role
func (ptp4lCfg *PTP4lConfig) ByRole(role types.PtpPortRole) (PTPInterface, error) {
	for _, p := range ptp4lCfg.Interfaces {
		if p != nil && p.Role == role {
			return *p, nil
		}
	}
	return PTPInterface{}, fmt.Errorf("interfaces not found for the role %d --> %s", role, ptp4lCfg.String())
}

// GetUnknownAlias ... when master port details are not know, get first interface alias name
func (ptp4lCfg *PTP4lConfig) GetUnknownAlias() (string, error) {
	for _, p := range ptp4lCfg.Interfaces {
		r := []rune(p.Name)
		alias := string(r[:len(r)-1]) + "x"
		return alias, nil
	}
	return "unknown", fmt.Errorf("interfaces not found for profilfe %s", ptp4lCfg.Profile)
}

// GetAliasByInterface ... get alias name by interface name
func (ptp4lCfg *PTP4lConfig) GetAliasByInterface(p PTPInterface) string {
	r := []rune(p.Name)
	alias := string(r[:len(r)-1]) + "x"
	return alias
}

// UpdateRole ... update role
func (pi *PTPInterface) UpdateRole(role types.PtpPortRole) {
	pi.Role = role
}

func (ptp4lCfg *PTP4lConfig) String() string {
	b := strings.Builder{}
	b.WriteString("  configName: " + ptp4lCfg.Name + "\r\n")
	b.WriteString("  profileName: " + ptp4lCfg.Profile + "\r\n")
	b.WriteString("  Ptp4lConfigInterfaces: " + "\n")
	for _, p := range ptp4lCfg.Interfaces {
		b.WriteString("  name: " + p.Name + "\n")
		b.WriteString("  portName: " + p.PortName + "\n")
		b.WriteString("  role: " + p.Role.String() + "\n")
		b.WriteString("  portID: " + fmt.Sprintf("%d", p.PortID) + "\n")
	}
	return b.String()
}

// ByInterface ... get interface object by interface name
func (ptp4lCfg *PTP4lConfig) ByInterface(iface string) (PTPInterface, error) {
	for _, p := range ptp4lCfg.Interfaces {
		if p != nil && p.Name == iface {
			return *p, nil
		}
	}
	return PTPInterface{}, fmt.Errorf("interfaces not found for the interface %s", iface)
}

// ByPortID ... get ptp interface by port id
func (ptp4lCfg *PTP4lConfig) ByPortID(id int) (PTPInterface, error) {
	for _, p := range ptp4lCfg.Interfaces {
		if p != nil && p.PortID == id {
			return *p, nil
		}
	}
	return PTPInterface{}, fmt.Errorf("interface was not found for the port id %d", id)
}

// Watcher monitors a file for changes
type Watcher struct {
	fsWatcher *fsnotify.Watcher
	close     chan struct{}
}

// Close ... close watcher
func (w *Watcher) Close() {
	close(w.close)
}

// NewPtp4lConfigWatcher ... create new ptp4l config file watcher
func NewPtp4lConfigWatcher(dirToWatch string, updatedConfig chan<- *PtpConfigUpdate) (w *Watcher, err error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Errorf("error setting for file change notifier %s", err)
	}
	w = &Watcher{
		fsWatcher: fsWatcher,
		close:     make(chan struct{}),
	}
	go func() {
		defer w.fsWatcher.Close()
		// initialize all
		for _, ptpConfig := range readAllConfig(dirToWatch) {
			log.Infof("sending ptpconfig changes for %s", *ptpConfig.Name)
			updatedConfig <- ptpConfig
		}
		for {
			select {
			case event, ok := <-w.fsWatcher.Events:
				if !ok {
					log.Error("could not read watcher events")
					continue
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					if checkIfPtP4lConf(event.Name) || checkIfPhc2SysConf(event.Name) || checkIfSyncE4lConf(event.Name) {
						if ptpConfig, configErr := readConfig(event.Name); configErr == nil {
							log.Infof("sending ptpconfig changes for %s", *ptpConfig.Name)
							updatedConfig <- ptpConfig
						}
					}
				} else if event.Op&fsnotify.Remove == fsnotify.Remove {
					if checkIfPtP4lConf(event.Name) || checkIfPhc2SysConf(event.Name) || checkIfSyncE4lConf(event.Name) {
						fName := filename(event.Name)
						ptpConfig := &PtpConfigUpdate{
							Name:      &fName,
							Ptp4lConf: nil,
							Removed:   true,
						}
						log.Infof("config removed file:%s", event.Name)
						updatedConfig <- ptpConfig
					}
				}
			case fileErr, ok := <-w.fsWatcher.Errors:
				if !ok {
					log.Errorf("cannot read watcher error %s", fileErr)
					continue
				}
				log.Errorf("error while watching for file changes %s", fileErr)
			}
		}
	}()
	err = w.fsWatcher.Add(dirToWatch)
	return w, err
}

func readAllConfig(dir string) []*PtpConfigUpdate {
	var ptpConfigs []*PtpConfigUpdate
	files, fileErr := os.ReadDir(dir)
	if fileErr != nil {
		log.Errorf("error reading all config fils %s", fileErr)
	}
	for _, file := range files {
		if checkIfPtP4lConf(file.Name()) || checkIfPhc2SysConf(file.Name()) || checkIfSyncE4lConf(file.Name()) {
			if p, err := readConfig(filepath.Join(dir, file.Name())); err == nil {
				ptpConfigs = append(ptpConfigs, p)
			}
		}
	}
	return ptpConfigs
}
func readConfig(path string) (*PtpConfigUpdate, error) {
	fName := filename(path)
	b, err := os.ReadFile(path)
	if err != nil {
		log.Errorf("error reading ptpconfig %s error %s", path, err)
		return nil, err
	}
	data := string(b)
	profile := GetPTPProfileName(data)
	ptpConfig := &PtpConfigUpdate{
		Name:      &fName,
		Ptp4lConf: &data,
		Profile:   &profile,
	}
	return ptpConfig, nil
}

func filename(path string) string {
	return fileNameRegEx.FindString(path)
}

func checkIfPtP4lConf(filename string) bool {
	return ptpConfigFileRegEx.MatchString(filename)
}
func checkIfPhc2SysConf(filename string) bool {
	return ptpPhc2sysConfigFileRegEx.MatchString(filename)
}
func checkIfSyncE4lConf(filename string) bool {
	return ptpSyncE4lConfigFileRegEx.MatchString(filename)
}

// GetPTPProfileName  ... get profile name from ptpconfig
func GetPTPProfileName(ptpConfig string) string {
	matches := profileRegEx.FindStringSubmatch(ptpConfig)
	// a regular expression
	if len(matches) > 1 {
		return matches[1]
	}
	log.Errorf("did not find matching profile name for reg ex %s and ptp config %s", "profile: \\s*([a-zA-Z0-9]+)", ptpConfig)
	return ""
}
