// Licensed Materials - Property of IBM
// (c) Copyright IBM Corporation 2018, 2019. All Rights Reserved.
// Note to U.S. Government Users Restricted Rights:
// Use, duplication or disclosure restricted by GSA ADP Schedule
// Contract with IBM Corp.
package common

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// KubeClient a k8s client used for k8s native resources
var KubeClient *kubernetes.Clientset

// KubeConfig is the given kubeconfig at startup
var KubeConfig *rest.Config

// Initialize to initialize some controller varaibles
func Initialize(kClient *kubernetes.Clientset, cfg *rest.Config) {

	KubeClient = kClient
	KubeConfig = cfg
}
