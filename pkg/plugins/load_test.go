package plugins

import (
	"fmt"
	"sync"
	"testing"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/stretchr/testify/assert"
)

var (
	pLoader PluginLoader = PluginLoader{Path: "../../plugins"}
)

func TestLoadAMQPPlugin(t *testing.T) {
	wg := &sync.WaitGroup{}
	testCases := map[string]struct {
		pgPath  string
		amqHost string
		wantErr error
	}{
		"Invalid Plugin Path": {
			pgPath:  "wrong",
			amqHost: "",
			wantErr: fmt.Errorf("amqp plugin not found in the path wrong"),
		},
		"Invalid amqp host": {
			pgPath:  "../../plugins",
			amqHost: "",
			wantErr: fmt.Errorf("error conecting to amqp"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			pLoader = PluginLoader{Path: tc.pgPath}
			err := pLoader.LoadAMQPPlugin(wg, "badHost", make(chan *channel.DataChan, 1), make(chan *channel.DataChan, 1), make(chan bool))
			if tc.wantErr != nil {
				assert.EqualError(t, tc.wantErr, err.Error())
			}
		})
	}

}

func TestLoadRestPlugin(t *testing.T) {
	wg := &sync.WaitGroup{}
	testCases := map[string]struct {
		pgPath    string
		port      int
		apiPath   string
		storePath string
		wantErr   error
	}{
		"Invalid Plugin Path": {
			pgPath:    "wrong",
			port:      8080,
			storePath: "../../",
			apiPath:   "/ap/cne",
			wantErr:   fmt.Errorf("rest plugin not found in the path wrong"),
		},
		"valid path": {
			pgPath:    "../../plugins",
			port:      8080,
			storePath: "../../",
			apiPath:   "/ap/cne",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			pLoader = PluginLoader{Path: tc.pgPath}
			_, err := pLoader.LoadRestPlugin(wg, tc.port, tc.apiPath, tc.storePath, make(chan *channel.DataChan, 1), make(chan bool))
			if tc.wantErr != nil {
				assert.EqualError(t, tc.wantErr, err.Error())
			} else {
				assert.Nil(t, err)
			}
		})
	}

}
