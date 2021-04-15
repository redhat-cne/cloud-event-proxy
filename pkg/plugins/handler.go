package plugins

import (
	"fmt"
	"log"
	"path/filepath"
	"plugin"
	"sync"

	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"

	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

// Handler handler for loading plugins
type Handler struct {
	Path string
}

// LoadAMQPPlugin loads amqp plugin
func (pl Handler) LoadAMQPPlugin(wg *sync.WaitGroup, amqpHost string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan, closeCh <-chan bool) (*v1amqp.AMQP, error) {
	log.Printf("Starting AMQP server")
	amqpPlugin, err := filepath.Glob(fmt.Sprintf("%s/amqp_plugin.so", pl.Path))
	if err != nil {
		log.Fatalf("cannot load amqp plugin %v", err)
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

	startFunc, ok := symbol.(func(wg *sync.WaitGroup, amqpHost string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan, closeCh <-chan bool) (*v1amqp.AMQP, error))
	if !ok {
		log.Fatalf("Plugin has no 'Start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
		return nil, fmt.Errorf("plugin has no 'start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
	}
	amqpInstance, err := startFunc(wg, amqpHost, dataIn, dataOut, closeCh)
	if err != nil {
		log.Printf("error starting amqp.%v", err)
		return amqpInstance, err
	}
	return amqpInstance, nil
}

// LoadRestPlugin load rest plugin
func (pl Handler) LoadRestPlugin(wg *sync.WaitGroup, port int, apiPath, storePath string, eventOutCh chan<- *channel.DataChan, closeCh <-chan bool) (*restapi.Server, error) {
	restPlugin, err := filepath.Glob(fmt.Sprintf("%s/rest_api_plugin.so", pl.Path))
	if err != nil {
		log.Fatalf("cannot load rest plugin %v", err)
	}
	if len(restPlugin) == 0 {
		return nil, fmt.Errorf("rest plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(restPlugin[0])
	if err != nil {
		log.Fatalf("cannot open rest plugin %v", err)
		return nil, err
	}

	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Fatalf("cannot open rest plugin start method %v", err)
		return nil, err
	}
	startFunc, ok := symbol.(func(*sync.WaitGroup, int, string, string, chan<- *channel.DataChan, <-chan bool) *restapi.Server)
	if !ok {
		log.Fatalf("Plugin has no 'Start(int, string, string, chan<- channel.DataChan,<-chan bool)(*rest_api.Server)' function")
	}
	return startFunc(wg, port, apiPath, storePath, eventOutCh, closeCh), nil
}

// LoadPTPPlugin loads ptp plugin
func (pl Handler) LoadPTPPlugin(wg *sync.WaitGroup, api *v1pubsub.API, eventInCh chan<- *channel.DataChan, closeCh <-chan bool, fn func(e ceevent.Event) error) error {
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
	startFunc, ok := symbol.(func(*sync.WaitGroup, *v1pubsub.API, chan<- *channel.DataChan, <-chan bool, func(e ceevent.Event) error) error)
	if !ok {
		log.Fatalf("Plugin has no 'Start(*sync.WaitGroup, *v1pubsub.API,  chan<- *channel.DataChan, <-chan bool, func(e ceevent.Event) error)(error)' function")
	}
	return startFunc(wg, api, eventInCh, closeCh, fn)
}
