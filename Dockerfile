#FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.16-openshift-4.9 AS builder
FROM openshift/origin-release:golang-1.15 AS builder
ENV GO111MODULE=off
ENV CGO_ENABLED=1
ENV COMMON_GO_ARGS=-race
ENV GOOS=linux
ENV GOPATH=/go

WORKDIR /go/src/github.com/redhat-cne/cloud-event-proxy
COPY . .

RUN hack/build-go.sh

#FROM registry.ci.openshift.org/ocp/4.9:base AS bin
FROM openshift/origin-base AS bin
#FROM fedora:30 as bin
#RUN yum install -y linuxptp ethtool make hwdata
COPY --from=builder /go/src/github.com/redhat-cne/cloud-event-proxy/build/cloud-event-proxy /
COPY --from=builder /go/src/github.com/redhat-cne/cloud-event-proxy/plugins/*.so /plugins/
#COPY --from=builder /go/src/github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/scripts/pmc.sh /
#RUN chmod +x /pmc.sh

LABEL io.k8s.display-name="Cloud Event Proxy" \
      io.k8s.description="This is a component of OpenShift Container Platform and provides a side car to handle cloud events." \
      io.openshift.tags="openshift" \
      maintainer="Aneesh Puttur <aputtur@redhat.com>"

ENTRYPOINT ["./cloud-event-proxy"]