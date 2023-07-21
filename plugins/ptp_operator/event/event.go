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

// PTPEventState ...
type PTPEventState struct {
	sync.Mutex
	CurrentPTPStateEvent ptp.SyncState
	DependsOn            map[string]*ClockState
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
	HelpText    string
}

// PMetric ...
type PMetric struct {
	isRegistered  bool
	metricGauge   *prometheus.GaugeVec
	metricCounter *prometheus.Counter
}

// UpdateCurrentEventState ...
func (p *PTPEventState) UpdateCurrentEventState(c ClockState) ptp.SyncState {
	p.Lock()
	defer func() {
		p.Unlock()
	}()
	// if the current state is not locked and the new state is locked then update the current state
	if d, ok := p.DependsOn[c.Process]; ok {
		d.Offset = c.Offset
		d.State = c.State
		d.Process = c.Process
		d.Value = c.Value
		d.ClockSource = c.ClockSource
		// update the metric
		if c.Offset != nil {
			d.Offset = pointer.Float64(*c.Offset)
		}
		p.DependsOn[c.Process] = d
		for k, v := range c.Value {
			iface := ""
			if d.IFace != nil {
				iface = *d.IFace
			}
			if d.Metric[k].metricGauge == nil {
				d.Metric[k].metricGauge.With(map[string]string{"from": d.Process, "process": d.Process,
					"node": d.NodeName, "iface": iface}).Set(float64(v))
			} else {
				log.Errorf("metric %s is not registered", k)
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
						Help:      c.HelpText,
					}, []string{"from", "process", "node", "iface"}),
				metricCounter: nil,
			}
			prometheus.MustRegister(metrics[k].metricGauge)
			iface := ""
			if clockState.IFace != nil {
				iface = *clockState.IFace
			}
			metrics[k].metricGauge.With(map[string]string{"from": clockState.Process, "process": clockState.Process,
				"node": clockState.NodeName, "iface": iface}).Set(float64(v))
		}
		clockState.Metric = metrics
		p.DependsOn[c.Process] = clockState
	}

	// if all locked then its locked
	// if anyone HOLDOVER then holdover
	// if  anyone FREERUN then freerun
	var currentState ptp.SyncState
	for _, v := range p.DependsOn {
		if v.State != ptp.LOCKED {
			if v.State == ptp.HOLDOVER {
				currentState = v.State
				break
			}
			currentState = v.State
		} else if currentState == "" {
			currentState = v.State
		}
	}
	p.CurrentPTPStateEvent = currentState
	return p.CurrentPTPStateEvent
}

// UnRegisterMetrics ... unregister  metrics by processname
func (p *PTPEventState) UnRegisterMetrics(processName string) {
	//	write a functions to unregister meteric from dependson object
	if d, ok := p.DependsOn[processName]; ok {
		// write loop d.Metric
		if d.Metric != nil {
			for _, v := range d.Metric {
				prometheus.Unregister(v.metricGauge)
			}
		}
	}
}

// UnRegisterAllMetrics ...  unregister all metrics
//
//	write a functions to unregister meteric from dependson object
func (p *PTPEventState) UnRegisterAllMetrics() {
	// write loop p.DependsOn
	for _, d := range p.DependsOn {
		// write loop d.Metric
		if d.Metric != nil {
			// unregister metric
			for _, v := range d.Metric {
				prometheus.Unregister(v.metricGauge)
				delete(p.DependsOn, d.Process)
			}
		}
	}
}

// DeleteAllMetrics ...  delete all metrics
//
//	write a functions to delete meteric from dependson object
func (p *PTPEventState) DeleteAllMetrics() {
	// write loop p.DependsOn
	for _, d := range p.DependsOn {
		// write loop d.Metric
		if d.Metric != nil {
			// unregister metric
			for _, v := range d.Metric {
				v.metricGauge.Delete(prometheus.Labels{"process": d.Process, "iface": *d.IFace, "node": d.NodeName})
			}
		}
		delete(p.DependsOn, d.Process)
	}
}
