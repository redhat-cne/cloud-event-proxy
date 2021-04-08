FROM openshift/origin-release:golang-1.15 AS builder
ENV GO111MODULE=on
ENV CGO_ENABLED=1
ENV COMMON_GO_ARGS=-race
ENV GOOS=linux
ENV GOPATH=/go
WORKDIR /go/src/github.com/redhat-cne/cloud-event-proxy
COPY . .

RUN git clone https://github.com/redhat-cne/sdk-go.git /go/src/github.com/redhat-cne/sdk-go
RUN git clone https://github.com/redhat-cne/rest-api.git /go/src/github.com/redhat-cne/rest-api

RUN hack/build-go.sh

FROM openshift/origin-base AS bin
COPY --from=builder /go/src/github.com/redhat-cne/cloud-event-proxy/cloud-event-proxy /usr/bin/

LABEL io.k8s.display-name="Cloud Event Proxy" \
      io.k8s.description="This is a component of OpenShift Container Platform and provides a side car to handle cloud events." \
      io.openshift.tags="openshift" \
      maintainer="Aneesh Puttur <aputtur@redhat.com>"

CMD ["/usr/bin/cloud-event-proxy"]