package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"github.com/redhat-cne/sdk-go/pkg/util/wait"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	httpClient "github.com/cloudevents/sdk-go/v2/client"
	cehttp "github.com/cloudevents/sdk-go/v2/protocol/http"
	httpP "github.com/cloudevents/sdk-go/v2/protocol/http"
	"github.com/google/uuid"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/localmetrics"
	"github.com/redhat-cne/sdk-go/pkg/protocol"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"
	log "github.com/sirupsen/logrus"
)

var (
	cancelTimeout            = 100 * time.Millisecond
	retryTimeout             = 500 * time.Millisecond
	RequestReadHeaderTimeout = 2 * time.Second
)

//Protocol ...
type Protocol struct {
	protocol.Binder
	Protocol *httpP.Protocol
}

// Server ...
type Server struct {
	sync.RWMutex
	Sender        map[string]*Protocol
	Publishers    []*types.URI
	ServiceName   string
	Port          int
	DataIn        <-chan *channel.DataChan
	DataOut       chan<- *channel.DataChan
	Client        httpClient.Client
	cancelTimeout time.Duration
	retryTimeout  time.Duration
	subscriberAPI *subscriberApi.API
	//close on true
	CloseCh                 <-chan struct{}
	ClientID                uuid.UUID
	httpServer              *http.Server
	statusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error
	processEventFn          func(e interface{}) error
}

//InitServer initialize http configurations
func InitServer(serviceName string, port int, storePath string, dataIn <-chan *channel.DataChan,
	dataOut chan<- *channel.DataChan, closeCh <-chan struct{},
	onStatusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error,
	processEventFn func(e interface{}) error) (*Server, error) {
	server := Server{
		Sender:                  map[string]*Protocol{},
		Port:                    port,
		DataIn:                  dataIn,
		ServiceName:             serviceName,
		DataOut:                 dataOut,
		CloseCh:                 closeCh,
		cancelTimeout:           cancelTimeout,
		retryTimeout:            retryTimeout,
		subscriberAPI:           subscriberApi.GetAPIInstance(storePath),
		statusReceiveOverrideFn: onStatusReceiveOverrideFn,
		processEventFn:          processEventFn,
		ClientID:                uuid.New(), //TODO: Persists this UUID to save so when restarts uses same UUID
	}
	return &server, nil
}

