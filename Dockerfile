FROM openshift/origin-release:golang-1.15 AS builder
ENV GO111MODULE=on
ENV CGO_ENABLED=1
ENV COMMON_GO_ARGS=-race
ENV GOOS=linux
ENV GOPATH=/go
WORKDIR /go/src/github.com/redhat-cne/cloud-event-proxy
COPY . .
# add credentials on build since it is using private repo
ARG DOCKER_GIT_CREDENTIALS
ARG GIT_USER
ARG SSH_PRIVATE_KEY
RUN mkdir -p ~/.ssh && umask 0077 && echo "${SSH_PRIVATE_KEY}" > ~/.ssh/id_rsa \
	&& git config --global url."git@github.com:".insteadOf https://github.com/ \
	&& ssh-keyscan github.com >> ~/.ssh/known_hosts

RUN echo "StrictHostKeyChecking no " > /root/.ssh/config

RUN git clone git@github.com:"${GIT_USER}"/redhat-cne/sdk-go.git /go/src/github.com/redhat-cne/sdk-go
RUN git clone git@github.com:"${GIT_USER}"/redhat-cne/rest-api.git /go/src/github.com/redhat-cne/rest-api

RUN hack/build-go.sh
RUN rm /root/.ssh/id_rsa

FROM scratch AS bin
COPY --from=builder /go/src/github.com/redhat-cne/cloud-event-proxy/cloud-event-proxy /usr/bin/


CMD ["/usr/bin/cloud-event-proxy"]