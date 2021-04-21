package plugins

import (
	"fmt"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/errorhandler"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
)

var (
	pLoader Handler = Handler{Path: "../../plugins"}

	scConfig          *common.SCConfiguration
	channelBufferSize int = 10
)

func init() {
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan bool),
		APIPort:    0,
		APIPath:    "/api/cne/",
		PubSubAPI:  v1pubsub.GetAPIInstance("../.."),
		StorePath:  "../..",
		AMQPHost:   "amqp:localhost:5672",
		BaseURL:    nil,
	}
}

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
			wantErr: fmt.Errorf("error connecting to amqp"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			pLoader = Handler{Path: tc.pgPath}
			_, err := pLoader.LoadAMQPPlugin(wg, scConfig)
			if err != nil {
				switch e := err.(type) {
				case errorhandler.AMQPConnectionError:
					t.Skipf("skipping amqp for this test %s", e.Error())
				default:
					if tc.wantErr != nil && err != nil {
						assert.EqualError(t, tc.wantErr, e.Error())
					}
				}
			}
		})
	}
	close(scConfig.CloseCh)
}

func TestLoadPTPPlugin(t *testing.T) {

	scConfig.CloseCh = make(chan bool)
	wg := &sync.WaitGroup{}
	_, err := common.StartPubSubService(wg, scConfig)
	assert.Nil(t, err)

	testCases := map[string]struct {
		pgPath  string
		wantErr error
	}{
		"Invalid Plugin Path": {
			pgPath:  "wrong",
			wantErr: fmt.Errorf("ptp plugin not found in the path wrong"),
		},
		"Valid Plugin Path": {
			pgPath:  "../../plugins",
			wantErr: nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			pLoader = Handler{Path: tc.pgPath}
			err := pLoader.LoadPTPPlugin(wg, scConfig, nil)
			if tc.wantErr != nil && err != nil {
				assert.EqualError(t, tc.wantErr, err.Error())
			}
		})
	}

}

func Test_End(t *testing.T) {
	close(scConfig.CloseCh)
}
