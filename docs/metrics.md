---
Title: Metrics
---

cloud event proxy uses  [Prometheus][prometheus] for metrics reporting. The metrics can be used for real-time monitoring and debugging.
cloud event proxy metrics collector does not persist its metrics; if a member restarts, the metrics will be reset.

The simplest way to see the available metrics is to cURL the metrics endpoint `/metrics`. The format is described [here](http://prometheus.io/docs/instrumenting/exposition_formats/).

Follow the [Prometheus getting started doc](http://prometheus.io/docs/introduction/getting_started/) to spin up a Prometheus server to collect metrics.

The naming of metrics follows the suggested [Prometheus best practices](http://prometheus.io/docs/practices/naming/).

A metric name has an `cne`  prefix as its namespace, and a subsystem prefix .

###Registering sdk and rest api collector in your application
The sdk and api collector are registered in cloud-event-proxy application along with its own metrics

```go
// Register metrics
localmetrics.RegisterMetrics()
apimetrics.RegisterMetrics()
sdkmetrics.RegisterMetrics()

```

### Metrics
skd-go and rest-api that are registered by this application can be found here.

All these metrics are prefixed with `cne_`

 
### [SDK-Go Metrics](https://github.com/redhat-cne/sdk-go/blob/main/docs/metrics.md)

| Name                                                  | Description                                              | Type    |
|-------------------------------------------------------|----------------------------------------------------------|---------|
| cne_transport_events_received          | Metric to get number of events received  by the transport.   | Gauge |
| cne_transport_events_published     | Metric to get number of events published by the transport.  | Gauge   |
| cne_transport_connection_reset     | Metric to get number of connection resets.  | Gauge   |
| cne_transport_sender     | Metric to get number of sender created.  | Gauge   |
| cne_transport_receiver     | Metric to get number of receiver created.  | Gauge   |
| cne_transport_status_check_published | Metric to get number of status check published by the transport | Gauge |

### [REST-API Metrics ](https://github.com/redhat-cne/rest-api/blob/main/docs/metrics.md)

| Name                                                  | Description                                              | Type    |
|-------------------------------------------------------|----------------------------------------------------------|---------|
| cne_api_events_published          | Metric to get number of events published by the rest api.   | Gauge |
| cne_api_subscriptions     | Metric to get number of subscriptions.  | Gauge   |
| cne_api_publishers     | Metric to get number of publishers.  | Gauge   |
| cne_api_status_ping | Metric to get number of status pings. | Gauge | 

### [cloud-event-proxy Metrics](#)
These metrics describe the status of the cloud native events.

| Name                                                  | Description                                              | Type    |
|-------------------------------------------------------|----------------------------------------------------------|---------|
| cne_events_ack          | Metric to get number of events produced.   | Gauge |
| cne_events_received     | Metric to get number of events received.  | Gauge   |


`cne_events_ack` -  The number of events that was acknowledged by the producer, grouped by status.

Example
``` 
# HELP cne_events_ack Metric to get number of events produced
# TYPE cne_events_ack gauge
cne_events_ack{status="failed",type="/news-service/finance"} 1
cne_events_ack{status="failed",type="/news-service/sports"} 1
cne_events_ack{status="success",type="/news-service/finance"} 8
cne_events_ack{status="success",type="/news-service/sports"} 8
```

`cne_events_received` -  This metrics indicates number of events that were received, grouped by status.

Example
```
# HELP cne_events_received Metric to get number of events received
# TYPE cne_events_received gauge
cne_events_received{status="success",type="/news-service/finance"} 3
cne_events_received{status="success",type="/news-service/sports"} 3
```

#### Full Metrics
```
# HELP cne_transport_connections_resets Metric to get number of connection resets
# TYPE cne_transport_connections_resets gauge
cne_transport_connection_reset 1
# HELP cne_transport_receiver Metric to get number of receiver created
# TYPE cne_transport_receiver gauge
cne_transport_receiver{address="/news-service/finance",status="active"} 2
cne_transport_receiver{address="/news-service/sports",status="active"} 2
# HELP cne_transport_sender Metric to get number of sender created
# TYPE cne_transport_sender gauge
cne_transport_sender{address="/news-service/finance",status="active"} 1
cne_transport_sender{address="/news-service/sports",status="active"} 1
# HELP cne_events_ack Metric to get number of events produced
# TYPE cne_events_ack gauge
cne_events_ack{status="success",type="/news-service/finance"} 18
cne_events_ack{status="success",type="/news-service/sports"} 18
# HELP cne_events_transport_published Metric to get number of events published by the transport
# TYPE cne_events_transport_published gauge
cne_events_transport_published{address="/news-service/finance",status="failed"} 1
cne_events_transport_published{address="/news-service/finance",status="success"} 18
cne_events_transport_published{address="/news-service/sports",status="failed"} 1
cne_events_transport_published{address="/news-service/sports",status="success"} 18
# HELP cne_events_transport_received Metric to get number of events received  by the transport
# TYPE cne_events_transport_received gauge
cne_events_transport_received{address="/news-service/finance",status="success"} 18
cne_events_transport_received{address="/news-service/sports",status="success"} 18
# HELP cne_events_api_published Metric to get number of events published by the rest api
# TYPE cne_events_api_published gauge
cne_events_api_published{address="/news-service/finance",status="success"} 19
cne_events_api_published{address="/news-service/sports",status="success"} 19
# HELP cne_events_received Metric to get number of events received
# TYPE cne_events_received gauge
cne_events_received{status="success",type="/news-service/finance"} 18
cne_events_received{status="success",type="/news-service/sports"} 18
# HELP promhttp_metric_handler_requests_in_flight Current number of scrapes being served.
# TYPE promhttp_metric_handler_requests_in_flight gauge
promhttp_metric_handler_requests_in_flight 1
# HELP promhttp_metric_handler_requests_total Total number of scrapes by HTTP status code.
# TYPE promhttp_metric_handler_requests_total counter
promhttp_metric_handler_requests_total{code="200"} 4
promhttp_metric_handler_requests_total{code="500"} 0
promhttp_metric_handler_requests_total{code="503"} 0
```