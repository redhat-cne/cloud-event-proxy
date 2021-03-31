package common

import (
	"log"
	"net/http"
	"time"
)

func EndPointHealthChk(url string) {
	log.Printf("health check %s ", url)
	for {
		log.Printf("checking for rest service health")
		response, err := http.Get(url)
		if err != nil {
			log.Printf("error while checking health of the rest service %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			return
		}
		response.Body.Close()

		time.Sleep(2 * time.Second)
	}
}

