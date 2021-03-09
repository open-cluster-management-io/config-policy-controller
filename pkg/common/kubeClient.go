// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project


package common

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// KubeClient a k8s client used for k8s native resources
var KubeClient *kubernetes.Interface

// KubeConfig is the given kubeconfig at startup
var KubeConfig *rest.Config

// Initialize to initialize some controller variables
func Initialize(kClient *kubernetes.Interface, cfg *rest.Config) {
	KubeClient = kClient
	KubeConfig = cfg
}
