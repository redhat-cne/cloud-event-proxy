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
)

// DependingClockState ...
type DependingClockState []*ClockState

// PTPEventState ...
type PTPEventState struct {
	sync.Mutex
	CurrentPTPStateEvent ptp.SyncState
	DependsOn            map[string]DependingClockState // [dpll][ens2f0]State
	Type                 ptp.EventType
}

// ClockState ...
type ClockState struct {
	State       ptp.SyncState
	Offset      *float64
	IFace       *string
	Process     string //dpll //gnss
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
func (p *PTPEventState) UpdateCurrentEventState(c ClockState) ptp.SyncState {
	p.Lock()
	defer func() {
		p.Unlock()
	}()
	// if the current state is not locked and the new state is locked then update the current state
	if dep, ok := p.DependsOn[c.Process]; ok {
		index, d := dep.hasClockState(*c.IFace)
		// update the clock state if found or else
		if index == -1 {
			d = &ClockState{}
			dep = append(dep, d)
		}
		d.Offset = c.Offset
		d.IFace = c.IFace
		d.State = c.State
		d.Process = c.Process
		d.Value = c.Value
		d.ClockSource = c.ClockSource
		// update the metric
		if c.Offset != nil {
			d.Offset = pointer.Float64(*c.Offset)
		}

		for k, v := range c.Value {
			iface := ""
			if d.IFace != nil {
				iface = *d.IFace
			}
			if d.Metric == nil {
				d.Metric = dep.hasMetric()
				d.HelpText = dep.hasMetricHelp()
			}
			if d.Metric[k].metricGauge != nil {
				d.Metric[k].metricGauge.With(map[string]string{"from": d.Process, "process": d.Process,
					"node": d.NodeName, "iface": iface}).Set(float64(v))
			}
		}
	} else {
		clockState := &ClockState{
			State:       c.State,
			Process:     c.Process,
			Value:       c.Value,
			ClockSource: c.ClockSource,
		}
		if c.Offset != nil {
			clockState.Offset = pointer.Float64(*c.Offset)
		}
		if c.IFace != nil {
			clockState.IFace = pointer.String(*c.IFace)
		}
		metrics := map[string]*PMetric{}
		for k, v := range c.Value {
			// create metrics
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
			err := prometheus.Register(metrics[k].metricGauge)
			if err != nil {
				log.Info("ignore, metric is already registered")
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
		p.DependsOn[c.Process] = []*ClockState{clockState}
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
func (p *PTPEventState) DeleteAllMetrics() {
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
					if v.metricGauge != nil && dd.IFace != nil {
						v.metricGauge.Delete(prometheus.Labels{"process": dd.Process, "iface": *dd.IFace, "node": dd.NodeName})
						prometheus.Unregister(v.metricGauge)
					}
				}
			}
			delete(p.DependsOn, dd.Process)
		}
	}
}
