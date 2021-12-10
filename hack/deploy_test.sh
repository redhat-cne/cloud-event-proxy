#!/bin/bash

set -e
DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
source "$DIR/common/common.sh"
source "$DIR/common/cne.sh"
source "$DIR/common/consumer.sh"
source "$DIR/common/router.sh"

KUBECONFIG="${KUBECONFIG:-}"
CNE_IMG="${CNE_IMG:-quay.io/aneeshkp/cloud-event-proxy}"
CONSUMER_IMG="${CONSUMER_IMG:-quay.io/aneeshkp/cloud-native-event-consumer}"


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
else
  action="apply"
  label_node || true
 create_namespaces $action || true
 deploy_amq $action $NamespaceAMQTesting || true
 deploy_consumer $action $NamespaceConsumerTesting $NamespaceAMQTesting || true
 deploy_producer $action $NamespaceProducerTesting $NamespaceAMQTesting || true

fi





