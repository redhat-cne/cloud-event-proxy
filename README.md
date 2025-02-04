# cloud-event-proxy
  The cloud-event-proxy project provides a mechanism for events from the K8s infrastructure to be delivered to CNFs with low-latency.  
  The initial event functionality focuses on the operation of the PTP synchronization protocol, but the mechanism can be extended for any infrastructure event that requires low-latency.  
  The mechanism is an integral part of k8s/OCP RAN deployments where the PTP protocol is used to provide timing synchronization for the RAN software elements


 [![go-doc](https://godoc.org/github.com/redhat-cne/cloud-event-proxy?status.svg)](https://godoc.org/github.com/redhat-cne/cloud-event-proxy)
 [![Go Report Card](https://goreportcard.com/badge/github.com/redhat-cne/cloud-event-proxy)](https://goreportcard.com/report/github.com/redhat-cne/cloud-event-proxy)
 [![LICENSE](https://img.shields.io/github/license/redhat-cne/cloud-event-proxy.svg)](https://github.com/redhat-cne/cloud-event-proxy/blob/main/LICENSE)
## Contents
* [Transport Protocol](#event-transporter)
    * [HTTP Protocol](#http-protocol)
* [Publishers](#creating-publisher)
    * [JSON Example](#publisher-json-example)
    * [Go Example](#creating-publisher-golang-example)
* [Subscriptions](#creating-subscriptions)
    * [JSON Example](#subscription-json-example)
    * [GO Example](#creating-subscription-golang-example)
* [Rest API](#rest-api)
* [Cloud Native Events](#cloud-native-events)
  * [Event via sdk](#publisher-event-create-via-go-sdk)
  * [Event via rest api](#publisher-event-create-via-rest-api)
* [Metrics](#metrics)
* [Plugin](#plugin)
  
## Event Transporter
Cloud event proxy currently support one type of transport protocol
1. HTTP Protocol

### HTTP Protocol
#### Producer
CloudEvents HTTP Protocol will be enabled based on url in `transport-host`.
If HTTP is identified then the publisher will start a publisher rest service, which is accessible outside the container via k8s service name.
The Publisher service will have the ability to register consumer endpoints to publish events.

The transport URL is defined in the format of 
```yaml
- "--transport-host=$(TRANSPORT_PROTOCAL)://$(TRANSPORT_SERVICE).$(TRANSPORT_NAMESPACE).svc.cluster.local:$(TRANSPORT_PORT)"
```

HTTP producer example

```yaml
 - name: cloud-event-sidecar
          image: quay.io/redhat-cne/cloud-event-proxy
          args:
            - "--metrics-addr=127.0.0.1:9091"
            - "--store-path=/store"
            - "--transport-host=http://ptp-event-publisher-service-NODE_NAME.openshift-ptp.svc.cluster.local:9043
            - "--api-port=9085"

```
The event producer uses a `pubsubstore` to store Subscriber information, including clientID, consumer service endpoint URI, resource ID etc. These are stored as one json file per registered subscriber. The `pubsubstore` needs to be mounted to a persistent volume in order to survive a pod reboot.

Example for configuring persistent storage

```yaml
     spec:
      nodeSelector:
        node-role.kubernetes.io/worker: ""
      serviceAccountName: hw-event-proxy-sa
      containers:
        - name: cloud-event-sidecar
          volumeMounts:
            - name: pubsubstore
              mountPath: /store
      volumes:
        - name: pubsubstore
          persistentVolumeClaim:
            claimName: cloud-event-proxy-store-storage-class-http-events
```

Example PersistentVolumeClaim
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: cloud-event-proxy-store-storage-class-http-events
spec:
  storageClassName: storage-class-http-events
  resources:
    requests:
      storage: 10Mi
  accessModes:
  - ReadWriteOnce
```

#### Consumer
Consumer application will also set its own `transport-host`, which enabled cloud event proxy to run a service to listen to
incoming events posted by the publisher.
Consumer will also use `http-event-publishers` variable to request for registering  publisher endpoints for consuming events.

HTTP consumer example
```yaml
 - name: cloud-event-sidecar
          image: quay.io/redhat-cne/cloud-event-proxy
          args:
            - "--metrics-addr=127.0.0.1:9091"
            - "--store-path=/store"
            - "--transport-host=consumer-events-subscription-service.cloud-events.svc.cluster.local:9043"
            - "--http-event-publishers=ptp-event-publisher-service-NODE_NAME.openshift-ptp.svc.cluster.local:9043"
            - "--api-port=8089"
```

## Creating Publisher
### Publisher JSON Example 
Create Publisher Resource: JSON request
```json
{
  "Resource": "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state",
  "UriLocation": "http://localhost:9090/ack/event"
}
```

Create Publisher Resource: JSON response
```json
{
  "Id": "789be75d-7ac3-472e-bbbc-6d62878aad4a",
  "Resource": "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state",
  "UriLocation": "http://localhost:9090/ack/event" ,
  "EndpointUri ": "http://localhost:9085/api/ocloudNotifications/v1/publishers/{publisherid}"
}
```

### Creating Publisher Golang Eexample

#### Creating publisher golang example with HTTP as transporter protocol
```go
package main
import (
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
)
func main(){
  //channel for the transport handler subscribed to get and set events  
    eventInCh := make(chan *channel.DataChan, 10)
    pubSubInstance = v1pubsub.GetAPIInstance(".")
    endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:9085", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
    // create publisher 
    pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, "test/test", "1.0"))

}
```

## Creating Subscriptions
### Subscription JSON Example
Create Subscription Resource: JSON request
```json
{
  "Resource": "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state",
  "UriLocation”: “http://localhost:9090/event"
}
```
Example Create Subscription Resource: JSON response
```json
{
  "Id": "789be75d-7ac3-472e-bbbc-6d62878aad4a",
  "Resource": "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state",
  "UriLocation": "http://localhost:9090/ack/event",
  "EndpointUri": "http://localhost:8089/api/ocloudNotifications/v1/subscriptions/{subscriptionid}"
}
```

### Creating Subscription Golang Example

#### Creating subscription golang example with HTTP as transporter protocol
```go
package main
import (
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
)
func main(){
    //channel for the transport handler subscribed to get and set events  
    eventInCh := make(chan *channel.DataChan, 10)
    
    pubSubInstance = v1pubsub.GetAPIInstance(".")
    endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8089", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
    // create subscription 
    pub, err := pubSubInstance.CreateSubscription(v1pubsub.NewPubSub(endpointURL, "test/test", "1.0"))
    
}

```

## Rest-API

### Rest-API to create a Publisher and Subscription
Cloud-Event-Proxy container running with rest api plugin will be running a webservice and exposing following end points.
```html

POST /api/ocloudNotifications/v1/subscriptions
POST /api/ocloudNotifications/v1/publishers
GET /api/ocloudNotifications/v1/subscriptions
GET /api/ocloudNotifications/v1/publishers
GET /api/ocloudNotifications/v1/subscriptions/$subscriptionid
GET /api/ocloudNotifications/v1/publishers/$publisherid
GET /api/ocloudNotifications/v1/health
POST /api/ocloudNotifications/v1/log
POST /api/ocloudNotifications/v1/create/event

```

## Cloud Native Events

The following example shows a Cloud Native Events serialized as JSON:
(Following json should be validated with Cloud native events' event_spec.json schema)


```JSON
{
    "id": "5ce55d17-9234-4fee-a589-d0f10cb32b8e",
    "type": "event.synchronization-state-chang",
    "time": "2021-02-05T17:31:00Z",
    "data": {
    "version": "v1.0",
    "values": [{
        "resource": "/cluster/node/ptp", 
        "dataType": "notification",
        "valueType": "enumeration",
        "value": "ACQUIRING-SYNC"
    }, {

        "resource": "/cluster/node/clock",
        "dataType": "metric",
        "valueType": "decimal64.3",
        "value": 100.3
    }]
    }
}
```
Event can be created via rest-api or calling sdk methods
To produce or consume an event, the producer and consumer should have created publisher and subscription objects
and should have access to the `id` of the publisher/subscription data objects.

```go
import (
   v1event "github.com/redhat-cne/sdk-go/v1/event"
   cneevent "github.com/redhat-cne/sdk-go/pkg/event"
   cneevent "github.com/redhat-cne/sdk-go/pkg/event/ptp"
)

// create an event
event := v1event.CloudNativeEvent()
event.SetID(pub.ID)
event.Type = string(ptp.PtpStateChange)
event.SetTime(types.Timestamp{Time: time.Now().UTC()}.Time)
event.SetDataContentType(cneevent.ApplicationJSON)
data := cneevent.Data{
Version: "v1",
Values: []cneevent.DataValue{
	    {
        Resource:  "/cluster/node/ptp",
        DataType:  cneevent.NOTIFICATION,
        ValueType: cneevent.ENUMERATION,
        Value:     cneevent.GNSS_ACQUIRING_SYNC,
        },
    },
}
data.SetVersion("v1") 
event.SetData(data)

```
### Publisher event create via go-sdk
```go
cloudEvent, _ := v1event.CreateCloudEvents(event, pub)
//send event to transport (rest API does this action by default)
v1event.SendNewEventToDataChannel(eventInCh, pub.Resource, cloudEvent)

```

### Publisher event create via rest-api
```go

//POST /api/ocloudNotifications/v1/create/event
if pub,err:=pubSubInstance.GetPublisher(publisherID);err==nil {
    url = fmt.SPrintf("%s%s", server.HostPath, "create/event")
    restClient.PostEvent(pub.EndPointURI.String(), event)
}

```

## Metrics

### sdk-go metrics
Cloud native events sdk-go comes with following metrics collectors .
1. Number of events received  by the transport
2. Number of events published by the transport.
3. Number of connection resets.
4. Number of sender created
5. Number of receiver created

### rest-api metrics
Cloud native events rest API comes with following metrics collectors .
1. Number of events published by the rest api.
2. Number of active subscriptions.
3. Number of active publishers.

### cloud-event-proxy metrics
1. Number of events produced.
1. Number of events received.

[Metrics details ](docs/metrics.md)
## Plugin
[Plugin details](plugins/README.md)

## Supported PTP configurations
[Supported configurations](docs/configurations.md)

