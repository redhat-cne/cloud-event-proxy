package restclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/redhat-cne/sdk-go/pkg/event"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

//Rest client to make http request
type Rest struct {
	client http.Client
}

//New get new rest client
func New() *Rest {
	return &Rest{
		client: http.Client{
			Timeout: time.Duration(2 * time.Second),
		},
	}
}

//PostEvent post an event to the give url and check for error
func (r *Rest) PostEvent(url string, event event.Event) error {
	b, err := json.Marshal(event)
	if err != nil {
		log.Printf("error marshalling event %v", event)
		return err
	}
	if status := r.Post(url, b); status == http.StatusBadRequest {
		return fmt.Errorf("error posting event")
	}
	return nil
}

//Post post with data
func (r *Rest) Post(url string, data []byte) int {

	request, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	request.Header.Set("content-type", "application/json")
	if err != nil {
		log.Printf("error creating post request %v", err)
		return http.StatusBadRequest
	}
	response, err := r.client.Do(request)
	if err != nil {
		log.Printf("error in post response")
		return http.StatusBadRequest
	}
	return response.StatusCode
}

//PostWithReturn post with data and return data
func (r *Rest) PostWithReturn(url string, data []byte) (int, []byte) {
	request, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	request.Header.Set("content-type", "application/json")
	if err != nil {
		log.Printf("error creating post request %v", err)
		return http.StatusBadRequest, nil
	}
	res, err := r.client.Do(request)
	if err != nil {
		log.Printf("error in post response")
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
