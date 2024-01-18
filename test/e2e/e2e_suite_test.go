// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var (
	testNamespace               string
	defaultTimeoutSeconds       int
	defaultConsistentlyDuration int
	kubeconfigManaged           string
	clientManaged               kubernetes.Interface
	clientManagedDynamic        dynamic.Interface
	gvrAPIService               schema.GroupVersionResource
	gvrConfigPolicy             schema.GroupVersionResource
	gvrCRD                      schema.GroupVersionResource
	gvrPod                      schema.GroupVersionResource
	gvrRole                     schema.GroupVersionResource
	gvrNS                       schema.GroupVersionResource
	gvrSCC                      schema.GroupVersionResource
	gvrSecret                   schema.GroupVersionResource
	gvrClusterClaim             schema.GroupVersionResource
	gvrConfigMap                schema.GroupVersionResource
	gvrDeployment               schema.GroupVersionResource
	gvrPolicy                   schema.GroupVersionResource
	gvrOperatorPolicy           schema.GroupVersionResource
	gvrSubscription             schema.GroupVersionResource
	gvrOperatorGroup            schema.GroupVersionResource
	defaultImageRegistry        string
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config policy controller e2e Suite")
}

func init() {
	klog.SetOutput(GinkgoWriter)
	klog.InitFlags(nil)
	flag.StringVar(&kubeconfigManaged, "kubeconfig_managed", "../../kubeconfig_managed_e2e",
		"Location of the kubeconfig to use; defaults to current kubeconfig if set to an empty string")
}

var _ = BeforeSuite(func() {
	format.TruncatedDiff = false

	By("Setup Hub client")
	gvrPod = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	gvrNS = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
	gvrConfigMap = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	gvrRole = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}
	gvrConfigPolicy = schema.GroupVersionResource{
		Group:    "policy.open-cluster-management.io",
		Version:  "v1",
		Resource: "configurationpolicies",
	}
	gvrOperatorPolicy = schema.GroupVersionResource{
		Group:    "policy.open-cluster-management.io",
		Version:  "v1beta1",
		Resource: "operatorpolicies",
	}
	gvrSubscription = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: "subscriptions",
	}
	gvrOperatorGroup = schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1",
		Resource: "operatorgroups",
	}
	gvrAPIService = schema.GroupVersionResource{
		Group:    "apiregistration.k8s.io",
		Version:  "v1",
		Resource: "apiservices",
	}
	gvrCRD = schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}
	gvrPolicy = schema.GroupVersionResource{
		Group:    "policy.open-cluster-management.io",
		Version:  "v1",
		Resource: "policies",
	}
	gvrSCC = schema.GroupVersionResource{
		Group:    "security.openshift.io",
		Version:  "v1",
		Resource: "securitycontextconstraints",
	}
	gvrSecret = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}
	gvrClusterClaim = schema.GroupVersionResource{
		Group:    "cluster.open-cluster-management.io",
		Version:  "v1alpha1",
		Resource: "clusterclaims",
	}
	gvrDeployment = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	clientManaged = NewKubeClient("", kubeconfigManaged, "")
	clientManagedDynamic = NewKubeClientDynamic("", kubeconfigManaged, "")
	defaultImageRegistry = "quay.io/open-cluster-management"
	testNamespace = "managed"
	defaultTimeoutSeconds = 60
	defaultConsistentlyDuration = 25
	By("Create watch namespace if needed")
	namespaces := clientManaged.CoreV1().Namespaces()
	if _, err := namespaces.Get(
		context.TODO(), testNamespace, metav1.GetOptions{},
	); err != nil && errors.IsNotFound(err) {
		Expect(namespaces.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}, metav1.CreateOptions{})).NotTo(BeNil())
	}
	Expect(namespaces.Get(context.TODO(), testNamespace, metav1.GetOptions{})).NotTo(BeNil())
})

func NewKubeClient(url, kubeconfig, context string) kubernetes.Interface {
	klog.V(5).Infof("Create kubeclient for url %s using kubeconfig path %s\n", url, kubeconfig)

	config, err := LoadConfig(url, kubeconfig, context)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return clientset
}

func NewKubeClientDynamic(url, kubeconfig, context string) dynamic.Interface {
	klog.V(5).Infof("Create kubeclient dynamic for url %s using kubeconfig path %s\n", url, kubeconfig)

	config, err := LoadConfig(url, kubeconfig, context)
	if err != nil {
		panic(err)
	}

	clientset, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return clientset
}

func LoadConfig(url, kubeconfig, context string) (*rest.Config, error) {
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	klog.V(5).Infof("Kubeconfig path %s\n", kubeconfig)

	// If we have an explicit indication of where the kubernetes config lives, read that.
	if kubeconfig != "" {
		if context == "" {
			return clientcmd.BuildConfigFromFlags(url, kubeconfig)
		}

		return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
			&clientcmd.ConfigOverrides{
				CurrentContext: context,
			}).ClientConfig()
	}

	// If not, try the in-cluster config.
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}

	// If no in-cluster config, try the default location in the user's home directory.
	if usr, err := user.Current(); err == nil {
		klog.V(5).Infof("clientcmd.BuildConfigFromFlags for url %s using %s\n", url,
			filepath.Join(usr.HomeDir, ".kube", "config"))

		if c, err := clientcmd.BuildConfigFromFlags("", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}

	return nil, fmt.Errorf("could not create a valid kubeconfig")
}

func deleteConfigPolicies(policyNames []string) {
	for _, policyName := range policyNames {
		err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).Delete(
			context.TODO(), policyName, metav1.DeleteOptions{},
		)
		if !errors.IsNotFound(err) {
			ExpectWithOffset(1, err).ToNot(HaveOccurred())
		}
	}

	for _, policyName := range policyNames {
		_ = utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, false, defaultTimeoutSeconds,
		)
	}
}

func deletePods(podNames []string, namespaces []string) {
	for _, podName := range podNames {
		for _, ns := range namespaces {
			err := clientManagedDynamic.Resource(gvrPod).Namespace(ns).Delete(
				context.TODO(), podName, metav1.DeleteOptions{},
			)
			if !errors.IsNotFound(err) {
				ExpectWithOffset(1, err).ToNot(HaveOccurred())
			}
		}
	}

	for _, podName := range podNames {
		for _, ns := range namespaces {
			_ = utils.GetWithTimeout(
				clientManagedDynamic, gvrPod, podName, ns, false, defaultTimeoutSeconds,
			)
		}
	}
}
