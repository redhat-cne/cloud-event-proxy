package main

import (
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1_amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"log"
	"path/filepath"
	"plugin"
	"sync"
)

var (
	eventOutCh chan channel.DataChan
	eventInCh  chan channel.DataChan
	close      chan bool
)

func init() {
	eventOutCh = make(chan channel.DataChan, 10)
	eventInCh = make(chan channel.DataChan, 10)
	close = make(chan bool)
}
func main() {
	wg := &sync.WaitGroup{}
	//base con configuration we should be able to build this plugin
	loadAMQPPlugin(wg)
	loadRestPlugin(wg)
	wg.Wait()
}
func loadAMQPPlugin(wg *sync.WaitGroup) {

	amqpPlugin, err := filepath.Glob("plugins/amqp_plugin.so")
	if err != nil {
		log.Printf("cannot load amqp plugin %v", err)
		return
	}
	p, err := plugin.Open(amqpPlugin[0])
	if err != nil {
		log.Printf("cannot open amqp plugin %v", err)
		return
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Printf("cannot open amqp plugin start method %v", err)
		return
	}

	startFunc, ok := symbol.(func(wg *sync.WaitGroup, amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP, error))
	if !ok {
		log.Printf("Plugin has no 'Start(amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP,error)' function")
		return
	}
	_, err = startFunc(wg, "amqp://localhost:5672", eventInCh, eventOutCh, close)
	if err != nil {
		log.Printf("error starting amqp.%v", err)
	}

}
func loadRestPlugin(wg *sync.WaitGroup) {

	// The plugins (the *.so files) must be in a 'plugins' sub-directory
	//all_plugins, err := filepath.Glob("plugins/*.so")
	//if err != nil {
	//	panic(err)
	//}
	restPlugin, err := filepath.Glob("plugins/rest_api_plugin.so")
	if err != nil {
		log.Printf("cannot load rest plugin %v", err)
		return
	}
	p, err := plugin.Open(restPlugin[0])
	if err != nil {
		log.Printf("cannot open rest plugin %v", err)
		return
	}
	symbol, err := p.Lookup("Start")
	if err != nil {
		log.Printf("cannot open rest plugin start method %v", err)
		return
	}

	startFunc, ok := symbol.(func(*sync.WaitGroup, int, string, string, chan<- channel.DataChan))
	if !ok {
		log.Printf("Plugin has no 'Start(int, string, string, chan<- channel.DataChan)' function")
	}
	startFunc(wg, 8080, "api/cnf/", ".", eventOutCh)

}
