package common

import (
	"context"
	"errors"
	"io"
	"net/http"
	"syscall"
	"time"

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

// GetByte ... getter method
func GetByte(url string) ([]byte, int, error) {
	log.Infof("health check %s ", url)
	// using variable url is security hole. Do we need to fix this
	response, errResp := http.Get(url)
	if errResp != nil {
		log.Warnf("return rest service  error  %v", errResp)
		return []byte(errResp.Error()), http.StatusBadRequest, errResp
	}
	defer response.Body.Close()
	var bodyBytes []byte
	var err error
	bodyBytes, err = io.ReadAll(response.Body)
	if err != nil {
		return []byte(err.Error()), http.StatusBadRequest, err
	}
	return bodyBytes, response.StatusCode, nil
}
