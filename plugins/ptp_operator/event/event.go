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

package event

import (
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
	"k8s.io/utils/pointer"
)

const (
	ptpNamespace = "openshift"
	ptpSubsystem = "ptp"
)

// ClockSourceType ...
type ClockSourceType string

const (
	// GNSS ... ClockSourceType = "gnss"
	GNSS ClockSourceType = "gnss"
	// DPLL ... ClockSourceType = "dpll"
	DPLL ClockSourceType = "dpll"
	// GM ... ClockSourceType
	GM ClockSourceType = "GM"
)

// DependingClockState ...
type DependingClockState []*ClockState

// PTPEventState ...
type PTPEventState struct {
	sync.Mutex
	CurrentPTPStateEvent ptp.SyncState
	DependsOn            map[string]DependingClockState // [dpll][]*ClockState
	Type                 ptp.EventType
}

// ClockState ...
type ClockState struct {
	State       ptp.SyncState // gives over all state as LOCKED OR not
	Offset      *float64      // for syne no offset for now until frequency offset is provided
	IFace       *string       // could be iface gnss , sync1
	Process     string        //dpll //gnss
	ClockSource ClockSourceType
	Value       map[string]int64
	Metric      map[string]*PMetric
	NodeName    string
	HelpText    map[string]string
}

// PMetric ...
type PMetric struct {
	isRegistered  bool
	metricGauge   *prometheus.GaugeVec
	metricCounter *prometheus.Counter
}

func (dt *DependingClockState) hasClockState(iface string) (int, *ClockState) {
	for index, dep := range *dt {
		if *dep.IFace == iface {
			return index, dep
		}
	}
	return -1, nil
}

func (dt *DependingClockState) hasMetric() map[string]*PMetric {
	for _, dep := range *dt {
		return dep.Metric
	}
	return nil
}
func (dt *DependingClockState) hasMetricHelp() map[string]string {
	for _, dep := range *dt {
		return dep.HelpText
	}
	return nil
}

// UpdateCurrentEventState ...
func (p *PTPEventState) UpdateCurrentEventState(c ClockState, metrics map[string]*PMetric, help map[string]string) ptp.SyncState {
	p.Lock()
	defer func() {
		p.Unlock()
	}()
	// if the current state is not locked and the new state is locked then update the current state
	if dep, ok := p.DependsOn[c.Process]; ok {
		index, clockState := dep.hasClockState(*c.IFace)
		// update the clock state if found or else
		if index == -1 {
			clockState = &ClockState{}
			clockState.Metric = dep.hasMetric() //possible same metrics available outside the nic stats
			clockState.HelpText = dep.hasMetricHelp()
			dep = append(dep, clockState)
			p.DependsOn[c.Process] = dep
		}
		clockState.Offset = c.Offset
		clockState.IFace = c.IFace
		clockState.State = c.State
		clockState.Process = c.Process
		clockState.Value = c.Value
		clockState.ClockSource = c.ClockSource
		clockState.NodeName = c.NodeName
		// update the metric
		if c.Offset != nil {
			clockState.Offset = pointer.Float64(*c.Offset)
		}
		if clockState.IFace != nil {
			iface := *clockState.IFace
			r := []rune(iface)
			alias := string(r[:len(r)-1]) + "x"
			for k, v := range c.Value {
				if clockState.Metric[k].metricGauge != nil {
					clockState.Metric[k].metricGauge.With(map[string]string{"from": clockState.Process, "process": clockState.Process,
						"node": clockState.NodeName, "iface": alias}).Set(float64(v))
				} else {
					log.Infof("metric object was not found for %s=%s", iface, k)
				}
			}
		} else {
			log.Error("interface name was empty, skipping creation of metrics")
		}
	} else {
		clockState := &ClockState{
			State:       c.State,
			Process:     c.Process,
			Value:       c.Value,
			ClockSource: c.ClockSource,
			NodeName:    c.NodeName,
		}
		if c.Offset != nil {
			clockState.Offset = pointer.Float64(*c.Offset)
		}
		if c.IFace != nil {
			clockState.IFace = pointer.String(*c.IFace)
		}
		if metrics == nil {
			metrics = map[string]*PMetric{}
			help = map[string]string{}
		}

		for k, v := range c.Value {
			// create metrics
			if m, hasMetric := metrics[k]; hasMetric {
				metrics[k] = m
				if h, hasHelpTxt := help[k]; hasHelpTxt {
					clockState.HelpText[k] = h // expects to have help text for value
				}
			} else {
				metrics[k] = &PMetric{
					isRegistered: true,
					metricGauge: prometheus.NewGaugeVec(
						prometheus.GaugeOpts{
							Namespace: ptpNamespace,
							Subsystem: ptpSubsystem,
							Name:      k,
							Help: func() string {
								if h, ok2 := c.HelpText[k]; ok2 {
									return h
								}
								return ""
							}(),
						}, []string{"from", "process", "node", "iface"}),
					metricCounter: nil,
				}
			}

			err := prometheus.Register(metrics[k].metricGauge)
			if err != nil {
				log.Infof("ignore, metric for %s is already  for interface %s and process %s", k, *clockState.IFace, clockState.Process)
			}
			iface := ""
			if clockState.IFace != nil {
				iface = *clockState.IFace
			}
			r := []rune(iface)
			alias := string(r[:len(r)-1]) + "x"
			metrics[k].metricGauge.With(map[string]string{"from": clockState.Process, "process": clockState.Process,
				"node": clockState.NodeName, "iface": alias}).Set(float64(v))
		}
		clockState.Metric = metrics
		p.DependsOn[clockState.Process] = []*ClockState{clockState}
	}
	// if all locked then its locked
	// if anyone HOLDOVER then holdover
	// if  anyone FREERUN then freerun
	var currentState ptp.SyncState
	for _, v := range p.DependsOn {
		for _, dd := range v {
			if dd.State != ptp.LOCKED {
				if dd.State == ptp.HOLDOVER {
					currentState = dd.State
					break
				}
				currentState = dd.State
			} else if currentState == "" {
				currentState = dd.State
			}
		}
	}
	p.CurrentPTPStateEvent = currentState
	return p.CurrentPTPStateEvent
}

