# cloud-event-proxy
 Cloud Native Events  manager

 [![go-doc](https://godoc.org/github.com/redhat-cne/cloud-event-proxy?status.svg)](https://godoc.org/github.com/redhat-cne/cloud-event-proxy)
 [![Go Report Card](https://goreportcard.com/badge/github.com/redhat-cne/cloud-event-proxy)](https://goreportcard.com/report/github.com/redhat-cne/cloud-event-proxy)
 [![LICENSE](https://img.shields.io/github/license/redhat-cne/cloud-event-proxy.svg)](https://github.com/redhat-cne/cloud-event-proxy/blob/main/LICENSE)
## Contents
* [Publishers](#creating-publisher)
    * [JSON Example](#publisher-json-example)
    * [Go Example](#creating-publisher-golang-example)
* [Subscriptions](#creating-subscriptions)
    * [JSON Example](#subscription-json-example)
    * [GO Example](#creating-subscription-golang-example)
* [Events](#events)
  * [Event via sdk](#publisher-event-create-via-go-sdk)
  * [Event via rest api](#publisher-event-create-via-rest-api)
* [Metrics](#metrics)    
  
## Creating Publisher
#### Publisher JSON Example 
Create Publisher Resource: JSON request
```json
{
  "Resource": "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state",
  "UriLocation": "http://localhost:8080/ack/event"
}
```

Create Publisher Resource: JSON response
```json
{
  "Id": "789be75d-7ac3-472e-bbbc-6d62878aad4a",
  "Resource": "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state",
  "UriLocation": "http://localhost:9090/ack/event" ,
  "EndpointUri ": "http://localhost:8080/api/ocloudNotifications/v1/publishers/{publisherid}"
}
```
### Creating publisher golang example
```go
package main
import (
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
    v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"github.com/redhat-cne/sdk-go/pkg/types"
)
func main(){
  //channel for the transport handler subscribed to get and set events  
    eventInCh := make(chan *channel.DataChan, 10)
    pubSubInstance = v1pubsub.GetAPIInstance(".")
    endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8080", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
    // create publisher 
    pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, "test/test"))
    // once the publisher response is received, create a transport sender object to send events.
    if err==nil{
        v1amqp.CreateSender(eventInCh, pub.GetResource())
    }
}
```

## Creating Subscriptions
#### Subscription JSON Example
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
  "EndpointUri": "http://localhost:8080/api/ocloudNotifications/v1/subscriptions/{subscriptionid}"
}
```

### Creating subscription golang example  
```go
package main
import (
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
    v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
	"github.com/redhat-cne/sdk-go/pkg/types"
)
func main(){
    //channel for the transport handler subscribed to get and set events  
    eventInCh := make(chan *channel.DataChan, 10)
    
    pubSubInstance = v1pubsub.GetAPIInstance(".")
    endpointURL := &types.URI{URL: url.URL{Scheme: "http", Host: "localhost:8080", Path: fmt.Sprintf("%s%s", apiPath, "dummy")}}
    // create subscription 
    pub, err := pubSubInstance.CreateSubscription(v1pubsub.NewPubSub(endpointURL, "test/test"))
    // once the subscription response is received, create a transport listener object to receive events.
    if err==nil{
        v1amqp.CreateListener(eventInCh, pub.GetResource())
    }
}
```
#### Rest-API to create a Publisher and Subscription.

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
#### Code snippet to create pub/sub
```go

// create publisher
pub, err := pubSubInstance.CreatePublisher(v1pubsub.NewPubSub(endpointURL, "test/test"))

//create subscription
sub, err := pubSubInstance.CreateSubscription(v1pubsub.NewPubSub(endpointURL, "test/test"))


```
#### Create Status Listener
```go
// 1.Create Status Listener Fn (onStatusRequestFn is action to be performed on status ping received)
onStatusRequestFn := func(e v2.Event) error {
log.Printf("got status check call,fire events for above publisher")
event, _ := createPTPEvent(pub)
_ = common.PublishEvent(config, event)
return nil
}
// 2. Create Listener object  
v1amqp.CreateNewStatusListener(config.EventInCh, fmt.Sprintf("%s/%s", pub.Resource, "status"), onStatusRequestFn, nil)

```


### AMQP Objects
#### Create AMQP Sender for Publisher object
```go
package main

import (
  v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
  v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

v1amqp.CreateSender(eventInCh, pub.GetResource())

```

#### Create AMQP listener for subscription object
```go
package main

import (
    v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
    v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

v1amqp.CreateListener(eventInCh, sub.GetResource())
```
### Events
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
        "data_type": "notification",
        "value_type": "enumeration",
        "value": "ACQUIRING-SYNC"
    }, {

        "resource": "/cluster/node/clock",
        "data_type": "metric",
        "value_type": "decimal64.3",
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
)

// create an event
event := v1event.CloudNativeEvent()
event.SetID(pub.ID)
event.Type = "ptp_status_type"
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
//send event to AMQP (rest API does this action by default)
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

##Metrics
###sdk-go metrics
Cloud native events sdk-go comes with following metrics collectors .
1. Number of events received  by the transport
2. Number of events published by the transport.
3. Number of connection resets.
4. Number of sender created
5. Number of receiver created
###rest-api metrics
Cloud native events rest API comes with following metrics collectors .
1. Number of events published by the rest api.
2. Number of active subscriptions.
3. Number of active publishers.
###cloud-event-proxy metrics
1. Number of events produced.
1. Number of events received.

[Metrics details ](docs/metrics.md)


