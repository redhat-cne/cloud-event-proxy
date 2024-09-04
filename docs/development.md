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
export IMG=quay.io/openshift/origin-cloud-event-proxy
export  CONSUMER_IMG=quay.io/redhat-cne/cloud-event-consumer
```

### Deploy examples
```shell
make deploy-example
```

### Undeploy examples
```shell
make undeploy-example
```
