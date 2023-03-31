/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CloudEventSubscriberSpec defines the desired state of CloudEventSubscriber
type CloudEventSubscriberSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	//map of clients
	Subscriber map[string]subscriber.Subscriber `json:"subscriber,omitempty"`
}

// CloudEventSubscriberStatus defines the observed state of CloudEventSubscriber
type CloudEventSubscriberStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	InactiveSubscribers map[string]subscriber.Subscriber `json:"inactiveSubscriber,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CloudEventSubscriber is the Schema for the cloudeventsubscribers API
type CloudEventSubscriber struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudEventSubscriberSpec   `json:"spec,omitempty"`
	Status CloudEventSubscriberStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudEventSubscriberList contains a list of CloudEventSubscriber
type CloudEventSubscriberList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudEventSubscriber `json:"items"`
}
