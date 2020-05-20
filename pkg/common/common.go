// Copyright (c) 2020 Red Hat, Inc.

package common

import (
	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// CreateRecorder return recorder
func CreateRecorder(kubeClient kubernetes.Interface, componentName string) (record.EventRecorder, error) {
	eventsScheme := runtime.NewScheme()
	if err := v1.AddToScheme(eventsScheme); err != nil {
		glog.Errorf("Error add to scheme : %v", err)
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	return eventBroadcaster.NewRecorder(eventsScheme, v1.EventSource{Component: componentName}), nil
}
