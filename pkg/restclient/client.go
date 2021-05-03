package restclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/redhat-cne/sdk-go/pkg/event"
	"golang.org/x/net/context"
)

var (
	httpTimeout time.Duration = 2 * time.Second
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
		log.Printf("error marshalling event %v", e)
		return err
	}
	if status := r.Post(url, b); status == http.StatusBadRequest {
		return fmt.Errorf("post returned status %d", status)
	}
	return nil
}

// Post post with data
func (r *Rest) Post(url *types.URI, data []byte) int {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", url.String(), bytes.NewBuffer(data))
	if err != nil {
		log.Printf("error creating post request %v", err)
		return http.StatusBadRequest
	}
	request.Header.Set("content-type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		log.Printf("error in post response %v", err)
		return http.StatusBadRequest
	}
	if response.Body != nil {
		defer response.Body.Close()
		// read any content and print
		body, readErr := ioutil.ReadAll(response.Body)
		if readErr == nil && len(body) > 0 {
			log.Printf("return body %s\n", string(body))
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
		log.Printf("error creating post request %v", err)
		return http.StatusBadRequest, nil
	}
	request.Header.Set("content-type", "application/json")
	res, err := r.client.Do(request)
	if err != nil {
		log.Printf("error in post response %v to %s ", err, url)
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
