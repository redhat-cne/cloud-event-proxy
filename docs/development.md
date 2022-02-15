## Running examples locally

### Install and run Apache Qpid Dispach Router
sudo dnf install qpid-dispatch-router
qdrouterd &

### Side car
```shell
make build-plugins
make run
```
### Consumer
```shell
make run-consumer
```

## Building images 

### Build with local dependencies

```shell
1. hack/local-ldd-dep.sh 
2. edit build-example-image.sh and rename consumer.Dockerfile to consumer.Dockerfile.local
3. edit build-image.sh and rename Dockerfile to Dockerfile.local
```

### Build Images

```shell
1. hack/build-go.sh
2. hack/build-example-go.sh 
3. hack/build-image.sh
4. hack/build-example-image.sh
# find out image tags ${TAG}
5. podman images
```

### Push images to a repo

```shell
podman push localhost/cloud-event-proxy:${TAG} quay.io/redhat-cne/cloud-event-proxy:latest
podman push localhost/cloud-event-consumer:${TAG} quay.io/redhat-cne/cloud-event-consumer:latest
```

Use consumer.yaml and service.yaml from examples/manifests folder to deploy to a cluster.
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
export SIDECAR_IMG=quay.io/aneeshkp/cloud-event-proxy
export  CONSUMER_IMG=quay.io/aneeshkp/cloud-event-consumer
```

### Setup AMQ Interconnect

Install the `Red Hat Integration - AMQ Interconnect` operator in a new namespace `<AMQP_NAMESPAVCE>` namespace from the OpenShift Web Console.

Open the `Red Hat Integration - AMQ Interconnect` operator, click `Create Interconnect` from the `Red Hat Integration - AMQ Interconnect` tab. Use default values and make sure the name is `amq-interconnect`.

Make sure amq-interconnect pods are running before the next step.
```shell
oc get pods -n `<AMQP_NAMESPAVCE>`
```

In consumer.yaml, change the `transport-host` args for `cloud-event-sidecar` container from
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
