package common

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"syscall"
	"time"

	cneevent "github.com/redhat-cne/sdk-go/pkg/event"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	log "github.com/sirupsen/logrus"
)

var (
	cancelTimeout = 2 * time.Second
)

// Post ...
func Post(address string, e cloudevents.Event) (int, error) {
	sendCtx, sendCancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer sendCancel()
	p, err := cloudevents.NewHTTP()
	if err != nil {
		log.Errorf("failed to create protocol: %s", err.Error())
		return http.StatusBadRequest, err
	}
	c, err := cloudevents.NewClient(p, cloudevents.WithUUIDs(), cloudevents.WithTimeNow())
	if err != nil {
		log.Errorf("failed to create http client: %s", err.Error())
		return http.StatusBadRequest, err
	}
	log.Infof("posting now %s, to  %s", e.Type(), address)
	e.SetDataContentType(cloudevents.ApplicationJSON)
	ctx := cloudevents.ContextWithTarget(sendCtx, address)
	result := c.Send(ctx, e)
	// With current implementation of cloudevents we cannot get ack on delivered of not
	if cloudevents.IsUndelivered(result) || errors.Is(result, syscall.ECONNREFUSED) {
		log.Errorf("failed to send to address %s with %s", address, result)
		return http.StatusBadRequest, result
	}
	return http.StatusOK, nil
}

// GetEventData ... getter method
func GetEventData(url string) (*cloudevents.Event, *cneevent.Data, error) {
	// using variable url is security hole. Do we need to fix this
	response, errResp := http.Get(url)
	if errResp != nil {
		log.Warnf("return rest service  error  %v", errResp)
		return nil, nil, errResp
	}
	defer response.Body.Close()

	event := &cloudevents.Event{}
	data := &cneevent.Data{}
	var err error
	if err = json.NewDecoder(response.Body).Decode(event); err != nil {
		return nil, nil, err
	}
	if err = json.Unmarshal(event.Data(), &data); err != nil {
		return nil, nil, err
	}
	return event, data, nil
}
