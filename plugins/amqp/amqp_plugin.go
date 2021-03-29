package main

import (
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1_amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"sync"
)

//Start amqp  services to process events,metrics and status
func Start(wg *sync.WaitGroup, amqpHost string, dataIn <-chan channel.DataChan, dataOut chan<- channel.DataChan, close <-chan bool) (*v1_amqp.AMQP, error) { //nolint:deadcode,unused
	amqpInstance, err := v1_amqp.GetAMQPInstance(amqpHost, dataIn, dataOut, close)
	if err == nil {
		amqpInstance.Start(wg)
	}
	return amqpInstance, err
}