// Start ...
func (h *Server) Start(wg *sync.WaitGroup) error {
	ctx := context.Background()
	p, err := cloudevents.NewHTTP()
	if err != nil {
		log.Errorf("failed to create handler: %s", err.Error())
		return err
	}
	subscriptionHandler, err := cloudevents.NewHTTPReceiveHandler(ctx, p, func(e cloudevents.Event) {
		eventType := channel.SUBSCRIBER
		status := channel.NEW

		out := channel.DataChan{
			Address:        e.Source(),
			Data:           &e,
			Status:         status,
			Type:           eventType, // could be new event of new subscriber (sender)
			ProcessEventFn: h.processEventFn,
		}
		var obj subscriber.Subscriber
		if err := json.Unmarshal(e.Data(), &obj); err != nil {
			out.Status = channel.FAILED
			localmetrics.UpdateSenderCreatedCount(out.Address, localmetrics.FAILED, 1)
			log.Errorf("failied to parse subscription %s", err)
		} else {
			out.Address = obj.GetEndPointURI()

			if obj.Action == channel.NEW {
				if _, ok := h.Sender[obj.GetEndPointURI()]; !ok { // we have a sender object
					log.Infof("(1)subscriber not found for the following address %s by %s, will attempt to create", e.Source(), obj.GetEndPointURI())
					if err := h.NewSender(obj.GetEndPointURI()); err != nil {
						log.Errorf("(1)error creating subscriber %v for address %s", err, obj.GetEndPointURI())
						localmetrics.UpdateSenderCreatedCount(obj.GetEndPointURI(), localmetrics.FAILED, 1)
						out.Status = channel.FAILED
					} else {
						_, _ = h.subscriberAPI.CreateSubscription(obj.ClientID, obj)
						localmetrics.UpdateSenderCreatedCount(obj.GetEndPointURI(), localmetrics.ACTIVE, 1)
						out.Status = channel.SUCCESS
					}
				} else {
					log.Infof("(1)subscriber already found for %s, by %s will update again\n", e.Source(), obj.GetEndPointURI())
					out.Status = channel.SUCCESS
					_, _ = h.subscriberAPI.CreateSubscription(obj.ClientID, obj)
				}
			} else {
				if subscriber, ok := h.Sender[obj.GetEndPointURI()]; !ok {
					log.Infof("deleting subscribers")
					_ = h.subscriberAPI.DeleteClient(obj.ClientID)
					h.DeleteSender(obj.GetEndPointURI())
					subscriber.Protocol.Client.CloseIdleConnections()
					subscriber.Client = nil
					localmetrics.UpdateSenderCreatedCount(obj.GetEndPointURI(), localmetrics.ACTIVE, -1)
				}
			}
		}
		h.DataOut <- &out
	})
	if err != nil {
		log.Errorf("failed to create subscription handler: %s", err.Error())
		return err
	}
	statusHandler, err := cloudevents.NewHTTPReceiveHandler(ctx, p, func(e cloudevents.Event) {
		out := channel.DataChan{
			Address: e.Source(),
			Data:    &e,
			Status:  channel.NEW,
			Type:    channel.STATUS, // could be new event of new subscriber (sender)
		}

		if h.statusReceiveOverrideFn != nil {
			out.Status = channel.SUCCESS
			localmetrics.UpdateEventReceivedCount(out.Address, localmetrics.SUCCESS, 1)
			if err := h.statusReceiveOverrideFn(e, &out); err != nil {
				out.Status = channel.FAILED
				localmetrics.UpdateEventReceivedCount(out.Address, localmetrics.FAILED, 1)
			} else {
				localmetrics.UpdateEventReceivedCount(out.Address, localmetrics.SUCCESS, 1)
				out.Status = channel.SUCCESS
			}
		} else {
			out.Status = channel.FAILED
			localmetrics.UpdateEventReceivedCount(out.Address, localmetrics.FAILED, 1)
		}
		h.DataOut <- &out
	})
	if err != nil {
		log.Errorf("failed to create status handler: %s", err.Error())
		return err
	}
	eventHandler, err := cloudevents.NewHTTPReceiveHandler(ctx, p, func(e cloudevents.Event) {
		out := channel.DataChan{
			Address: e.Source(), // cloud event source is bus address
			Data:    &e,
			Status:  channel.NEW,
			Type:    channel.EVENT,
		}
		localmetrics.UpdateEventReceivedCount(out.Address, localmetrics.SUCCESS, 1)
		h.DataOut <- &out
	})
	if err != nil {
		log.Errorf("failed to create event handler: %s", err.Error())
		return err
	}

	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	r.Handle("/event", eventHandler)
	r.Handle("/subscription", subscriptionHandler)
	r.Handle("/status", statusHandler)

	err = r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil {
			log.Println("ROUTE:", pathTemplate)
		}
		pathRegexp, err := route.GetPathRegexp()
		if err == nil {
			log.Println("Path regexp:", pathRegexp)
		}
		queriesTemplates, err := route.GetQueriesTemplates()
		if err == nil {
			log.Println("Queries templates:", strings.Join(queriesTemplates, ","))
		}
		queriesRegexps, err := route.GetQueriesRegexp()
		if err == nil {
			log.Println("Queries regexps:", strings.Join(queriesRegexps, ","))
		}
		methods, err := route.GetMethods()
		if err == nil {
			log.Println("Methods:", strings.Join(methods, ","))
		}
		log.Println()
		return nil
	})

	if err != nil {
		log.Println(err)
	}

	wg.Add(1)
	log.Infof("starting  publisher/subscriber http transporter %d", h.Port)
	go wait.Until(func() {
		h.httpServer = &http.Server{
			ReadHeaderTimeout: RequestReadHeaderTimeout,
			Addr:              fmt.Sprintf(":%d", h.Port),
			Handler:           r,
		}
		err := h.httpServer.ListenAndServe()
		if err != nil {
			log.Errorf("restarting due to error with http messaging server %s\n", err.Error())
		}
	}, 1*time.Second, h.CloseCh)

	return nil
}

