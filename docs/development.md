## Running examples

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
podman push localhost/cloud-event-proxy:edf2bcfd-dirty quay.io/aneeshkp/cloud-event-proxy:latest
podman push localhost/cloud-native-event-consumer:edf2bcfd-dirty quay.io/aneeshkp/cloud-native-event-consumer:latest
podman push localhost/cloud-native-event-producer:edf2bcfd-dirty quay.io/aneeshkp/cloud-native-event-producer:latest
```

Use producer.yaml,consumer.yaml and service.yaml from examples/manifests folder to deploy to a cluster.
Make sure you update the image path.


### Deploying examples using kustomize

Install Kustomize
```shell
curl -s "https://raw.githubusercontent.com/\
kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash
 
mv kustomize /usr/local/bin/

```
### Set Env variables
```shell
export version = latest 
export SIDECAR_IMG = quay.io/aneeshkp/cloud-event-proxy
export  PRODUCER_IMG = quay.io/aneeshkp/cloud-native-event-producer
export  CONSUMER_IMG = quay.io/aneeshkp/cloud-native-event-consumer
```

```shell
make deploy-example
```


### Delete deployment
```shell
make undeploy-example
```

### AMQ Interconnect Operator installation

