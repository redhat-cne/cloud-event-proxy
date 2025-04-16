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

package restclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	ce "github.com/cloudevents/sdk-go/v2/event"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/types"
	log "github.com/sirupsen/logrus"

	"golang.org/x/net/context"
)

var (
	httpTimeout = 2 * time.Second
)

// Rest client to make http request
type Rest struct {
	client http.Client
}

// New get new rest client
func New() *Rest {
	return &Rest{
		client: http.Client{
			Timeout: httpTimeout,
		},
	}
}

// PostEvent post an event to the give url and check for error
func (r *Rest) PostEvent(url *types.URI, e event.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		log.Errorf("error marshalling event %v", e)
		return err
	}
	_, err = r.Post(url, b)
	return err
}

// PostCloudEvent post an Cloud Event to the give url and check for error
func (r *Rest) PostCloudEvent(url *types.URI, e ce.Event) (int, error) {
	b, err := json.Marshal(e)
	if err != nil {
		log.Errorf("error marshalling event %v", e)
		return http.StatusBadRequest, err
	}
	return r.Post(url, b)
}

// Post with data
func (r *Rest) Post(url *types.URI, data []byte) (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", url.String(), bytes.NewBuffer(data))
	if err != nil {
		log.Error("error creating post request")
		return http.StatusBadRequest, fmt.Errorf("error creating post request %v", err)
	}
	request.Header.Set("content-type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		log.Error("error in post response")
		return http.StatusBadRequest, err
	}
	if response.Body != nil {
		defer response.Body.Close()
		// read any content and print
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			log.Error("error in reading response body")
			return http.StatusBadRequest, err
		}
		if len(body) > 0 {
			log.Debugf("%s return response %s\n", url.String(), string(body))
		}
	}
	return response.StatusCode, nil
}

// PostWithReturn post with data and return data
func (r *Rest) PostWithReturn(url *types.URI, data []byte) (int, []byte) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", url.String(), bytes.NewBuffer(data))
	if err != nil {
		log.Errorf("error creating post with return: %v", err)
		return http.StatusBadRequest, nil
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in post response to %s: %v", url, err)
		return http.StatusBadRequest, nil
	}
	if res.Body != nil {
		defer res.Body.Close()
	}

	body, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		return http.StatusBadRequest, nil
	}
	return res.StatusCode, body
}

// Put  http request
func (r *Rest) Put(url *types.URI) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "PUT", url.String(), nil)
	if err != nil {
		log.Errorf("error creating put request: %v", err)
		return http.StatusBadRequest
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in put response to %s: %v", url, err)
		return http.StatusBadRequest
	}
	defer res.Body.Close()
	return res.StatusCode
}

// Delete  http request
func (r *Rest) Delete(url *types.URI) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "DELETE", url.String(), nil)
	if err != nil {
		log.Errorf("error creating delete request: %v", err)
		return http.StatusBadRequest
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in delete response to %s: %v", url, err)
		return http.StatusBadRequest
	}
	defer res.Body.Close()
	return res.StatusCode
}

// Get  http request
func (r *Rest) Get(url *types.URI) (int, []byte, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("error creating get request: %v", err)
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("error in get response to %s: %v", url, err)
	}
	defer res.Body.Close()
	var body []byte
	if body, err = io.ReadAll(res.Body); err != nil {
		return res.StatusCode, body, nil
	}
	return http.StatusBadRequest, nil, fmt.Errorf("error reading body in get response to %s: %v", url, err)
}
