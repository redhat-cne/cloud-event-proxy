package utils

import (
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
)

// RecoverFunc ...
func RecoverFunc(rw http.ResponseWriter, callback func()) {
	if err := recover(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "panic: %v\n%s", err, debug.Stack())
		rw.Header().Set(
			"X-Faas-Response-Error-Code", "function_panic",
		)
		rw.Header().Set(
			"X-Faas-Response-Error-Message",
			"Function panic, please check log for more details.",
		)
		rw.WriteHeader(http.StatusInternalServerError)

		if callback != nil {
			callback()
		}
	}
}

// SetInvalidCloudEventHeader ...
func SetInvalidCloudEventHeader(rw http.ResponseWriter, err error) {
	rw.Header().Set(
		"X-redhat-cne-Response-Error-Code", "invalid_cloud_event",
	)
	rw.Header().Set(
		"X-redhat-cne-Response-Error-Message",
		fmt.Sprintf(`The request is not valid cloudevent message, %v.`, err),
	)
	rw.WriteHeader(http.StatusBadRequest)
}
