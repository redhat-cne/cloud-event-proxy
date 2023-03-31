package kubernetes

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	cloudv1beta1 "github.com/redhat-cne/cloud-event-proxy/pkg/api/cloud.event.proxy.org/v1beta1"
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	"os"

	"k8s.io/client-go/kubernetes"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	cneclient "github.com/redhat-cne/cloud-event-proxy/pkg/client/clientset/versioned"
)

// Client has info on how to connect to the kubernetes cluster
type Client struct {
	client    cneclient.Interface
	clientSet kubernetes.Interface
	retries   int
	timeout   time.Duration
}

func NewClientConfig(kubeConfig string) *rest.Config {
	var config *rest.Config
	var err error

	if kubeConfig == "" {
		kubeConfig = os.Getenv("KUBECONFIG")
	}

	if kubeConfig != "" {
		glog.V(4).Infof("Loading kube client config from path %q", kubeConfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
	} else {
		glog.V(4).Infof("Using in-cluster kube client config")
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		glog.Infof("Failed to create a valid client")
		return nil
	}
	return config

}
func NewClient(timeout time.Duration) (*Client, *rest.Config, error) {
	var config *rest.Config
	var kubeConfig string
	var err error
	kubeConfig = os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		glog.V(4).Infof("Loading kube client config from path %q", kubeConfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, nil, err
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	}

	client, err := newClient(config, timeout)
	return client, config, err
}

func NewClientViaKubeconfig(kubeconfigPath string, timeout time.Duration) (*Client, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{}).ClientConfig()

	if err != nil {
		return nil, err
	}

	return newClient(config, timeout)
}

func newClient(config *rest.Config, timeout time.Duration) (*Client, error) {
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	c, err := cneclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return NewKubernetesClient(c, clientSet, timeout), nil
}

func NewKubernetesClient(k8sClient cneclient.Interface, k8sClientSet kubernetes.Interface, timeout time.Duration) *Client {
	return &Client{
		client:    k8sClient,
		clientSet: k8sClientSet,
		retries:   1,
		timeout:   timeout,
	}
}

func (self *Client) GetSubscriberForNode(ctx context.Context, namespace, nodeName string) (cloudv1beta1.CloudEventSubscriber, error) {

	ctxWithTimeout, cancel := context.WithTimeout(ctx, self.timeout)
	defer cancel()

	subscriberList, err := self.client.CloudV1beta1().CloudEventSubscribers(namespace).List(ctxWithTimeout, metav1.ListOptions{})
	if err != nil {
		return cloudv1beta1.CloudEventSubscriber{}, err
	}

	for _, sub := range subscriberList.Items {
		if sub.Name == nodeName {
			return sub, nil
		}
	}
	return cloudv1beta1.CloudEventSubscriber{}, fmt.Errorf("subscriber for %s not found", nodeName)
}

func (self *Client) DeleteSubscriber(ctx context.Context, namespace, nodeName, clientID string,
	subscriber subscriber.Subscriber) (*cloudv1beta1.CloudEventSubscriber, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if eventSubscriber, err := self.GetSubscriberForNode(ctxWithTimeout, namespace, nodeName); err == nil {
		if _, ok := eventSubscriber.Spec.Subscriber[clientID]; ok {
			delete(eventSubscriber.Spec.Subscriber, clientID)
			return self.client.CloudV1beta1().CloudEventSubscribers(namespace).Update(ctxWithTimeout, &eventSubscriber, metav1.UpdateOptions{})
		}
	}
	return nil, nil
}
func (self *Client) UpdateSubscriber(ctx context.Context, namespace, nodeName, clientID string,
	subscriber subscriber.Subscriber) (*cloudv1beta1.CloudEventSubscriber, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if eventSubscriber, err := self.GetSubscriberForNode(ctxWithTimeout, namespace, nodeName); err == nil {
		eventSubscriber.Spec.Subscriber[clientID] = subscriber
		return self.client.CloudV1beta1().CloudEventSubscribers(namespace).Update(ctxWithTimeout, &eventSubscriber, metav1.UpdateOptions{})
	}
	return nil, nil
}
