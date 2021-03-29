package main

import (
	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"sync"
)

//var server *restapi.Server

//Start start rest service to creat delete , update and handle subscriptions,publishers, events and status
func Start(wg *sync.WaitGroup, port int, apiPath, storePath string, eventOutCh chan<- channel.DataChan) { //nolint:deadcode,unused
	server := restapi.InitServer(port, apiPath, storePath, eventOutCh)
	server.Start(wg)
}
