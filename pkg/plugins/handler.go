package plugins

import (
	"fmt"
	"path/filepath"
	"plugin"
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	log "github.com/sirupsen/logrus"

	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

// Handler handler for loading plugins
type Handler struct {
	Path string
}

// LoadAMQPPlugin loads amqp plugin
func (pl Handler) LoadAMQPPlugin(wg *sync.WaitGroup, scConfig *common.SCConfiguration) (*v1amqp.AMQP, error) {
	log.Printf("Starting AMQP server")
	amqpPlugin, err := filepath.Glob(fmt.Sprintf("%s/amqp_plugin.so", pl.Path))
	if err != nil {
		log.Fatalf("cannot load amqp plugin %v\n", err)
		return nil, err
	}
	if len(amqpPlugin) == 0 {
		return nil, fmt.Errorf("amqp plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(amqpPlugin[0])
	if err != nil {
		log.Fatalf("cannot open amqp plugin %v", err)
		return nil, err
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Fatalf("cannot open amqp plugin start method %v", err)
		return nil, err
	}

	startFunc, ok := symbol.(func(wg *sync.WaitGroup, scConfig *common.SCConfiguration) (*v1amqp.AMQP, error))
	if !ok {
		log.Fatalf("Plugin has no 'Start(*sync.WaitGroup,*common.SCConfiguration) (*v1_amqp.AMQP,error)' function")
		return nil, fmt.Errorf("plugin has no 'start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
	}
	amqpInstance, err := startFunc(wg, scConfig)
	if err != nil {
		log.Printf("error starting amqp at %s error: %v", scConfig.AMQPHost, err)
		return amqpInstance, err
	}
	return amqpInstance, nil
}

// LoadPTPPlugin loads ptp plugin
func (pl Handler) LoadPTPPlugin(wg *sync.WaitGroup, scConfig *common.SCConfiguration, fn func(e ceevent.Event) error) error {
	restPlugin, err := filepath.Glob(fmt.Sprintf("%s/ptp_operator_plugin.so", pl.Path))
	if err != nil {
		log.Fatalf("cannot load ptp plugin %v", err)
	}
	if len(restPlugin) == 0 {
		return fmt.Errorf("ptp plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(restPlugin[0])
	if err != nil {
		log.Fatalf("cannot open ptp plugin %v", err)
		return err
	}

	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Fatalf("cannot open ptp plugin start method %v", err)
		return err
	}
	startFunc, ok := symbol.(func(*sync.WaitGroup, *common.SCConfiguration, func(e ceevent.Event) error) error)
	if !ok {
		log.Fatalf("Plugin has no 'Start(*sync.WaitGroup, *common.SCConfiguration, func(e ceevent.Event) error)(error)' function")
	}
	return startFunc(wg, scConfig, fn)
}
