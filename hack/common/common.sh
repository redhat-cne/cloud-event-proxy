#!/bin/bash

set -e
NODE_ROLE="${NODE_ROLE:-node-role.kubernetes.io/worker}"
NamespaceProducerTesting="cloud-event-producer-testing"
NamespaceConsumerTesting="cloud-event-consumer-testing"
NamespaceAMQTesting="amq-router-testing"
AMQTransportHost="amqp://amq-router.${NamespaceAMQTesting}.svc.cluster.local"
TransportType="AMQP"
HTTPMockTransportHost="mock-event-publisher-service.${NamespaceProducerTesting}.svc.cluster.local:9043"
HTTPConsumerTransportHost="consumer-events-subscription-service.${NamespaceConsumerTesting}.svc.cluster.local:9043"


label_node() {
  oc label --overwrite node $(oc  get nodes -l node-role.kubernetes.io/worker="" | grep Ready | cut -f1 -d" " | head -1) app=local
}
create_namespaces() {
action=$1
echo "$action namespace "
cat <<EOF | oc $action -f -
apiVersion: v1
kind: Namespace
metadata:
  name: $NamespaceProducerTesting
  labels:
    name: $NamespaceProducerTesting
    security.openshift.io/scc.podSecurityLabelSync: "false"
    pod-security.kubernetes.io/audit: "privileged"
    pod-security.kubernetes.io/enforce: "privileged"
    pod-security.kubernetes.io/warn: "privileged"
    #openshift.io/cluster-monitoring: "true"
EOF
create_amq_namespaces $action
create_consumer_namespaces $action
}
create_amq_namespaces() {
action=$1
echo "$action namespace "
cat <<EOF | oc $action -f -
apiVersion: v1
kind: Namespace
metadata:
  name: $NamespaceAMQTesting
  labels:
    name: $NamespaceAMQTesting
    security.openshift.io/scc.podSecurityLabelSync: "false"
    pod-security.kubernetes.io/audit: "privileged"
    pod-security.kubernetes.io/enforce: "privileged"
    pod-security.kubernetes.io/warn: "privileged"
    #openshift.io/cluster-monitoring: "true"
EOF
}

create_consumer_namespaces(){
action=$1
echo "$action namespace "
cat <<EOF | oc $action -f -
apiVersion: v1
kind: Namespace
metadata:
  name: $NamespaceConsumerTesting
  labels:
    name: $NamespaceConsumerTesting
    security.openshift.io/scc.podSecurityLabelSync: "false"
    pod-security.kubernetes.io/audit: "privileged"
    pod-security.kubernetes.io/enforce: "privileged"
    pod-security.kubernetes.io/warn: "privileged"
    #openshift.io/cluster-monitoring: "true"
EOF
}
