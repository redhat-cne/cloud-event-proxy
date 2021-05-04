FROM openshift/origin-release:golang-1.15 AS builder
ENV GO111MODULE=on
ENV CGO_ENABLED=1
ENV COMMON_GO_ARGS=-race
ENV GOOS=linux
ENV GOPATH=/go
WORKDIR /go/src/github.com/redhat-cne/cloud-event-proxy
COPY . .

RUN hack/build-example-go.sh

FROM openshift/origin-base AS bin
COPY --from=builder /go/src/github.com/redhat-cne/cloud-event-proxy/cloud-native-event-producer /

LABEL io.k8s.display-name="Cloud Event Proxy Sample Producer " \
      io.k8s.description="This is a component of OpenShift Container Platform and provides a producer sample to produce events." \
      io.openshift.tags="openshift" \
      maintainer="Aneesh Puttur <aputtur@redhat.com>"

ENTRYPOINT ["./cloud-native-event-producer"]