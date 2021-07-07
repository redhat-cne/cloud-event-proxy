module github.com/redhat-cne/cloud-event-proxy

go 1.15

replace github.com/redhat-cne/sdk-go v0.0.0-unpublished => ../sdk-go

require (
	github.com/cloudevents/sdk-go/v2 v2.4.1
	github.com/prometheus/client_golang v1.11.0
	github.com/redhat-cne/rest-api v0.1.0
	github.com/redhat-cne/sdk-go v0.0.0-unpublished
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20210716203947-853a461950ff
	google.golang.org/grpc v1.39.0
	google.golang.org/protobuf v1.27.1
)
