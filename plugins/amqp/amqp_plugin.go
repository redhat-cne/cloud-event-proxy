package main

import (
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"sync"

	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

// Start amqp  services to process events,metrics and status
func Start(wg *sync.WaitGroup, config *common.SCConfiguration) (amqpInstance *v1amqp.AMQP, err error) { //nolint:deadcode,unused
	if amqpInstance, err = v1amqp.GetAMQPInstance(config.AMQPHost, config.EventInCh, config.EventOutCh, config.CloseCh); err != nil {
		return
	}
	amqpInstance.Start(wg)
	return
}
