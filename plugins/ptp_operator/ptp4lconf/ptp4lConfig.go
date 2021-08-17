package ptp4lconf

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ptpConfigFileRegEx = regexp.MustCompile(`ptp4l.[0-9]*.config`)
	sectionHead        = regexp.MustCompile(`\[([^\[\]]*)\]`)
)

const (
	ptp4lGlobalSection = "global"
)

//PtpConfigUpdate ...
type PtpConfigUpdate struct {
	Name      *string `json:"name,omitempty"`
	Ptp4lConf *string `json:"ptp4lConf,omitempty"`
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
		" removed:%t", name, ptp4lConf, p.Removed)
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

//PTPInterface ...
type PTPInterface struct {
	Name     string
	PortID   int
	PortName string
	Role     types.PtpPortRole
}

//PTP4lConfig ...
type PTP4lConfig struct {
	Name       string
	Interfaces []*PTPInterface
}

//ByRole ...
func (Ptp4lCfg *PTP4lConfig) ByRole(role types.PtpPortRole) (PTPInterface, error) {
	for _, p := range Ptp4lCfg.Interfaces {
		if p != nil && p.Role == role {
			return *p, nil
		}
	}
	return PTPInterface{}, fmt.Errorf("interfaces not found for the role %d --> %s", role, Ptp4lCfg.String())
}

//UpdateRole ...
func (pi *PTPInterface) UpdateRole(role types.PtpPortRole) {
	pi.Role = role
}

func (Ptp4lCfg *PTP4lConfig) String() string {
	b := strings.Builder{}
	b.WriteString("  configName: " + Ptp4lCfg.Name + "\r\n")
	b.WriteString("  Ptp4lConfigInterfaces: " + "\n")
	for _, p := range Ptp4lCfg.Interfaces {
		b.WriteString("  name: " + p.Name + "\n")
		b.WriteString("  portName: " + p.PortName + "\n")
		b.WriteString("  role: " + p.Role.String() + "\n")
		b.WriteString("  portID: " + fmt.Sprintf("%d", p.PortID) + "\n")
	}
	return b.String()
}

//ByInterface ...
func (Ptp4lCfg *PTP4lConfig) ByInterface(iface string) (PTPInterface, error) {
	for _, p := range Ptp4lCfg.Interfaces {
		if p != nil && p.Name == iface {
			return *p, nil
		}
	}
	return PTPInterface{}, fmt.Errorf("interfaces not found for the interface %s", iface)
}

//ByPortID ...
func (Ptp4lCfg *PTP4lConfig) ByPortID(id int) (PTPInterface, error) {
	for _, p := range Ptp4lCfg.Interfaces {
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

//Close ...
func (w *Watcher) Close() {
	close(w.close)
}

//NewPtp4lConfigWatcher ...
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
		//initialize all
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
					if checkIfPtP4lConf(event.Name) {
						if ptpConfig, err := readConfig(event.Name); err == nil {
							log.Infof("sending ptpconfig changes for %s", *ptpConfig.Name)
							updatedConfig <- ptpConfig
						}
					}
				} else if event.Op&fsnotify.Remove == fsnotify.Remove {
					if checkIfPtP4lConf(event.Name) {
						fName := filename(event.Name)
						ptpConfig := &PtpConfigUpdate{
							Name:      &fName,
							Ptp4lConf: nil,
							Removed:   true,
						}
						log.Println("config removed file:", event.Name)
						updatedConfig <- ptpConfig
					}
				}
			case err, ok := <-w.fsWatcher.Errors:
				if !ok {
					log.Errorf("cannot read watcher error %s", err)
					continue
				}
				log.Errorf("error while watching for file changes %s", err)
			}
		}
	}()
	err = w.fsWatcher.Add(dirToWatch)
	return w, err

}

func readAllConfig(dir string) []*PtpConfigUpdate {
	var ptpConfigs []*PtpConfigUpdate
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Errorf("error reading all config fils %s", err)
	}
	for _, file := range files {
		if checkIfPtP4lConf(file.Name()) {
			if p, err := readConfig(filepath.Join(dir, file.Name())); err == nil {
				ptpConfigs = append(ptpConfigs, p)
			}
		}
	}
	return ptpConfigs
}
func readConfig(path string) (*PtpConfigUpdate, error) {
	fName := filename(path)
	b, err := ioutil.ReadFile(path)
	if err != nil {
		log.Errorf("error reading ptpconfig %s error %s", path, err)
		return nil, err
	}
	data := string(b)
	ptpConfig := &PtpConfigUpdate{
		Name:      &fName,
		Ptp4lConf: &data,
	}
	return ptpConfig, nil
}

func filename(path string) string {
	r, e := regexp.Compile("([^/]+$)")
	if e != nil {
		log.Errorf("error finding ptp4l config filename for path %s error %s", path, e)
		return path
	}
	return r.FindString(path)
}

func checkIfPtP4lConf(filename string) bool {
	return ptpConfigFileRegEx.MatchString(filename)
}
