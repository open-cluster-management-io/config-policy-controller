// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	coretypes "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	testclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var c client.Client

var depKey = types.NamespacedName{Name: "default"}

const timeout = time.Second * 5

func TestCreateNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, _ := manager.New(cfg, manager.Options{})
	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	//making sure the namespace created is accessible
	name := "my-name"
	instance := createNamespace(name)
	depKey = types.NamespacedName{Name: name}
	err := c.Create(context.TODO(), instance)
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, instance) }, timeout).
		Should(gomega.Succeed())
}
func TestGetSelectedNamespaces(t *testing.T) {
	// testing the actual logic
	allNamespaces := []string{"default", "dev-accounting", "dev-HR", "dev-research", "kube-public", "kube-sys"}
	included := []string{"dev-*", "kube-*", "default"}
	excluded := []string{"dev-research", "kube-sys"}
	expectedResult := []string{"default", "dev-accounting", "dev-HR", "kube-public"}
	actualResutl := GetSelectedNamespaces(included, excluded, allNamespaces)
	if len(expectedResult) != len(actualResutl) {
		t.Errorf("expectedResult = %v, however actualResutl = %v", expectedResult, actualResutl)
		return
	}
	sort.Strings((expectedResult))
	sort.Strings((actualResutl))
	if !reflect.DeepEqual(actualResutl, expectedResult) {
		t.Errorf("expectedResult = %v, however actualResutl = %v", expectedResult, actualResutl)
		return
	}
}

func createNamespace(nsName string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: nsName,
	},
	}
}

func TestGetAllNamespaces(t *testing.T) {
	var typeMeta = metav1.TypeMeta{
		Kind: "namespace",
	}
	var objMeta = metav1.ObjectMeta{
		Name: "default",
	}
	var ns = coretypes.Namespace{
		TypeMeta:   typeMeta,
		ObjectMeta: objMeta,
	}
	var simpleClient kubernetes.Interface = testclient.NewSimpleClientset()
	simpleClient.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
	Initialize(&simpleClient, nil)
	_, err := GetAllNamespaces()
	assert.Nil(t, err)
}
