## Running examples locally

### Side car
```shell
make build-plugins
make run
```
### Consumer
```shell
make run-consumer
```
### Producer
```shell
make run-producer
```

## Building images 
```shell
1. hack/build-image.sh
2. hack/build-example-image.sh
3. podman images
```
#### Push images to a repo

```shell
podman push localhost/cloud-event-proxy:75fa2432-dirty quay.io/jacding/cloud-event-proxy:latest
podman push localhost/cloud-native-event-consumer:75fa2432-dirty quay.io/jacding/cloud-native-event-consumer:latest
podman push localhost/cloud-native-event-producer:75fa2432-dirty quay.io/jacding/cloud-native-event-producer:latest
```

Use producer.yaml,consumer.yaml and service.yaml from examples/manifests folder to deploy to a cluster.
Make sure you update the image path.


## Deploying examples using kustomize

### Install Kustomize
```shell
curl -s "https://raw.githubusercontent.com/\
kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash
 
mv kustomize /usr/local/bin/

```
### Set Env variables
```shell
export version=latest 
export SIDECAR_IMG=quay.io/jacding/cloud-event-proxy
export PRODUCER_IMG=quay.io/jacding/cloud-native-event-producer
export CONSUMER_IMG=quay.io/jacding/cloud-native-event-consumer
```

### Setup AMQ Interconnect

Install the `Red Hat Integration - AMQ Interconnect` operator in a new namespace `<AMQP_NAMESPAVCE>` namespace from the OpenShift Web Console.

Open the `Red Hat Integration - AMQ Interconnect` operator, click `Create Interconnect` from the `Red Hat Integration - AMQ Interconnect` tab. Use default values and make sure the name is `amq-interconnect`.

Make sure amq-interconnect pods are running before the next step.
```shell
oc get pods -n `<AMQP_NAMESPAVCE>`
```

In producer.yaml and consumer.yaml, change the `transport-host` args for `cloud-native-event-sidecar` container from
```
- "--transport-host=amqp://amq-interconnect"
```
to
```
- "--transport-host=amqp://amq-interconnect.<AMQP_NAMESPAVCE>.svc.cluster.local"
```

### Deploy examples
```shell
make deploy-example
```

### Undeploy examples
```shell
make undeploy-example
```