// Shutdown ... shutdown rest service api, but it will not close until close chan is called
func (h *Server) Shutdown() {
	log.Warnf("trying to shutdown rest api sever, please use close channel to shutdown ")
	h.httpServer.Close()
}

// SetClientID ...
func (h *Server) SetClientID(clientID uuid.UUID) {
	h.ClientID = clientID
}

// RegisterPublishers this will register publisher
func (h *Server) RegisterPublishers(publisherURL ...*types.URI) {
	var publishers []*types.URI
	for _, p1 := range publisherURL {
		var found = false
		for _, p2 := range h.Publishers {
			if p2.String() == p1.String() {
				log.Infof("publisher %s already exists, skipping registration", p1)
				found = true
				break
			}
		}
		if !found {
			log.Infof("publisher %s does not exists, registering", p1)
			publishers = append(publishers, p1)
		}
	}
	h.Publishers = publishers
}

// UnRegisterPublishers this will un register publisher
func (h *Server) UnRegisterPublishers(publisherURL *types.URI) {
	for i, p := range h.Publishers {
		if p.String() == publisherURL.String() {
			h.Publishers = append(h.Publishers[:i], h.Publishers[i+1:]...)
			return
		}
	}
}

// SetOnStatusReceiveOverrideFn ... sets receiver function
func (h *Server) SetOnStatusReceiveOverrideFn(fn func(e cloudevents.Event, dataChan *channel.DataChan) error) {
	h.statusReceiveOverrideFn = fn
}

// SetProcessEventFn ...
func (h *Server) SetProcessEventFn(fn func(e interface{}) error) {
	h.processEventFn = fn
}

