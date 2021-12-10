package nodes

import (
	"fmt"
	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"context"
	corev1 "k8s.io/api/core/v1"
)

// NodesSelector represent the label selector used to filter impacted nodes.
const (
	EventNodeLabel = "app=local"
)

// NodeTopology  ...
type NodeTopology struct {
	NodeName   string
	NodeObject *corev1.Node
}

// LabelNode ...
func LabelNode(nodeName, key, value string) (*corev1.Node, error) {
	NodeObject, err := testclient.Client.Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	NodeObject.Labels[key] = value
	NodeObject, err = testclient.Client.Nodes().Update(context.Background(), NodeObject, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return NodeObject, nil
}

// FilterNodes ...
func FilterNodes(nodesSelector string, toFilter []NodeTopology) ([]NodeTopology, error) {
	if nodesSelector == "" {
		return toFilter, nil
	}
	toMatch, err := testclient.Client.Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: nodesSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("Error in getting nodes matching the %s label selector, %v", nodesSelector, err)
	}
	if len(toMatch.Items) == 0 {
		return nil, fmt.Errorf("Failed to get nodes matching %s label selector", nodesSelector)
	}

	res := make([]NodeTopology, 0)
	for _, n := range toFilter {
		for _, m := range toMatch.Items {
			if n.NodeName == m.Name {
				res = append(res, n)
				break
			}
		}
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("Failed to find matching nodes with %s label selector", nodesSelector)
	}
	return res, nil
}

// GetNodes ...
func GetNodes() ([]NodeTopology, error) {
	nodes, err := testclient.Client.Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Error in getting nodes  %v", err)
	}
	res := make([]NodeTopology, 0)
	for _, node := range nodes.Items {
		n := NodeTopology{}
		n.NodeName = node.Name
		n.NodeObject = &node
		res = append(res, n)
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("Failed to find nodes")
	}
	return res, nil
}
