# Plugins 

In the cloud event proxy framework container, go plugins are utilized to modify the generic event framework by adding domain specific business logics.

Following flags enable plugins.
1. To load a PTP plugin, specify PTP_PLUGIN=true.
   
2. HTTP is the required transport handler for cloud event proxy, it is autoloaded based transport-host url schema.

# PTP PLUGIN

While using a Cloud event proxy with PTP plugin enabled and deployed as a sidecar via ptp operator,
The PTP plugin modifies the cloud event proxy framework with following characteristics.

1.Create publisher for the following types of events

Resource address | Resource Type | Description 
--- |----------------| --- 
/cluster/node/{node-name}/sync/ptp-status/lock-state |event.sync.ptp-status.ptp-state-change |ptp-state-change notification is signalled from equipment at state change
/cluster/node/{node-name}/sync/sync-status/os-clock-sync-state |event.sync.sync-status.os-clock-sync-state-change |State of node OS clock synchronization is notified at state change
/cluster/node/{node-name}/sync/ptp-status/ptp-clock-class-change |event.sync.ptp-status.ptp-clock-class-change |ptp-clock-class-change notification is generated when the clock-class changes.

The publisher details will be available to access via following url within  cloud-event-proxy-container

```
curl http://localhost:9085/api/ocloudNotifications/v1/publishers
[
 {
  "id": "a8c0b15c-567d-4b77-8b93-585794903745",
  "endpointUri": "http://localhost:9085/api/ocloudNotifications/v1/dummy",
  "uriLocation": "http://localhost:9085/api/ocloudNotifications/v1/publishers/a8c0b15c-567d-4b77-8b93-585794903745",
  "resource": "/cluster/node/NODE_NAME/sync/ptp-status/lock-state"
 },
 {
  "id": "f9ed7264-797c-407e-830e-854f8b48c24f",
  "endpointUri": "http://localhost:9085/api/ocloudNotifications/v1/dummy",
  "uriLocation": "http://localhost:9085/api/ocloudNotifications/v1/publishers/f9ed7264-797c-407e-830e-854f8b48c24f",
  "resource": "/cluster/node/NODE_NAME/sync/sync-status/os-clock-sync-state"
 },
 {
  "id": "c53efc49-2f24-4afd-a610-d0eee96ffd04",
  "endpointUri": "http://localhost:9085/api/ocloudNotifications/v1/dummy",
  "uriLocation": "http://localhost:9085/api/ocloudNotifications/v1/publishers/c53efc49-2f24-4afd-a610-d0eee96ffd04",
  "resource": "/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change"
 }
]


```
2. Listens to linuxptp daemon logs of ptp4l and phc2sys
3. Generates Events for PTP by parsing ptp4l/phc2sys logs
4. Add Prometheus metrics for all  PTP events which are accessible within the cloud-event-proxy using 

 ```
 curl localhost:9091/metrics
# HELP cne_transport_events_published Metric to get number of events published by the transport
# TYPE cne_transport_events_published gauge
cne_transport_events_published{address="/cluster/node/NODE_NAME/sync/ptp-status/lock-state",status="success"} 3
cne_transport_events_published{address="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change",status="success"} 2
# HELP cne_transport_events_received Metric to get number of events received  by the transport
# TYPE cne_transport_events_received gauge
cne_transport_events_received{address="/cluster/node/NODE_NAME/sync/ptp-status/lock-state/status",status="success"} 2053
cne_transport_events_received{address="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change/status",status="success"} 2053
cne_transport_events_received{address="/cluster/node/NODE_NAME/sync/sync-status/os-clock-sync-state/status",status="success"} 2053
# HELP cne_transport_receiver Metric to get number of receiver created
# TYPE cne_transport_receiver gauge
cne_transport_receiver{address="/cluster/node/NODE_NAME/sync/ptp-status/lock-state/status",status="active"} 1
cne_transport_receiver{address="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change/status",status="active"} 1
cne_transport_receiver{address="/cluster/node/NODE_NAME/sync/sync-status/os-clock-sync-state/status",status="active"} 1
# HELP cne_transport_sender Metric to get number of sender created
# TYPE cne_transport_sender gauge
cne_transport_sender{address="/cluster/node/NODE_NAME/sync/ptp-status/lock-state",status="active"} 1
cne_transport_sender{address="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change",status="active"} 1
cne_transport_sender{address="/cluster/node/NODE_NAME/sync/sync-status/os-clock-sync-state",status="active"} 1
# HELP cne_api_events_published Metric to get number of events published by the rest api
# TYPE cne_api_events_published gauge
cne_api_events_published{address="/cluster/node/NODE_NAME/sync/ptp-status/lock-state",status="success"} 3
cne_api_events_published{address="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change",status="success"} 2
# HELP cne_api_publishers Metric to get number of publishers
# TYPE cne_api_publishers gauge
cne_api_publishers{status="active"} 3
# HELP cne_events_ack Metric to get number of events produced
# TYPE cne_events_ack gauge
cne_events_ack{status="success",type="/cluster/node/NODE_NAME/sync/ptp-status/lock-state"} 3
cne_events_ack{status="success",type="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change"} 2
# HELP cne_status_check_received Metric to get number of status check received
# TYPE cne_status_check_received gauge
cne_status_check_received{status="success",type="/cluster/node/NODE_NAME/sync/ptp-status/lock-state/status"} 2053
cne_status_check_received{status="success",type="/cluster/node/NODE_NAME/sync/ptp-status/ptp-clock-class-change/status"} 2053
cne_status_check_received{status="success",type="/cluster/node/NODE_NAME/sync/sync-status/os-clock-sync-state/status"} 2053
# HELP openshift_ptp_clock_class 
# TYPE openshift_ptp_clock_class gauge
openshift_ptp_clock_class{node="NODE_NAME",process="ptp4l"} 135
# HELP openshift_ptp_delay_ns 
# TYPE openshift_ptp_delay_ns gauge
openshift_ptp_delay_ns{from="master",iface="ens2fx",node="NODE_NAME",process="ptp4l"} 520
# HELP openshift_ptp_frequency_adjustment_ns 
# TYPE openshift_ptp_frequency_adjustment_ns gauge
openshift_ptp_frequency_adjustment_ns{from="master",iface="ens2fx",node="NODE_NAME",process="ptp4l"} -15705
# HELP openshift_ptp_interface_role 0 = PASSIVE, 1 = SLAVE, 2 = MASTER, 3 = FAULTY, 4 =  UNKNOWN, 5 = LISTENING
# TYPE openshift_ptp_interface_role gauge
openshift_ptp_interface_role{iface="ens2f0",node="NODE_NAME",process="ptp4l"} 1
# HELP openshift_ptp_max_offset_ns 
# TYPE openshift_ptp_max_offset_ns gauge
openshift_ptp_max_offset_ns{from="master",iface="ens2fx",node="NODE_NAME",process="ptp4l"} 8
# HELP openshift_ptp_offset_ns 
# TYPE openshift_ptp_offset_ns gauge
openshift_ptp_offset_ns{from="master",iface="ens2fx",node="NODE_NAME",process="ptp4l"} 4
# HELP openshift_ptp_process_restart_count 
# TYPE openshift_ptp_process_restart_count counter
openshift_ptp_process_restart_count{config="ptp4l.0.config",node="NODE_NAME",process="phc2sys"} 1
openshift_ptp_process_restart_count{config="ptp4l.0.config",node="NODE_NAME",process="ptp4l"} 1
# HELP openshift_ptp_process_status 0 = DOWN, 1 = UP
# TYPE openshift_ptp_process_status gauge
openshift_ptp_process_status{config="ptp4l.0.config",node="NODE_NAME",process="phc2sys"} 1
openshift_ptp_process_status{config="ptp4l.0.config",node="NODE_NAME",process="ptp4l"} 1
# HELP openshift_ptp_threshold 
# TYPE openshift_ptp_threshold gauge
openshift_ptp_threshold{node="NODE_NAME",profile="test-slave",threshold="HoldOverTimeout"} 5
openshift_ptp_threshold{node="NODE_NAME",profile="test-slave",threshold="MaxOffsetThreshold"} 100
openshift_ptp_threshold{node="NODE_NAME",profile="test-slave",threshold="MinOffsetThreshold"} -100
# HELP promhttp_metric_handler_requests_in_flight Current number of scrapes being served.
# TYPE promhttp_metric_handler_requests_in_flight gauge
promhttp_metric_handler_requests_in_flight 1
# HELP promhttp_metric_handler_requests_total Total number of scrapes by HTTP status code.
# TYPE promhttp_metric_handler_requests_total counter
promhttp_metric_handler_requests_total{code="200"} 2056
promhttp_metric_handler_requests_total{code="500"} 0
promhttp_metric_handler_requests_total{code="503"} 0

 ```

 