// HTTPProcessor ...
//Server the web Server listens  on data and do either create subscribers and acts as publisher
/*
//create a  status ping
in <- &channel.DataChan{
	Address: addr,
	Type:    channel.STATUS,
	Status:  channel.NEW,
    OnReceiveOverrideFn: func(e cloudevents.Event) error {}
    ProcessOutChDataFn: func (e event.Event) error {}

}

// create a subscriber
in <- &channel.DataChan{
		ID:      subscriptionOneID,
		Address: subscriptionOne.Resource,
		Type:    channel.SUBSCRIBER,
	}

// send data
in <- &channel.DataChan{
	Address: addr,
	Data:    &event,
	Status:  channel.NEW,
	Type:    channel.EVENT,
}
*/
func (h *Server) HTTPProcessor(wg *sync.WaitGroup) {
	time.Sleep(250 * time.Millisecond)
	log.Infof("starting http process for %s", h.ServiceName)

	wg.Add(1)
	go func(h *Server, wg *sync.WaitGroup) {
		defer wg.Done()
		for { //nolint:gosimple    Producer: Sender Object--->Event       Default Listener:Consumer
			select {
			case d := <-h.DataIn:
				if d.Type == channel.PUBLISHER {
					if s := types.ParseURI(d.Address); s != nil {
						if d.Status == channel.NEW {
							h.RegisterPublishers(s)
						} else {
							h.UnRegisterPublishers(s)
						}
					} else {
						log.Errorf("failed to parse address %s for publisher", d.Address)
					}
				} else if d.Type == channel.SUBSCRIBER { // Listener  means subscriber aka sender
					// post it to the address that has been specified : to target URL
					subs := subscriber.New(h.ClientID.String())
					//Self URL
					_ = subs.SetEndPointURI(h.ServiceName)
					obj := pubsub.PubSub{ // all we need is ID and  resource address
						ID:       d.ID,
						Resource: d.Address,
					}
					// create a subscriber model
					subs.AddSubscription(obj)
					subs.Action = d.Status
					ce, _ := subs.CreateCloudEvents()
					ce.SetSubject(d.Status.String())

					if len(h.Publishers) > 0 {
						if err := Post(fmt.Sprintf("%s/subscription", h.Publishers[0]), *ce); err != nil {
							log.Errorf("(1)error creating: %v at  %s with data %s=%s", err, h.Publishers[0], ce.String(), ce.Data())
							log.Errorf("Data sent Address := %s ", d.Address)
							localmetrics.UpdateSenderCreatedCount(d.Address, localmetrics.ACTIVE, -1)
							d.Status = channel.FAILED
							h.DataOut <- d
						} else {
							log.Infof("successfully created subscription for %s", d.Address)
							localmetrics.UpdateSenderCreatedCount(d.Address, localmetrics.ACTIVE, 1)
							d.Status = channel.SUCCESS
							h.DataOut <- d
						}
					} else {
						log.Errorf("no publisher endpoint found to request for subscription %s", d.Address)
						d.Status = channel.FAILED
						h.DataOut <- d
					}
				} else if d.Type == channel.EVENT && d.Status == channel.NEW {
					// post the events to the address specified
					log.Infof("fetch all urls to send events for %s", d.Address)
					urls := h.subscriberAPI.GetSubscriberURLByResource(d.Address)
					if len(urls) == 0 {
						log.Errorf("no subscriber found for resource %s", d.Address)
						d.Status = channel.FAILED
						localmetrics.UpdateEventCreatedCount(d.Address, localmetrics.FAILED, -1)
						h.DataOut <- d
					} else {
						localmetrics.UpdateEventCreatedCount(d.Address, localmetrics.SUCCESS, 1)
						for _, url := range urls {
							log.Infof("Loop and post events %s, who have subscribed to  %s", d.Address, url) // this address is event address
							//TODO write efficient way to multi thread this call
							data := &channel.DataChan{
								ID:                  d.ID,
								Address:             d.Address,
								Data:                d.Data,
								Status:              d.Status,
								Type:                d.Type,
								OnReceiveFn:         d.OnReceiveFn,
								OnReceiveOverrideFn: d.OnReceiveOverrideFn,
								ProcessEventFn:      d.ProcessEventFn,
							}
							if err := Post(fmt.Sprintf("%s/event", url), *data.Data); err != nil {
								log.Errorf("error %s sending event %v at  %s", err, *data.Data, url)
								data.Address = d.Address
								data.Status = channel.FAILED
								h.DataOut <- data
							} else {
								data.Address = d.Address
								data.Status = channel.SUCCESS
								h.DataOut <- data
							}
						}
					}
				} else if d.Type == channel.STATUS {
					// here what you got is request for status for particular address
					// create a subscription object with list of subscription  you are interested to ping
					// d.Address is resource address
					// if its empty then get all address and ID and create subscription object
					//else get only sub you are interested
					//TODO: change to get status for all events
					// current implementation expects to have a resource address
					// post it to the address that has been specified : to target URL
					subs := subscriber.New(h.ClientID.String())
					//Self URL
					_ = subs.SetEndPointURI(h.ServiceName)
					obj := pubsub.PubSub{}
					if d.Address != "" {
						obj.Resource = d.Address
					}
					subs.AddSubscription(obj)
					subs.Action = d.Status
					ce, _ := subs.CreateCloudEvents()
					ce.SetSubject(d.Status.String())

					if err := Post(fmt.Sprintf("%s/status", h.Publishers[0]), *ce); err != nil {
						log.Infof("error sending events status check to %s for %s", h.Publishers[0], d.Address) // this address is event address
						d.Status = channel.FAILED
						h.DataOut <- d
					} else {
						log.Infof("successfully sent status ping  to %s for  %s", h.Publishers[0], d.Address)
						d.Status = channel.SUCCESS
						h.DataOut <- d
					}
				}
			case <-h.CloseCh:
				log.Warn("shutting down subscriber ")
				h.Shutdown()
				//atomic.StoreUint32(&h.state, closed)
				for key, s := range h.Sender {
					h.DeleteSender(key)
					s.Client = nil
				}
				return
			}
		}
	}(h, wg)
}

