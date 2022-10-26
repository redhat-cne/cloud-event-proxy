#!/bin/bash

set -e
DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
source "$DIR/common/common.sh"
source "$DIR/common/cne.sh"
source "$DIR/common/consumer.sh"
source "$DIR/common/router.sh"

KUBECONFIG="${KUBECONFIG:-}"
CNE_IMG="${CNE_IMG:-quay.io/openshift/origin-cloud-event-proxy}"
CONSUMER_IMG="${CONSUMER_IMG:-quay.io/redhat-cne/cloud-event-consumer}"
CONSUMER_TYPE="${CONSUMER_TYPE:-MOCK}"


if [ "$KUBECONFIG" == "" ]; then
	echo "[ERROR]: No KUBECONFIG provided"
	exit 1
fi
action=$1
ttype=$2
if [ "$ttype" == "HTTP" ]; then
  TransportType="HTTP"
else
 TransportType="AMQ"
fi

if [ "$action" == "undeploy" ]; then
  action="delete"
  deploy_amq $action $NamespaceAMQTesting || true
  deploy_consumer $action $NamespaceConsumerTesting "" "" || true
  deploy_producer $action $NamespaceProducerTesting "" || true
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
    if [ "$TransportType" == "AMQ" ]; then
        deploy_consumer $action $NamespaceConsumerTesting $AMQTransportHost "" || true
        deploy_producer $action $NamespaceProducerTesting $AMQTransportHost || true
    elfi
        deploy_consumer $action $NamespaceConsumerTesting $HTTPConsumerTransportHost $HTTPMockTransportHost || true
        deploy_producer $action $NamespaceProducerTesting $HTTPMockTransportHost || true
    fi
elif [ "$action" == "delete-consumer" ]; then
  action="delete"
  deploy_consumer $action $NamespaceConsumerTesting "" "" || true
  create_consumer_namespaces $action || true
else
  action="apply"
  label_node || true
  create_namespaces $action || true
  # currently ginkgo cannot skip amq test, so leave it for now
  deploy_amq $action $NamespaceAMQTesting || true
  if [ "$TransportType" == "AMQ" ]; then
    deploy_consumer $action $NamespaceConsumerTesting $AMQTransportHost "" || true
    deploy_producer $action $NamespaceProducerTesting $AMQTransportHost || true
  else
      deploy_consumer $action $NamespaceConsumerTesting $HTTPConsumerTransportHost $HTTPMockTransportHost || true
      deploy_producer $action $NamespaceProducerTesting $HTTPMockTransportHost || true
  fi
fi