// UnRegisterMetrics ... unregister  metrics by processname
func (p *PTPEventState) UnRegisterMetrics(processName string) {
	//	write a functions to unregister metric from dependence on object
	if d, ok := p.DependsOn[processName]; ok {
		// write loop d.Metric
		for _, dd := range d {
			if dd.Metric != nil {
				for _, v := range dd.Metric {
					prometheus.Unregister(v.metricGauge)
				}
			}
		}
	}
}

// UnRegisterAllMetrics ...  unregister all metrics
//
//	write a functions to unregister meteric from dependson object
func (p *PTPEventState) UnRegisterAllMetrics() {
	// write loop p.DependsOn
	if p.DependsOn == nil {
		return
	}
	for _, d := range p.DependsOn {
		// write loop d.Metric
		for _, dd := range d {
			if dd.Metric != nil {
				// unregister metric
				for _, v := range dd.Metric {
					prometheus.Unregister(v.metricGauge)
					delete(p.DependsOn, dd.Process)
				}
			}
		}
	}
}

// DeleteAllMetrics ...  delete all metrics
//
//	write a functions to delete meteric from dependson object
func (p *PTPEventState) DeleteAllMetrics(m []*prometheus.GaugeVec) {
	// write loop p.DependsOn
	if p.DependsOn == nil {
		return
	}
	for _, d := range p.DependsOn {
		// write loop d.Metric
		for _, dd := range d {
			if dd.IFace == nil {
				continue
			}
			r := []rune(*dd.IFace)
			alias := string(r[:len(r)-1]) + "x"
			if dd.Metric != nil {
				// unregister metric
				for _, v := range dd.Metric {
					if v.metricGauge != nil && dd.IFace != nil {
						v.metricGauge.Delete(prometheus.Labels{"process": dd.Process, "iface": alias, "node": dd.NodeName})
						prometheus.Unregister(v.metricGauge)
					}
				}
				for _, mm := range m {
					mm.Delete(prometheus.Labels{
						"process": dd.Process, "from": dd.Process, "node": dd.NodeName, "iface": alias})
					// find metrics without from - click clock state
					mm.Delete(prometheus.Labels{
						"process": dd.Process, "node": dd.NodeName, "iface": alias})
				}
			}
			delete(p.DependsOn, dd.Process)
		}
	}
}

// PrintDependsOn ...
func (p *PTPEventState) PrintDependsOn() string {
	out := strings.Builder{}
	for process, d := range p.DependsOn {
		out.WriteString("Depending Process (key): " + process + "\n")
		for _, dd := range d {
			if dd.IFace != nil {
				out.WriteString("IFace " + *dd.IFace + "\n")
			}
			out.WriteString("ClockSource " + string(dd.ClockSource) + "\n")
			out.WriteString("Process " + dd.Process + "\n")
		}
		out.WriteString("\n")
	}
	return out.String()
}

// PrintClockState ...
func (c *ClockState) PrintClockState() string {
	out := strings.Builder{}
	out.WriteString("ClockState Process: " + c.Process + "\n")
	if c.IFace != nil {
		out.WriteString("Iface: " + *c.IFace + "\n")
	}
	for k := range c.Value {
		out.WriteString("Name: " + k + "\n")
	}
	return out.String()
}
