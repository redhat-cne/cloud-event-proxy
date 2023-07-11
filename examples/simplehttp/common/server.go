package common

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	cehttp "github.com/cloudevents/sdk-go/v2/protocol/http"
	"github.com/gorilla/mux"
	"github.com/redhat-cne/cloud-event-proxy/examples/simplehttp/utils"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
)

// StartServer ... start event consumer application
func StartServer(wg *sync.WaitGroup, clientAddress string, stopHTTPServerChan chan bool) {
	//logger := log.New(os.Stdout, "", log.LstdFlags)
	defer func() {
		wg.Done()
	}()

	r := mux.NewRouter()
	r.HandleFunc("/", getRoot)
	r.HandleFunc("/event", getEvent).Methods("POST")
	log.Printf("Server started at %s", clientAddress)
	//logMiddleware := common.NewLogMiddleware(logger)
	//r.Use(logMiddleware.Func())
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

func getRoot(w http.ResponseWriter, _ *http.Request) {
	io.WriteString(w, "I am groot!\n")
}

func getEvent(w http.ResponseWriter, r *http.Request) {
	defer utils.RecoverFunc(w, nil)
	message := cehttp.NewMessageFromHttpRequest(r)
	event, err := binding.ToEvent(r.Context(), message)
	if err != nil {
		utils.SetInvalidCloudEventHeader(w, err)
		return
	}
	data := cneevent.Data{}
	if err = json.Unmarshal(event.Data(), &data); err == nil {
		PrintEvent(&data, event.Type(), event.Time())
		w.Header().Set("Content-Type", cloudevents.ApplicationJSON)
		w.WriteHeader(http.StatusOK)
	} else {
		utils.SetInvalidCloudEventHeader(w, err)
	}
}
