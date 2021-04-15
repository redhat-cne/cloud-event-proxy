package common

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	healthCheckPause time.Duration = 2 * time.Second
)

// EndPointHealthChk checks for rest service health
func EndPointHealthChk(url string) (err error) {
	log.Printf("health check %s ", url)
	for i := 0; i <= 5; i++ {
		log.Printf("checking for rest service health")
		response, errResp := http.Get(url)
		if errResp != nil {
			log.Printf("try %d, return health check of the rest service for error  %v", i, errResp)
			time.Sleep(healthCheckPause)
			err = errResp
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			log.Printf("rest service returned healthy status")
			time.Sleep(1 * time.Second)
			err = nil
			return
		}
		response.Body.Close()
		time.Sleep(healthCheckPause)
		err = fmt.Errorf("error connecting %v ", err)
	}
	return
}

// GetIntEnv get int value from env
func GetIntEnv(key string) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.Atoi(val); err == nil {
			return ret
		}
	}
	return 0
}

// GetBoolEnv get bool value from env
func GetBoolEnv(key string) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.ParseBool(val); err == nil {
			return ret
		}
	}
	return false
}
