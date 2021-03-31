package common

import (
	"log"
	"net/http"
	"time"
)

//EndPointHealthChk checks for rest service health
func EndPointHealthChk(url string) {
	log.Printf("health check %s ", url)
	for {
		log.Printf("checking for rest service health")
		response, err := http.Get(url)
		if err != nil {
			log.Printf("retrung health check of the rest service for error  %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			log.Printf("rest service returned healthy status")
			return
		}
		response.Body.Close()

		time.Sleep(2 * time.Second)
	}
}
