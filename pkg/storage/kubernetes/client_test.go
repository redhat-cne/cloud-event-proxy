package kubernetes_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	client "github.com/redhat-cne/cloud-event-proxy/pkg/storage/kubernetes"
	subscriberAPI "github.com/redhat-cne/sdk-go/v1/subscriber"
	testclient "k8s.io/client-go/kubernetes/fake"
)

const (
	nodeName               = "test_node_name"
	storePath              = "."
	configMapRetryInterval = 3 * time.Second
)

var (
	clients *client.Client
)

func setupClient() {
	clients = &client.Client{}
	clients.SetClientSet(testclient.NewSimpleClientset(Subscriptions()))
}

func Subscriptions() *v1.ConfigMap {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceSystem, Name: nodeName},
		Data: map[string]string{
			"03dc46b6-2493-37fa-82e2-7545714a17d6": `{
				"clientID": "03dc46b6-2493-37fa-82e2-7545714a17d6",
				"subStore": {
					"Store": {
						"32659c8d-5a81-3522-b2e0-256675e341d1": {
							"SubscriptionId": "32659c8d-5a81-3522-b2e0-256675e341d1",
							"EndpointUri": "http://dummyEndpointUri",
							"UriLocation": null,
							"ResourceAddress": "/cluster/node/cnfocto2.ptp.lab.eng.bos.redhat.com/sync/ptp-status/lock-state"
						},
						"5ddba6d8-be9b-3e74-8ba1-191d478024f8": {
							"SubscriptionId": "5ddba6d8-be9b-3e74-8ba1-191d478024f8",
							"EndpointUri": "http://dummyEndpointUri",
							"UriLocation": null,
							"ResourceAddress": "/cluster/node/cnfocto2.ptp.lab.eng.bos.redhat.com/sync/sync-status/os-clock-sync-state"
						},
						"746fba50-7ce8-335f-9d8d-e12a4ab6b30b": {
							"SubscriptionId": "746fba50-7ce8-335f-9d8d-e12a4ab6b30b",
							"EndpointUri": "http://dummyEndpointUri",
							"UriLocation": null,
							"ResourceAddress": "/cluster/node/cnfocto2.ptp.lab.eng.bos.redhat.com/sync/ptp-status/ptp-clock-class-change"
						}
					}
				},
				"EndPointURI": "http://event-consumer-external3:27017",
				"status": 1,
				"action": 0
			}`,
			"04dc46b6-2493-37fa-82e2-7545714a17d6": `{
				"clientID": "03dc46b6-2493-37fa-82e2-7545714a17d6",
				"subStore": {
					"Store": {
						"32659c8d-5a81-3522-b2e0-256675e341d1": {
							"SubscriptionId": "32659c8d-5a81-3522-b2e0-256675e341d1",
							"EndpointUri": "http://dummyEndpointUri",
							"UriLocation": null,
							"ResourceAddress": "/cluster/node/cnfocto2.ptp.lab.eng.bos.redhat.com/sync/ptp-status/lock-state"
						},
						"5ddba6d8-be9b-3e74-8ba1-191d478024f8": {
							"SubscriptionId": "5ddba6d8-be9b-3e74-8ba1-191d478024f8",
							"EndpointUri": "http://dummyEndpointUri",
							"UriLocation": null,
							"ResourceAddress": "/cluster/node/cnfocto2.ptp.lab.eng.bos.redhat.com/sync/sync-status/os-clock-sync-state"
						},
						"746fba50-7ce8-335f-9d8d-e12a4ab6b30b": {
							"SubscriptionId": "746fba50-7ce8-335f-9d8d-e12a4ab6b30b",
							"EndpointUri": "http://dummyEndpointUri",
							"UriLocation": null,
							"ResourceAddress": "/cluster/node/cnfocto2.ptp.lab.eng.bos.redhat.com/sync/ptp-status/ptp-clock-class-change"
						}
					}
				},
				"EndPointURI": "http://event-consumer-external2:27017",
				"status": 1,
				"action": 0
			}`,
		},
	}
	return cm
}

func TestClient_InitConfigMap(t *testing.T) {
	setupClient()
	err := clients.InitConfigMap(".", nodeName, metav1.NamespaceSystem, configMapRetryInterval, 0)
	assert.Nil(t, err)
	cm, e := clients.GetConfigMap(context.Background(), nodeName, metav1.NamespaceSystem)
	assert.Nil(t, e)
	assert.NotEmpty(t, cm)
}

func TestClient_GetConfigMap(t *testing.T) {
	setupClient()
	err := clients.InitConfigMap(".", nodeName, metav1.NamespaceSystem, configMapRetryInterval, 0)
	assert.Nil(t, err)
	cm, e := clients.GetConfigMap(context.Background(), nodeName, metav1.NamespaceSystem)
	assert.Nil(t, e)
	assert.NotEmpty(t, cm)
	assert.Equal(t, 2, len(cm.Data))
}

func Test_LoadingSubscriptionFromFileToCache(t *testing.T) {
	setupClient()
	err := clients.InitConfigMap(".", nodeName, metav1.NamespaceSystem, configMapRetryInterval, 0)
	assert.Nil(t, err)
	cm, e := clients.GetConfigMap(context.Background(), nodeName, metav1.NamespaceSystem)
	assert.Nil(t, e)
	assert.NotEmpty(t, cm)
	assert.Equal(t, 2, len(cm.Data))
	instance := subscriberAPI.GetAPIInstance(storePath)
	assert.NotEmpty(t, instance.SubscriberStore.Store)
}
