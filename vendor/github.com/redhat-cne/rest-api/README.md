# Rest API
[![go-doc](https://godoc.org/github.com/redhat-cne/rest-api?status.svg)](https://godoc.org/github.com/redhat-cne/rest-api)
[![Go Report Card](https://goreportcard.com/badge/github.com/redhat-cne/rest-api)](https://goreportcard.com/report/github.com/redhat-cne/rest-api)
[![LICENSE](https://img.shields.io/github/license/redhat-cne/rest-api.svg)](https://github.com/redhat-cne/rest-api/blob/main/LICENSE)

Available  routes 
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
# Pub/Sub Rest API
Rest API spec .

## Version: 1.0.0


### /publishers/

#### POST
##### Summary

Creates a new publisher.

##### Description

If publisher creation is success(or if already exists), publisher will be returned with Created (201).

##### Parameters

| Name | Located in | Description | Required | Schema |
| ---- | ---------- | ----------- | -------- | ---- |
| publisher | body | publisher to add to the list of pub | [PubSub](#pubsub) |

##### Responses

| Code | Description | Schema |
| ---- | ----------- | ------ |
| 201 | publisher/subscription data model | [PubSub](#pubsub) |
| 400 | Error Bad Request | object |

### /subscriptions/

#### POST
##### Summary

Creates a new subscription.

##### Description

If subscription creation is success(or if already exists), subscription will be returned with Created (201).

##### Parameters

| Name | Located in | Description | Required | Schema |
| ---- | ---------- | ----------- | -------- | ---- |
| subscription | body | subscription to add to the list of subscriptions | yes | [PubSub](#pubsub) |

##### Responses

| Code | Description | Schema |
| ---- | ----------- | ------ |
| 201 | publisher/subscription data model | [PubSub](#pubsub) |
| 400 | Error Bad Request | object |



### /create/event/

#### POST
##### Summary

Creates a new event.

##### Description

If publisher is present for the event, then event creation is success and be returned with Accepted (202).

##### Parameters

| Name | Located in | Description | Required | Schema |
| ---- | ---------- | ----------- | -------- | ---- |
| event | body | event along with publisher id | Yes | [Event](#event) |

##### Responses

| Code | Description | Schema |
| ---- | ----------- | ------ |
| 202 | Accepted | object |
| 400 | Error Bad Request | object |

### /subscriptions/status/{subscriptionid}

#### PUT
##### Summary

Creates a new status ping request.

##### Description

If a subscription is present for the request, then status request is success and be returned with Accepted (202).

##### Parameters

| Name | Located in | Description | Required | Schema |
| ---- | ---------- | ----------- | -------- | ---- |
| subscriptionid | request  | subscription id | Yes |  |

##### Responses

| Code | Description | Schema |
| ---- | ----------- | ------ |
| 202 | Accepted | object |
| 400 | Error Bad Request | object |

### Models

#### Data

Data ... cloud native events' data Json payload is as follows,
```json

{
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
```

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| values | [ [DataValue](#datavalue) ] |  | Yes |
| version | string |  | Yes |

#### DataType

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| DataType | string |  | Yes |

#### DataValue

DataValue Json payload is as follows,
```json

{
"resource": "/cluster/node/ptp",
"dataType": "notification",
"valueType": "enumeration",
"value": "ACQUIRING-SYNC"
}
```

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| dataType | [DataType](#datatype) |  | yes |
| resource | string |  | yes |
| value | object |  | yes |
| valueType | [ValueType](#valuetype) |  | yes |

#### Event

Event Json  payload is as follows,
```json

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
Event request model

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| data | [Data](#data) |  | yes |
| dataContentType | string | DataContentType - the Data content type +required | yes |
| dataSchema | [URI](#uri) |  | No |
| id | string | ID of the event; must be non-empty and unique within the scope of the producer. +required | yes |
| time | [Timestamp](#timestamp) |  | yes |
| type | string | Type - The type of the occurrence which has happened. +required | yes |

#### PubSub
PubSub Json request payload is as follows,
```json

{
"id": "789be75d-7ac3-472e-bbbc-6d62878aad4a",
"endpointUri": "http://localhost:9090/ack/event",
"uriLocation":  "<http://localhost:8080/api/ocloudNotifications/v1/publishers/{publisherid>}",
"resource":  "/east-edge-10/vdu3/o-ran-sync/sync-group/sync-status/sync-state"
}
```
PubSub request model

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| endpointUri | [URI](#uri) |  | yes |
| resource | string | Resource - The type of the Resource. +required | yes |


#### Timestamp

Timestamp wraps time.Time to normalize the time layout to RFC3339. It is
intended to enforce compliance with the Cloud Native events spec for their
definition of Timestamp. Custom marshal methods are implemented to ensure
the outbound Timestamp is a string in the RFC3339 layout.

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| Timestamp | object | Timestamp wraps time.Time to normalize the time layout to RFC3339. It is intended to enforce compliance with the Cloud Native events spec for their definition of Timestamp. Custom marshal methods are implemented to ensure the outbound Timestamp is a string in the RFC3339 layout. |  |

#### URI

URI is a wrapper to url.URL. It is intended to enforce compliance with
the Cloud Native Events spec for their definition of URI. Custom
marshal methods are implemented to ensure the outbound URI object
is a flat string.


#### ValueType

| Name | Type | Description | Required |
| ---- | ---- | ----------- | -------- |
| ValueType | ENUM | ENUMERATION,DECIMAL | yes |


## Collecting metrics with Prometheus
Cloud native events rest API comes with following metrics collectors .
1. Number of events published by the rest api.
2. Number of active subscriptions.
3. Number of active publishers.

[Metrics details ](docs/metrics.md)