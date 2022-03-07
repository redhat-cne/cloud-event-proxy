#!/bin/bash

set -e
DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
source "$DIR/common/common.sh"
source "$DIR/common/cne.sh"
source "$DIR/common/consumer.sh"
source "$DIR/common/router.sh"

KUBECONFIG="${KUBECONFIG:-}"
IMG="${IMG:-quay.io/openshift/origin-cloud-event-proxy}"
CONSUMER_IMG="${CONSUMER_IMG:-quay.io/redhat-cne/cloud-event-consumer}"
CONSUMER_TYPE="${CONSUMER_TYPE:-MOCK}"


if [ "$KUBECONFIG" == "" ]; then
	echo "[ERROR]: No KUBECONFIG provided"
	exit 1
fi
action=$1

if [ "$action" == "undeploy" ]; then
  action="delete"
 deploy_amq $action $NamespaceAMQTesting || true
 deploy_consumer $action $NamespaceConsumerTesting $NamespaceAMQTesting || true
 deploy_producer $action $NamespaceProducerTesting $NamespaceAMQTesting || true
 create_namespaces $action || true
elif [ "$action" == "deploy-amq" ]; then
  action="apply"
  create_amq_namespaces $action || true
  deploy_amq $action $NamespaceAMQTesting || true
elif [ "$action" == "delete-amq" ]; then
  action="delete"
  deploy_amq $action $NamespaceAMQTesting || true
  create_amq_namespaces $action || true
elif [ "$action" == "deploy-consumer" ]; then
  action="apply"
  create_consumer_namespaces $action || true
  deploy_consumer $action $NamespaceConsumerTesting $NamespaceAMQTesting || true
elif [ "$action" == "delete-consumer" ]; then
  action="delete"
  deploy_consumer $action $NamespaceConsumerTesting $NamespaceAMQTesting || true
  create_consumer_namespaces $action || true
else
  action="apply"
  label_node || true
 create_namespaces $action || true
 deploy_amq $action $NamespaceAMQTesting || true
 deploy_consumer $action $NamespaceConsumerTesting $NamespaceAMQTesting || true
 deploy_producer $action $NamespaceProducerTesting $NamespaceAMQTesting || true
fi
