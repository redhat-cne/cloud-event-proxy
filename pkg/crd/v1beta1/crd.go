package v1beta1

import (
	"context"
	"fmt"
	"time"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	CRDName   = "cloudeventsubscribers.cloud-event-proxy.org"
	GroupName = "cloud-event-proxy.org"
	Version   = "v1beta1"
	timeout   = 2 * time.Second
)

func waitCRDAccepted(c apiextensionsclientset.Interface) error {
	err := wait.Poll(1*time.Second, 10*time.Second, func() (bool, error) {
		crd, err := c.ApiextensionsV1beta1().CustomResourceDefinitions().Get(context.Background(), CRDName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensions.Established &&
				condition.Status == apiextensions.ConditionTrue {
				return true, nil
			}
		}

		return false, fmt.Errorf("CRD is not accepted")
	})

	return err
}

func CreateCustomResourceDefinition(ctx context.Context, clientSet apiextensionsclientset.Interface) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	crd := &apiextensions.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: CRDName,
		},
		Spec: apiextensions.CustomResourceDefinitionSpec{
			Group:   GroupName,
			Version: Version,
			Scope:   apiextensions.NamespaceScoped,
			Names: apiextensions.CustomResourceDefinitionNames{
				Plural:   "cloudeventsubscribers",
				Singular: "cloudeventsubscriber",
				Kind:     "CloudEventSubscriber",
				ListKind: "CloudEventSubscriberList",
			},
		},
		Status: apiextensions.CustomResourceDefinitionStatus{},
	}

	_, err := clientSet.ApiextensionsV1beta1().CustomResourceDefinitions().Create(ctxWithTimeout, crd, metav1.CreateOptions{})

	if err != nil {
		return err
	}

	return waitCRDAccepted(clientSet)
}
