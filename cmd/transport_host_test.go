package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultTransportHost_Default(t *testing.T) {
	origEnv, envWasSet := os.LookupEnv("NAME_SPACE")
	defer func() {
		if envWasSet {
			os.Setenv("NAME_SPACE", origEnv)
		} else {
			os.Unsetenv("NAME_SPACE")
		}
	}()

	os.Unsetenv("NAME_SPACE")
	host := defaultTransportHost()
	assert.Equal(t,
		"http://ptp-event-publisher-service-NODE_NAME.openshift-ptp.svc.cluster.local:9043",
		host, "should use openshift-ptp when NAME_SPACE is not set")
}

func TestDefaultTransportHost_CustomNamespace(t *testing.T) {
	origEnv, envWasSet := os.LookupEnv("NAME_SPACE")
	defer func() {
		if envWasSet {
			os.Setenv("NAME_SPACE", origEnv)
		} else {
			os.Unsetenv("NAME_SPACE")
		}
	}()

	os.Setenv("NAME_SPACE", "custom-ns")
	host := defaultTransportHost()
	assert.Equal(t,
		"http://ptp-event-publisher-service-NODE_NAME.custom-ns.svc.cluster.local:9043",
		host, "should use custom namespace from NAME_SPACE env var")
	assert.NotContains(t, host, "openshift-ptp",
		"should not contain default namespace when overridden")
}

func TestDefaultTransportHost_EmptyEnvKeepsDefault(t *testing.T) {
	origEnv, envWasSet := os.LookupEnv("NAME_SPACE")
	defer func() {
		if envWasSet {
			os.Setenv("NAME_SPACE", origEnv)
		} else {
			os.Unsetenv("NAME_SPACE")
		}
	}()

	os.Setenv("NAME_SPACE", "")
	host := defaultTransportHost()
	assert.Equal(t,
		"http://ptp-event-publisher-service-NODE_NAME.openshift-ptp.svc.cluster.local:9043",
		host, "empty NAME_SPACE should fall back to openshift-ptp")
}
