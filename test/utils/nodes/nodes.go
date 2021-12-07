package nodes

import (
	"os"

	corev1 "k8s.io/api/core/v1"
)

// NodesSelector represent the label selector used to filter impacted nodes.
var NodesSelector string

func init() {
	NodesSelector = os.Getenv("NODES_SELECTOR")
}

// NodeTopology  ...
type NodeTopology struct {
	NodeName      string
	InterfaceList []string
	NodeObject    *corev1.Node
}
