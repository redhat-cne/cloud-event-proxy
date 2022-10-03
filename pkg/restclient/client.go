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
	"io/ioutil"
	"net/http"
	"time"

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

	if status := r.Post(url, b); status == http.StatusBadRequest {
		return fmt.Errorf("post returned status %d", status)
	}
	return nil
}

// Post with data
func (r *Rest) Post(url *types.URI, data []byte) int {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", url.String(), bytes.NewBuffer(data))
	if err != nil {
		log.Errorf("error creating post request %v", err)
		return http.StatusBadRequest
	}
	request.Header.Set("content-type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in post response %v", err)
		return http.StatusBadRequest
	}
	if response.Body != nil {
		defer response.Body.Close()
		// read any content and print
		body, readErr := ioutil.ReadAll(response.Body)
		if readErr == nil && len(body) > 0 {
			log.Debugf("%s return response %s\n", url.String(), string(body))
		}
	}
	return response.StatusCode
}

// PostWithReturn post with data and return data
func (r *Rest) PostWithReturn(url *types.URI, data []byte) (int, []byte) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", url.String(), bytes.NewBuffer(data))
	if err != nil {
		log.Errorf("error creating post request %v", err)
		return http.StatusBadRequest, nil
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in post response %v to %s ", err, url)
		return http.StatusBadRequest, nil
	}
	if res.Body != nil {
		defer res.Body.Close()
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		return http.StatusBadRequest, nil
	}
	return res.StatusCode, body
}

// Put  http request
func (r *Rest) Put(url *types.URI) int {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "PUT", url.String(), nil)
	if err != nil {
		log.Errorf("error creating post request %v", err)
		return http.StatusBadRequest
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in post response %v to %s ", err, url)
		return http.StatusBadRequest
	}
	defer res.Body.Close()
	return res.StatusCode
}

// Get  http request
func (r *Rest) Get(url *types.URI) (int, string) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", url.String(), nil)
	if err != nil {
		log.Errorf("error creating post request %v", err)
		return http.StatusBadRequest, fmt.Sprintf("error creating post request %v", err)
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Errorf("error in post response %v to %s ", err, url)
		return http.StatusBadRequest, fmt.Sprintf("error in post response %v to %s ", err, url)
	}
	defer res.Body.Close()
	if body, readErr := ioutil.ReadAll(res.Body); readErr == nil {
		return res.StatusCode, string(body)
	}
	return http.StatusBadRequest, fmt.Sprintf("error in post response %v to %s ", err, url)
}