// SendTo sends events to the address specified
func (h *Server) SendTo(wg *sync.WaitGroup, address string, e *cloudevents.Event, eventType channel.Type) {
	if sender, ok := h.Sender[address]; ok {
		if sender == nil {
			log.Errorf("event failed to send due to connection error,waiting to be reconnected %s", address)
			if eventType == channel.EVENT {
				localmetrics.UpdateEventCreatedCount(address, localmetrics.FAILED, 1)
			} else if eventType == channel.STATUS {
				localmetrics.UpdateStatusCheckCount(address, localmetrics.FAILED, 1)
			}
			h.DataOut <- &channel.DataChan{
				Address: address,
				Data:    e,
				Status:  channel.FAILED,
				Type:    eventType,
			}
			return
		}
		wg.Add(1)
		go func(h *Server, sender *Protocol, eventType channel.Type, address string, e *cloudevents.Event, wg *sync.WaitGroup) {
			defer wg.Done()

			//sendTimes := 3
			//sendCount := 0
			//RetrySend:

			log.Infof("Sending %s now using %s event %v", e.Type(), sender.Address, e)
			ctx := cloudevents.ContextWithTarget(context.Background(), sender.Address)

			c, err := cloudevents.NewClient(sender.Protocol, cloudevents.WithUUIDs(), cloudevents.WithTimeNow())
			if err != nil {
				log.Errorf("failed to create http client: %s", err.Error())
			}
			log.Infof("posting now %s", address)
			if result := c.Send(ctx, *e); cloudevents.IsUndelivered(result) {
				log.Errorf("failed to send(TO): %s result %v ", address, result)
				if result == io.EOF {
					h.SetSender(address, nil)
					log.Errorf("%s failed to send: %v", eventType, result)
					if eventType == channel.EVENT {
						localmetrics.UpdateEventCreatedCount(address, localmetrics.CONNECTION_RESET, 1)
					} else if eventType == channel.STATUS {
						localmetrics.UpdateStatusCheckCount(address, localmetrics.CONNECTION_RESET, 1)
					}
					h.DataOut <- &channel.DataChan{
						Address: address,
						Data:    e,
						Status:  channel.FAILED,
						Type:    eventType,
					}
					log.Errorf("connection lost addressing %s", address)
					//connection lost or connection must have cleaned
				} else {
					if eventType == channel.EVENT {
						localmetrics.UpdateEventCreatedCount(address, localmetrics.FAILED, 1)
					} else if eventType == channel.STATUS {
						localmetrics.UpdateEventCreatedCount(address, localmetrics.FAILED, 1)
					}
					log.Errorf("failed to send to %s", address)
					h.DataOut <- &channel.DataChan{
						Address: address,
						Data:    e,
						Status:  channel.FAILED,
						Type:    eventType,
					}
				}
			} else if cloudevents.IsACK(result) {
				localmetrics.UpdateEventCreatedCount(address, localmetrics.SUCCESS, 1)
				h.DataOut <- &channel.DataChan{
					Address: address,
					Data:    e,
					Status:  channel.SUCCESS,
					Type:    eventType,
				}
				var httpResult *cehttp.Result
				cloudevents.ResultAs(result, &httpResult)
				log.Printf("Sent  with status code %d", httpResult.StatusCode)
			} else {
				h.DataOut <- &channel.DataChan{
					Address: address,
					Data:    e,
					Status:  channel.FAILED,
					Type:    eventType}
				log.Errorf("failed here %v", result)
				var httpResult *cehttp.Result
				cloudevents.ResultAs(result, &httpResult)
				log.Printf("Sent  with status code %d", httpResult.StatusCode)
			}
		}(h, sender, eventType, address, e, wg)
	}
}

