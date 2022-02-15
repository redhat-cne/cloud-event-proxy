#!/bin/bash

set -e
NODE_ROLE="${NODE_ROLE:-node-role.kubernetes.io/worker}"
NamespaceProducerTesting="cloud-event-producer-testing"
NamespaceConsumerTesting="cloud-event-consumer-testing"
NamespaceAMQTesting="amq-router-testing"

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
    #openshift.io/cluster-monitoring: "true"
EOF

 cat <<EOF | oc $action -f -
apiVersion: v1
kind: Namespace
metadata:
  name: $NamespaceConsumerTesting
  labels:
    name: $NamespaceConsumerTesting
    #openshift.io/cluster-monitoring: "true"
EOF

cat <<EOF | oc $action -f -
apiVersion: v1
kind: Namespace
metadata:
  name: $NamespaceAMQTesting
  labels:
    name: $NamespaceAMQTesting
    #openshift.io/cluster-monitoring: "true"
EOF
}
