package plugins

import (
	"fmt"
	"log"
	"path/filepath"
	"plugin"
	"sync"

	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

//Handler handler for loading plugins
type Handler struct {
	Path string
}

// LoadAMQPPlugin ...
func (pl Handler) LoadAMQPPlugin(wg *sync.WaitGroup, amqpHost string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan, close <-chan bool) error {
	log.Printf("Starting AMQP server")
	amqpPlugin, err := filepath.Glob(fmt.Sprintf("%s/amqp_plugin.so", pl.Path))
	if err != nil {
		log.Fatalf("cannot load amqp plugin %v", err)
		return err
	}
	if len(amqpPlugin) == 0 {
		return fmt.Errorf("amqp plugin not found in the path %s", pl.Path)
	}
	p, err := plugin.Open(amqpPlugin[0])
	if err != nil {
		log.Fatalf("cannot open amqp plugin %v", err)
		return err
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Fatalf("cannot open amqp plugin start method %v", err)
		return err
	}

	startFunc, ok := symbol.(func(wg *sync.WaitGroup, amqpHost string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan, close <-chan bool) (*v1amqp.AMQP, error))
	if !ok {
		log.Fatalf("Plugin has no 'Start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
		return fmt.Errorf("plugin has no 'start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
	}
	_, err = startFunc(wg, amqpHost, dataIn, dataOut, close)
	if err != nil {
		log.Printf("error starting amqp.%v", err)
		return err
	}
	return nil
}

//LoadRestPlugin ...
func (pl Handler) LoadRestPlugin(wg *sync.WaitGroup, port int, apiPath, storePath string, eventOutCh chan<- *channel.DataChan, close <-chan bool) (*restapi.Server, error) {

	// The plugins (the *.so files) must be in a 'plugins' sub-directory
	//all_plugins, err := filepath.Glob("plugins/*.so")
	//if err != nil {
	//	panic(err)
	//}
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

	return startFunc(wg, port, apiPath, storePath, eventOutCh, close), nil

}
