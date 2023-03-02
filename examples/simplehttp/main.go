package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/redhat-cne/cloud-event-proxy/examples/simplehttp/common"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	log "github.com/sirupsen/logrus"
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
	clientAddress          = "localhost:3306"
	clientExternalEndPoint = "http://event-consumer-external:3306"
	publisherServiceName   = "http://localhost:9043"
)

func init() {
	clientID = func(serviceName string) uuid.UUID {
		var namespace = uuid.NameSpaceURL
		var url = []byte(serviceName)
		return uuid.NewMD5(namespace, url)
	}(fmt.Sprintf("%s/event", clientExternalEndPoint))
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
	wg = sync.WaitGroup{}
	nodeName = os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Infof("please set env variable, export NODE_NAME=k8s nodename")
		os.Exit(1)
	}
	//1. health check on publisher endpoint-check for availability
	healthCheckPublisher()

	//2. consumer app - spin web server to receive events
	wg.Add(1)
	go StartServer()

	// EVENT subscription and consuming
	initResources()
	// 1.first subscribe to all resources
	if e := common.Subscribe(clientID, subs, nodeName, fmt.Sprintf("%s/subscription", publisherServiceName),
		fmt.Sprintf("%s/event", clientExternalEndPoint)); e != nil {
		log.Errorf("error processing subscription %s", e)
		stopHTTPServerChan <- true
		os.Exit(1)
	}
	// 2. event will be received at /event as event happens
	// Polling for events
	//2.call get current state once for all three resource
	data, _, _ := common.GetCurrentState(clientID, publisherServiceName, subs[0].Resource)
	log.Infof("Get CurrentState returned: %s", data)

	data, _, _ = common.GetCurrentState(clientID, publisherServiceName, subs[1].Resource)
	log.Infof("Get CurrentState returned: %s", data)

	data, _, _ = common.GetCurrentState(clientID, publisherServiceName, subs[2].Resource)
	log.Infof("Get CurrentState returned: %s", data)

	//wait for 5 secs for any events and then delete subscription
	time.Sleep(5 * time.Second)
	common.DeleteSubscription(fmt.Sprintf("%s/subscription", publisherServiceName), clientID)
	stopHTTPServerChan <- true
	wg.Wait()
}

// StartServer ... start event consumer application
func StartServer() {
	defer func() {
		wg.Done()
	}()
	stopHTTPServerChan = make(chan bool)
	r := mux.NewRouter()
	r.HandleFunc("/", getRoot)
	r.HandleFunc("/event", getEvent)

	log.Infof("Server started at %s", clientAddress)

	srv := &http.Server{
		Handler: r,
		Addr:    clientAddress,
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 2 * time.Second,
		ReadTimeout:  2 * time.Second,
	}

	go func() {
		// always returns error. ErrServerClosed on graceful close
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// unexpected error. port in use?
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()

	// wait here till a signal is received
	<-stopHTTPServerChan
	if err := srv.Shutdown(context.TODO()); err != nil {
		panic(err) // failure/timeout shutting down the server gracefully
	}
	fmt.Println("Server closed - Channels")
}

func getRoot(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "I am groot!\n")
}

func getEvent(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got / request\n")
	io.WriteString(w, "This is my website!\n")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("could not read body: %s\n", err)
	}
	defer r.Body.Close()
	fmt.Printf("%s: got event ", body)
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
			log.Infof("port forwarding is not set to access k8s service")
			help()
			log.Fatalln(err)
		}
		log.Fatalln(err)
	}
	defer resp.Body.Close()
}