// NewClient ...
func (h *Server) NewClient(host string, connOption []httpP.Option) (httpClient.Client, error) {
	//--
	c, err2 := cloudevents.NewClientHTTP(cloudevents.WithTarget(host))
	if err2 != nil {
		log.Errorf("failed to create http client: %s", err2.Error())
		return nil, err2
	}

	return c, nil
}

// SetSender is a wrapper for setting the value of a key in the underlying map
func (h *Server) SetSender(key string, val *Protocol) {
	h.Lock()
	defer h.Unlock()
	h.Sender[key] = val
}

// DeleteSender ... delete listener
func (h *Server) DeleteSender(key string) {
	h.Lock()
	defer h.Unlock()
	delete(h.Sender, key)
}

// NewSender creates new QDR ptp
func (h *Server) NewSender(address string) error {
	l := Protocol{}
	h.SetSender(address, &l)
	//server.NewClient(host, []httpP.Option{})
	p, err := cloudevents.NewHTTP(cloudevents.WithTarget(address))
	if err != nil {
		log.Errorf("failed to create http protocol: %s", err.Error())
		return err
	}

	//--
	c, err2 := cloudevents.NewClient(p, cloudevents.WithUUIDs(), cloudevents.WithTimeNow())
	if err2 != nil {
		log.Errorf("failed to create http client: %s", err2.Error())
		return err2
	}

	log.Infof("created new client for subscriber %s", address)
	l.Address = address
	l.Protocol = p
	l.Client = c
	// this could be changed to use client ID
	h.SetSender(address, &l)
	return nil
}

// GET ... getter method
func GET(url string) (int, error) {
	log.Infof("health check %s ", url)
	// using variable url is security hole. Do we need to fix this
	response, errResp := http.Get(url)
	if errResp != nil {
		log.Warnf("return health check of the rest service for error  %v", errResp)
		return http.StatusBadRequest, errResp
	}
	if response != nil && response.StatusCode == http.StatusOK {
		response.Body.Close()
		log.Info("rest service returned healthy status")
		return http.StatusOK, nil
	}
	return http.StatusInternalServerError, nil
}

// Post ...
func Post(address string, e cloudevents.Event) error {
	//server.NewClient(host, []httpP.Option{})
	ctx := cloudevents.ContextWithTarget(context.Background(), address)
	p, err := cloudevents.NewHTTP()
	if err != nil {
		log.Errorf("failed to create protocol: %s", err.Error())
		return err
	}

	c, err := cloudevents.NewClient(p, cloudevents.WithUUIDs(), cloudevents.WithTimeNow())
	if err != nil {
		log.Errorf("failed to create http client: %s", err.Error())
		return err
	}
	log.Infof("posting now %s, to  %s", e.Type(), address)
	e.SetDataContentType(cloudevents.ApplicationJSON)
	result := c.Send(ctx, e)
	if cloudevents.IsUndelivered(result) {
		log.Errorf("failed to send to address %s with %s", address, result)
		return result
	} else if !cloudevents.IsACK(result) {
		log.Printf("sent: not accepted : %t", cloudevents.IsACK(result))
		return result
	}
	var httpResult *cehttp.Result

	if cloudevents.ResultAs(result, &httpResult) {
		if httpResult.StatusCode == http.StatusOK {
			log.Infof("sent with status code %d::%v", httpResult.StatusCode, result)
			return nil
		}
		log.Printf("Sent with status code %d, result: %v", httpResult.StatusCode, result)
		return fmt.Errorf(httpResult.Format, httpResult.Args...)
	}
	log.Printf("Send did not return an HTTP response: %s", result)
	return fmt.Errorf("send did not return an HTTP response: %s", result)
}
