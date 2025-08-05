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

// Package plugins ...
package plugins

import (
	"fmt"
	"path/filepath"
	"plugin"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	log "github.com/sirupsen/logrus"
)

// Handler handler for loading plugins
type Handler struct {
	Path string
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
