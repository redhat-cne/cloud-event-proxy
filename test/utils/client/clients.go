// Copyright 2020 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package client ...
package client

import (
	"os"

	"github.com/golang/glog"

	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	discovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	networkv1client "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Client defines the client set that will be used for testing
var Client *Set

func init() {
	Client = New("")
}

// Set provides the struct to talk with relevant API
type Set struct {
	k8sClient.Client
	corev1client.CoreV1Interface
	networkv1client.NetworkingV1Client
	appsv1client.AppsV1Interface
	discovery.DiscoveryInterface
	Config *rest.Config
}

// New returns a *ClientBuilder with the given kubeconfig.
func New(kubeConfig string) *Set {
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

	clientSet := &Set{}
	clientSet.CoreV1Interface = corev1client.NewForConfigOrDie(config)
	clientSet.AppsV1Interface = appsv1client.NewForConfigOrDie(config)
	clientSet.NetworkingV1Client = *networkv1client.NewForConfigOrDie(config)
	clientSet.DiscoveryInterface = discovery.NewDiscoveryClientForConfigOrDie(config)
	clientSet.Config = config

	myScheme := runtime.NewScheme()
	if err = scheme.AddToScheme(myScheme); err != nil {
		panic(err)
	}

	if err = apiext.AddToScheme(myScheme); err != nil {
		panic(err)
	}

	clientSet.Client, err = k8sClient.New(config, k8sClient.Options{
		Scheme: myScheme,
	})

	if err != nil {
		return nil
	}

	return clientSet
}
