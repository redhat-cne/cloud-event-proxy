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

package main

import (
	"bufio"
	"fmt"
	v2 "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptp_event "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"net"
	"os"
	"strings"
	"time"

	ceevent "github.com/redhat-cne/sdk-go/pkg/event"

	log "github.com/sirupsen/logrus"
	"sync"

	ptp_metrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

const(
	ptp4lProcessName   = "ptp4l"
	phc2sysProcessName = "phc2sys"
)
var (
	resourceAddress string        = "/cluster/node/ptp"
	eventInterval   time.Duration = time.Second * 5
	config          *common.SCConfiguration
)

// Start ptp plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, configuration *common.SCConfiguration, fn func(e ceevent.Event) error) error { //nolint:deadcode,unused
	// The name of NodePtpDevice CR for this node is equal to the node name
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Error("cannot find NODE_NAME environment variable")
		return fmt.Errorf("cannot find NODE_NAME environment variable")
	}
	// register metrics type
	ptp_metrics.RegisterMetrics(nodeName)
	wg.Add(1)
	go listenToMetrics()

	// 1. Create event Publication
	var pub pubsub.PubSub
	var err error
	var e ceevent.Event
	config = configuration
	if pub, err = createPublisher(resourceAddress); err != nil {
		log.Errorf("failed to create a publisher %v", err)
		return err
	}
	log.Printf("Created publisher %v", pub)
	// 2.Create Status Listener
	onStatusRequestFn := func(e v2.Event) error {
		log.Info("got status check call,fire events for above publisher")
		event, _ := createPTPEvent(pub)
		_ = common.PublishEvent(config, event)
		return nil
	}
	v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onStatusRequestFn, fn)
	// 3. Fire initial Event
	log.Info("sending initial events ( probably not needed until consumer asks for it in initial state)")
	e, _ = createPTPEvent(pub)
	_ = common.PublishEvent(config, e)
	// event handler
	log.Info("spinning event loop")
	wg.Add(1)
	go sendEvents(wg, pub)
	return nil
}

func sendEvents(wg *sync.WaitGroup, pub pubsub.PubSub) {
	ticker := time.NewTicker(eventInterval)
	defer ticker.Stop()
	defer wg.Done()
	for {
		select {
		case <-ticker.C:
			log.Info("sending events")
			e, _ := createPTPEvent(pub)
			_ = common.PublishEvent(config, e)
		case <-config.CloseCh:
			log.Info("done")
			return
		}
	}
}
func createPublisher(address string) (pub pubsub.PubSub, err error) {
	// this is loopback on server itself. Since current pod does not create any server
	returnURL := fmt.Sprintf("%s%s", config.BaseURL, "dummy")
	pubToCreate := v1pubsub.NewPubSub(types.ParseURI(returnURL), address)
	pub, err = common.CreatePublisher(config, pubToCreate)
	if err != nil {
		log.Errorf("failed to create publisher %v", pub)
	}
	return pub, err
}

func createPTPEvent(pub pubsub.PubSub) (ceevent.Event, error) {
	// create an event
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  "/cluster/node/ptp",
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     ceevent.ACQUIRING_SYNC,
		},
		},
	}
	e, err := common.CreateEvent(pub.ID, "PTP_EVENT", data)
	return e, err
}

func listenToMetrics(){
	log.Info("establishing socket connection for metrics")
	l,err:=listen("/tmp/metrics.sock")
	if err!=nil {
		log.Errorf("error setting up socket %s",err)
		return
	}else{
		log.Info("connection established successfully")
	}
		for {
			fd, err := l.Accept()
			if err != nil {
				log.Errorf("accept error: %s", err)
			}else{
				go processMessages(fd)
			}
		}
}


func processMessages(c net.Conn) {
	scanner := bufio.NewScanner(c)
	for {
		ok := scanner.Scan()
		if !ok {
			break
		}
		msg:=scanner.Text()
		log.Printf("plugin got %s",msg)
		m:=strings.Split(msg,"::")
		if len(m)>2{
			if m[0]==phc2sysProcessName{
				ptp_event.ExtractEvent(m[0],m[2])
			}
			ptp_metrics.ExtractMetrics(m[0],m[1],m[2])
			// process data
		}
	}

}

var (
	staleSocketTimeout = 100 * time.Millisecond
)

func listen(addr string) (net.Listener, error) {
	uAddr, err := net.ResolveUnixAddr("unix", addr)
	if err != nil {
		return nil, err
	}

	// Try to listen on the socket. If that fails we check to see if it's a stale
	// socket and remove it if it is. Then we try to listen one more time.
	l, err := net.ListenUnix("unix", uAddr)
	if err != nil {
		if err = removeIfStaleUnixSocket(addr); err != nil {
			return nil, err
		}
		if l, err = net.ListenUnix("unix", uAddr); err != nil {
			return nil, err
		}
	}
	return l, err
}


// removeIfStaleUnixSocket takes in a path and removes it iff it is a socket
// that is refusing connections
func removeIfStaleUnixSocket(socketPath string) error {
	// Ensure it's a socket; if not return without an error
	if st, err := os.Stat(socketPath); err != nil || st.Mode()&os.ModeType != os.ModeSocket {
		return nil
	}
	// Try to connect
	conn, err := net.DialTimeout("unix", socketPath, staleSocketTimeout)
	if err!=nil {//=syscall.ECONNREFUSED {
		return os.Remove(socketPath)
	}else if err == nil {
		return conn.Close()
	}

	return nil
}
