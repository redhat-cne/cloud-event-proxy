package common

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
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

//TrimSuffix ...
func TrimSuffix(s, suffix string) string {
	if strings.HasSuffix(s, suffix) { //nolint:gosimple
		s = s[:len(s)-len(suffix)]
	}
	return s
}

//TrimPrefix ...
func TrimPrefix(s, prefix string) string {
	if strings.HasPrefix(s, prefix) { //nolint:gosimple
		s = s[len(s)-len(prefix):]
	}
	return s
}

//GetIntEnv ...
func GetIntEnv(key string) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.Atoi(val); err == nil {
			return ret
		}
	}
	return 0
}

//GetBoolEnv ...
func GetBoolEnv(key string) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if ret, err := strconv.ParseBool(val); err == nil {
			return ret
		}
	}
	return false
}
