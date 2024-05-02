#!/bin/bash

set -e
DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
KUBECONFIG="${KUBECONFIG:-}"
CNE_IMG="${CNE_IMG:-quay.io/openshift/origin-cloud-event-proxy}"
CONSUMER_IMG="${CONSUMER_IMG:-quay.io/redhat-cne/cloud-event-consumer}"
CONSUMER_TYPE="${CONSUMER_TYPE:-MOCK}"

source "$DIR/common/common.sh"
source "$DIR/common/cne.sh"
source "$DIR/common/consumer.sh"

if [ "$KUBECONFIG" == "" ]; then
	echo "[ERROR]: No KUBECONFIG provided"
	exit 1
fi
action=$1
TransportType="HTTP"


if [ "$action" == "undeploy" ]; then
  action="delete"
  deploy_consumer $action $NamespaceConsumerTesting "" "" || true
  deploy_producer $action $NamespaceProducerTesting "" || true
  create_namespaces $action || true
elif [ "$action" == "deploy-consumer" ]; then
  action="apply"
  create_consumer_namespaces $action || true
  deploy_consumer $action $NamespaceConsumerTesting $HTTPConsumerTransportHost $HTTPMockTransportHost || true
  deploy_producer $action $NamespaceProducerTesting $HTTPMockTransportHost || true
elif [ "$action" == "delete-consumer" ]; then
  action="delete"
  deploy_consumer $action $NamespaceConsumerTesting "" "" || true
  create_consumer_namespaces $action || true
else
  action="apply"
  label_node || true
  create_namespaces $action || true
  deploy_consumer $action $NamespaceConsumerTesting $HTTPConsumerTransportHost $HTTPMockTransportHost || true
  deploy_producer $action $NamespaceProducerTesting $HTTPMockTransportHost || true
fi
