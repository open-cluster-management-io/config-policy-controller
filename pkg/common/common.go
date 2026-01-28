// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	UninstallingAnnotation string = "policy.open-cluster-management.io/uninstalling"
)

// CreateRecorder return recorder
func CreateRecorder(kubeClient kubernetes.Interface, componentName string) (record.EventRecorder, error) {
	eventsScheme := runtime.NewScheme()
	if err := v1.AddToScheme(eventsScheme); err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	return eventBroadcaster.NewRecorder(eventsScheme, v1.EventSource{Component: componentName}), nil
}

func LogConstructor(controllerName string, kind string, req *reconcile.Request) logr.Logger {
	log := ctrl.Log.WithName(controllerName)

	if req != nil {
		log = log.WithValues("kind", kind, "namespace", req.Namespace, "name", req.Name)
	}

	return log
}
