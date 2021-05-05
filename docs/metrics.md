---
Title: Metrics
---

SDK-GO populates [Prometheus][prometheus]  collectors for metrics reporting. The metrics can be used for real-time monitoring and debugging. Network attachment definition admission controller does not persist its metrics; if a member restarts, the metrics will be reset.

The simplest way to see the available metrics is to cURL the metrics endpoint `/metrics`. The format is described [here](http://prometheus.io/docs/instrumenting/exposition_formats/).

Follow the [Prometheus getting started doc](http://prometheus.io/docs/introduction/getting_started/) to spin up a Prometheus server to collect Network attachment definition admission controller metrics.

The naming of metrics follows the suggested [Prometheus best practices](http://prometheus.io/docs/practices/naming/).

A metric name has an `cne`  prefix as its namespace, and a subsystem prefix .

###Registering sdk and rest api collector in your application
The sdk and api collector are registered in cloud-event-proxy application along with its own metrics


## cne namespace metrics

The metrics under the `cne` prefix are for monitoring .  If there is any change of these metrics, it will be included in release notes.


### Metrics
skd-go and  rest-api  that are registered by this application can be found here
[SDK-GO Metrics details ](https://github.com/redhat-cne/sdk-go/docs/metrics.md)
[REST-API Metrics details ](https://github.com/redhat-cne/rest-api/docs/metrics.md)

These metrics describe the status of the cloud native events.

All these metrics are prefixed with `cne_`

| Name                                                  | Description                                              | Type    |
|-------------------------------------------------------|----------------------------------------------------------|---------|
| cne_events_ack          | Metric to get number of events produced.   | Gauge |
| cne_events_received     | Metric to get number of events received.  | Gauge   |


`cne_events_ack` -  The number of events that was acknowledged by the producer, grouped by status.

Example
```json 
# HELP cne_events_ack Metric to get number of events produced
# TYPE cne_events_ack gauge
cne_events_ack{status="failed",type="/news-service/finance"} 1
cne_events_ack{status="failed",type="/news-service/sports"} 1
cne_events_ack{status="success",type="/news-service/finance"} 8
cne_events_ack{status="success",type="/news-service/sports"} 8
```

`cne_events_received` -  This metrics indicates number of events that were received, grouped by status.

Example
```json
# HELP cne_events_received Metric to get number of events received
# TYPE cne_events_received gauge
cne_events_received{status="success",type="/news-service/finance"} 3
cne_events_received{status="success",type="/news-service/sports"} 3
```

####Full metrics
```json
# HELP cne_amqp_connections_resets Metric to get number of connection resets
# TYPE cne_amqp_connections_resets gauge
cne_amqp_connection_reset 1
# HELP cne_amqp_receiver Metric to get number of receiver created
# TYPE cne_amqp_receiver gauge
cne_amqp_receiver{address="/news-service/finance",status="active"} 2
cne_amqp_receiver{address="/news-service/sports",status="active"} 2
# HELP cne_amqp_sender Metric to get number of sender created
# TYPE cne_amqp_sender gauge
cne_amqp_sender{address="/news-service/finance",status="active"} 1
cne_amqp_sender{address="/news-service/sports",status="active"} 1
# HELP cne_events_ack Metric to get number of events produced
# TYPE cne_events_ack gauge
cne_events_ack{status="success",type="/news-service/finance"} 18
cne_events_ack{status="success",type="/news-service/sports"} 18
# HELP cne_events_amqp_published Metric to get number of events published by the transport
# TYPE cne_events_amqp_published gauge
cne_events_amqp_published{address="/news-service/finance",status="failed"} 1
cne_events_amqp_published{address="/news-service/finance",status="success"} 18
cne_events_amqp_published{address="/news-service/sports",status="failed"} 1
cne_events_amqp_published{address="/news-service/sports",status="success"} 18
# HELP cne_events_amqp_received Metric to get number of events received  by the transport
# TYPE cne_events_amqp_received gauge
cne_events_amqp_received{address="/news-service/finance",status="success"} 18
cne_events_amqp_received{address="/news-service/sports",status="success"} 18
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