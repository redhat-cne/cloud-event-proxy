package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/redhat-cne/cloud-event-proxy/examples/simplehttp/common"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
)

var (
	resourcePrefix     string = "/cluster/node/%s%s"
	subs               []pubsub.PubSub
	nodeName           string
	clientID           uuid.UUID
	stopHTTPServerChan chan bool
	wg                 sync.WaitGroup
)

// oc -n openshift-ptp port-forward linuxptpdaemon 9043:9043 --address='0.0.0.0
const (
	clientAddress          = ":27017"
	clientExternalEndPoint = "http://event-consumer-external:27017"
	publisherServiceName   = "http://localhost:9043"
)

func init() {
	clientID = func(serviceName string) uuid.UUID {
		var namespace = uuid.NameSpaceURL
		var url = []byte(serviceName)
		return uuid.NewMD5(namespace, url)
	}(clientExternalEndPoint)
}

func initResources() {
	for _, resource := range common.GetResources() {
		subs = append(subs, pubsub.PubSub{
			ID:       getUUID(fmt.Sprintf(resourcePrefix, nodeName, resource)).String(),
			Resource: fmt.Sprintf(resourcePrefix, nodeName, resource),
		})
	}
}

func main() {
	cancelChan := make(chan os.Signal, 1)
	stopHTTPServerChan = make(chan bool)
	// catch SIGETRM or SIGINTERRUPT
	signal.Notify(cancelChan, syscall.SIGTERM, syscall.SIGINT)
	wg = sync.WaitGroup{}
	nodeName = os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Printf("please set env variable, export NODE_NAME=k8s nodename")
		os.Exit(1)
	}
	//1. health check on publisher endpoint-check for availability
	healthCheckPublisher()

	//2. consumer app - spin web server to receive events
	wg.Add(1)
	go common.StartServer(&wg, clientAddress, stopHTTPServerChan)

	// EVENT subscription and consuming
	initResources()
	// 1.first subscribe to all resources
	if e := common.Subscribe(clientID, subs, nodeName, fmt.Sprintf("%s/subscription", publisherServiceName),
		clientExternalEndPoint); e != nil {
		log.Printf("error processing subscription %s", e)
		stopHTTPServerChan <- true
		os.Exit(1)
	}
	// 2. event will be received at /event as event happens
	// Polling for events
	//2.call get current state once for all three resource
	common.PrintHeader()
	// get current state
	callGetCurrentState()
	fmt.Println("\n---------------------------------------------------------------------------------")
	//wait for 5 secs for any events and then delete subscription
	<-cancelChan
	callGetCurrentState()
	common.DeleteSubscription(fmt.Sprintf("%s/subscription", publisherServiceName), clientID)
	stopHTTPServerChan <- true
	wg.Wait()
}
func callGetCurrentState() {
	for _, r := range subs {
		if event, data, err := common.GetCurrentState(clientID, publisherServiceName, r.Resource); err == nil {
			common.PrintEvent(data, event.Type(), event.Time())
		} else {
			fmt.Println(common.ColorRed, "PTP not set")
		}
	}
}

func getUUID(s string) uuid.UUID {
	var namespace = uuid.NameSpaceURL
	var url = []byte(s)
	return uuid.NewMD5(namespace, url)
}

func help() {
	fmt.Println("--------------------------------")
	fmt.Println("1. setting up port forwarding to the container port to expose the service external to k8s")
	fmt.Println(" oc get pods -n openshift-ptp -l app=linuxptp-daemon")
	fmt.Println("NAME                            READY   STATUS    RESTARTS   AGE")
	fmt.Println("linuxptp-daemon-xmjcr           3/3     Running   0          13h")
	fmt.Println("")
	fmt.Println("Run following command to set port forwarding")
	fmt.Println("oc -n openshift-ptp port-forward linuxptp-daemon-xmjcr 9043:9043 --address='0.0.0.0'")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println("2. Setup endpoint for k8s to access service running externally in laptop")
	fmt.Println("edit the file to update your laptop ipaddress")
	fmt.Println("oc apply -f examples/simplehttp/external-service.yaml -n openshift-ptp")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println("set up NODE_NAME ENV variable to build subscriptions to the events")
	fmt.Println("export NODE_NAME=k8s nodename")
	fmt.Println("----------------------------------------------------------------")
	fmt.Println("go run main.go")
}

func healthCheckPublisher() {
	resp, err := http.Get(fmt.Sprintf("%s/health", publisherServiceName))
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			log.Printf("port forwarding is not set to access k8s service")
			help()
			log.Fatalln(err)
		}
		log.Fatalln(err)
	}
	defer resp.Body.Close()
}
