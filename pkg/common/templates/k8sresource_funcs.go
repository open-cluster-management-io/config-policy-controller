// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	base64 "encoding/base64"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// retrieves value of the key in the given Secret, namespace
func fromSecret(namespace string, secretname string, key string) (string, error) {
	glog.V(2).Infof("fromSecret for namespace: %v, secretname: %v, key:%v", namespace, secretname, key)

	secretsClient := (*kubeClient).CoreV1().Secrets(namespace)
	secret, getErr := secretsClient.Get(context.TODO(), secretname, metav1.GetOptions{})

	if getErr != nil {
		glog.Errorf("Error Getting secret:  %v", getErr)
		return "", getErr
	}
	glog.V(2).Infof("Secret is %v", secret)

	keyVal := secret.Data[key]
	glog.V(2).Infof("Secret Key:%v, Value: %v", key, keyVal)

	// when using corev1 secret api, the data is returned decoded ,
	// re-encododing to be able to use it in the referencing secret
	sEnc := base64.StdEncoding.EncodeToString(keyVal)
	glog.V(2).Infof("encoded secret Key:%v, Value: %v", key, sEnc)

	return sEnc, nil
}

// retrieves value for the key in the given Configmap, namespace
func fromConfigMap(namespace string, cmapname string, key string) (string, error) {
	glog.V(2).Infof("fromConfigMap for namespace: %v, configmap name: %v, key:%v", namespace, cmapname, key)

	configmapsClient := (*kubeClient).CoreV1().ConfigMaps(namespace)
	configmap, getErr := configmapsClient.Get(context.TODO(), cmapname, metav1.GetOptions{})

	if getErr != nil {
		glog.Errorf("Error getting configmap:  %v", getErr)
		return "", getErr
	}
	glog.V(2).Infof("Configmap is %v", configmap)

	keyVal := configmap.Data[key]
	glog.V(2).Infof("Configmap Key:%v, Value: %v", key, keyVal)

	return keyVal, nil
}

//convenience functions to base64 encode string values
//for setting in value in Referencing Secret resources
func base64encode(v string) string {
	return base64.StdEncoding.EncodeToString([]byte(v))
}

func base64decode(v string) string {
	data, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return err.Error()
	}
	return string(data)
}
