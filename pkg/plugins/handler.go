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

package plugins

import (
	"fmt"
	"path/filepath"
	"plugin"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/sdk-go/pkg/channel"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	log "github.com/sirupsen/logrus"

	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	v1http "github.com/redhat-cne/sdk-go/v1/http"
)

// Handler handler for loading plugins
type Handler struct {
	Path string
}

// LoadAMQPPlugin loads amqp plugin
func (pl Handler) LoadAMQPPlugin(wg *sync.WaitGroup, scConfig *common.SCConfiguration, amqInitTimeout time.Duration) (*v1amqp.AMQP, error) {
	log.Infof("Starting AMQP server with amqInitTimeout set to %v", amqInitTimeout)
	amqpPlugin, err := filepath.Glob(fmt.Sprintf("%s/amqp_plugin.so", pl.Path))
	if err != nil {
		log.Errorf("cannot load amqp plugin %v\n", err)
		return nil, err
	}
	if len(amqpPlugin) == 0 {
		return nil, fmt.Errorf("amqp plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(amqpPlugin[0])
	if err != nil {
		log.Errorf("cannot open amqp plugin %v", err)
		return nil, err
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Errorf("cannot open amqp plugin start method %v", err)
		return nil, err
	}

	startFunc, ok := symbol.(func(wg *sync.WaitGroup, scConfig *common.SCConfiguration, amqInitTimeout time.Duration) (*v1amqp.AMQP, error))
	if !ok {
		log.Errorf("AMQ Plugin has no 'Start(*sync.WaitGroup,*common.SCConfiguration) (*v1_amqp.AMQP,error)' function")
		return nil, fmt.Errorf("AMQ plugin has no 'start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
	}
	amqpInstance, err := startFunc(wg, scConfig, amqInitTimeout)
	if err != nil {
		log.Errorf("error starting amqp at %s error: %v", scConfig.TransportHost.URL, err)
		return amqpInstance, err
	}
	scConfig.TransPortInstance = amqpInstance
	return amqpInstance, nil
}

// LoadPTPPlugin loads ptp plugin
func (pl Handler) LoadPTPPlugin(wg *sync.WaitGroup, scConfig *common.SCConfiguration, fn func(e interface{}) error) error {
	pttPlugin, err := filepath.Glob(fmt.Sprintf("%s/ptp_operator_plugin.so", pl.Path))
	if err != nil {
		log.Errorf("cannot load ptp plugin %v", err)
	}
	if len(pttPlugin) == 0 {
		return fmt.Errorf("ptp plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(pttPlugin[0])
	if err != nil {
		log.Errorf("cannot open ptp plugin %v", err)
		return err
	}

	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Errorf("cannot open ptp plugin start method %v", err)
		return err
	}
	startFunc, ok := symbol.(func(*sync.WaitGroup, *common.SCConfiguration, func(e interface{}) error) error)
	if !ok {
		log.Errorf("PTP Plugin has no 'Start(*sync.WaitGroup, *common.SCConfiguration,  fn func(e interface{}) error)(error)' function")
		return fmt.Errorf("PTP Plugin has no 'Start(*sync.WaitGroup, *common.SCConfiguration,  fn func(e interface{}) error)(error)' function")
	}
	return startFunc(wg, scConfig, fn)
}

// LoadMockPlugin loads mock test  plugin
func (pl Handler) LoadMockPlugin(wg *sync.WaitGroup, scConfig *common.SCConfiguration, fn func(e interface{}) error) error {
	mockPlugin, err := filepath.Glob(fmt.Sprintf("%s/mock_plugin.so", pl.Path))
	if err != nil {
		log.Errorf("cannot load mock plugin %v", err)
	}
	if len(mockPlugin) == 0 {
		return fmt.Errorf("mock plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(mockPlugin[0])
	if err != nil {
		log.Errorf("cannot open mock plugin %v", err)
		return err
	}

	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Errorf("cannot open mock plugin start method %v", err)
		return err
	}
	startFunc, ok := symbol.(func(*sync.WaitGroup, *common.SCConfiguration, func(e interface{}) error) error)
	if !ok {
		log.Errorf("Mock Plugin has no 'Start(*sync.WaitGroup, *common.SCConfiguration,  fn func(e interface{}) error)(error)' function")
	}
	return startFunc(wg, scConfig, fn)
}

// LoadHTTPPlugin loads http test  plugin
func (pl Handler) LoadHTTPPlugin(wg *sync.WaitGroup, scConfig *common.SCConfiguration,
	onStatusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error, fn func(e interface{}) error) (*v1http.HTTP, error) {
	httPlugin, err := filepath.Glob(fmt.Sprintf("%s/http_plugin.so", pl.Path))
	if err != nil {
		log.Errorf("cannot load http plugin %v", err)
	}
	if len(httPlugin) == 0 {
		return nil, fmt.Errorf("http plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(httPlugin[0])
	if err != nil {
		log.Errorf("cannot open http plugin %v", err)
		return nil, err
	}

	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Errorf("cannot open http plugin start method %v", err)
		return nil, err
	}
	startFunc, ok := symbol.(func(*sync.WaitGroup, *common.SCConfiguration, func(e cloudevents.Event, dataChan *channel.DataChan) error, func(e interface{}) error) (*v1http.HTTP, error))
	if !ok {
		log.Errorf("HTTP Plugin has no 'Start(*sync.WaitGroup, *common.SCConfiguration,func(e cloudevents.Event, dataChan *channel.DataChan) error, fn func(e interface{}) error)(error)' function")
		return nil, fmt.Errorf("HTTP Plugin has no 'Start(*sync.WaitGroup, *common.SCConfiguration,func(e cloudevents.Event, dataChan *channel.DataChan) error, fn func(e interface{}) error)(error)' function")
	}
	httpInstance, err := startFunc(wg, scConfig, onStatusReceiveOverrideFn, fn)
	if err != nil {
		log.Errorf("error starting http at %s error: %v", scConfig.TransportHost.URL, err)
		return httpInstance, err
	}
	scConfig.TransPortInstance = httpInstance
	return httpInstance, nil
}
