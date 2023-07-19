// Package namespaces ...
package namespaces

import (
	"context"
	"time"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"

	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
)

// WaitForDeletion waits until the namespace will be removed from the cluster
func WaitForDeletion(cs *testclient.Set, nsName string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		_, err := cs.Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
}

// Create creates a new namespace with the given name.
// If the namespace exists, it returns.
func Create(namespace string, cs *testclient.Set) error {
	_, err := cs.Namespaces().Create(context.Background(), &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		}},
		metav1.CreateOptions{},
	)

	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// Clean cleans all dangling objects from the given namespace.
func Clean(namespace string, cs *testclient.Set) error {
	_, err := cs.Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		return nil
	}

	err = cs.NetworkPolicies(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{
		GracePeriodSeconds: pointer.Int64(0),
	}, metav1.ListOptions{})
	if err != nil {
		return err
	}

	_ = cs.Pods(namespace).DeleteCollection(context.Background(), metav1.DeleteOptions{
		GracePeriodSeconds: pointer.Int64(0),
	}, metav1.ListOptions{})

	allServices, err := cs.Services(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, s := range allServices.Items {
		if isPlatformService(namespace, s.Name) {
			continue
		}
		err = cs.Services(namespace).Delete(context.Background(), s.Name, metav1.DeleteOptions{
			GracePeriodSeconds: pointer.Int64(0)})
		if err != nil && errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return err
}

func isPlatformService(namespace, serviceName string) bool {
	switch {
	case namespace != "default":
		return false
	case serviceName == "kubernetes":
		return true
	case serviceName == "openshift":
		return true
	default:
		return false
	}
}
