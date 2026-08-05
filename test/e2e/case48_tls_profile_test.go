// Copyright (c) 2026 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sdktls "open-cluster-management.io/sdk-go/pkg/tls"
)

const (
	controllerNamespace = "open-cluster-management-agent-addon"
	containerName       = "config-policy-controller"
	podLabelSelector    = "name=config-policy-controller"
)

// This test only works when the controller is running as a real Deployment-managed pod in the
// cluster: it verifies that the ocm-tls-profile ConfigMap watcher exits the process on change,
// which relies on the kubelet restarting the container. Running it against the locally-run
// instrumented binary used by `make e2e-test-coverage` would just kill that process and take
// down the rest of the suite, so it is excluded from that target via the "running-in-cluster"
// label (see e2e-test-running-in-cluster in the Makefile).
var _ = Describe("TLS profile ConfigMap", Label("running-in-cluster"), Serial, func() {
	var podName string

	BeforeEach(func(ctx context.Context) {
		pods, err := clientManaged.CoreV1().Pods(controllerNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: podLabelSelector,
		})
		Expect(err).ToNot(HaveOccurred())

		if len(pods.Items) == 0 {
			Skip("config-policy-controller is not running as a pod in the cluster")
		}

		podName = pods.Items[0].Name
	})

	AfterEach(func(ctx context.Context) {
		if podName == "" {
			return
		}

		initialRestarts := getContainerRestartCount(ctx, podName)

		err := clientManaged.CoreV1().ConfigMaps(controllerNamespace).Delete(
			ctx, sdktls.ConfigMapName, metav1.DeleteOptions{},
		)
		if k8serrors.IsNotFound(err) {
			return
		}

		Expect(err).ToNot(HaveOccurred())

		// Deleting the ConfigMap is itself a change the watcher reacts to (the effective TLS
		// config falls back to a TLS 1.2 floor with Go's default cipher suites), so it also
		// restarts the container. Wait for that to settle here, otherwise a subsequent
		// "running-in-cluster" spec could start against a pod that's still restarting from this
		// cleanup.
		By("Waiting for the controller container to restart again after removing the ocm-tls-profile ConfigMap")
		Eventually(func() int {
			return getContainerRestartCount(ctx, podName)
		}, defaultTimeoutSeconds, 1).Should(BeNumerically(">", initialRestarts))

		By("Waiting for the pod to become ready again")
		waitForPodReady(ctx, podName)
	})

	It("restarts the controller container when the ocm-tls-profile ConfigMap changes", func(ctx context.Context) {
		initialRestarts := getContainerRestartCount(ctx, podName)

		By("Creating the ocm-tls-profile ConfigMap")

		_, err := clientManaged.CoreV1().ConfigMaps(controllerNamespace).Create(
			ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sdktls.ConfigMapName,
					Namespace: controllerNamespace,
				},
				Data: map[string]string{
					sdktls.ConfigMapKeyMinVersion: "VersionTLS13",
				},
			}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Verifying the controller container restarts to pick up the new TLS settings")
		Eventually(func() int {
			return getContainerRestartCount(ctx, podName)
		}, defaultTimeoutSeconds, 1).Should(BeNumerically(">", initialRestarts))

		By("Waiting for the pod to become ready again")
		waitForPodReady(ctx, podName)
	})
})

func getContainerRestartCount(ctx context.Context, podName string) int {
	pod, err := clientManaged.CoreV1().Pods(controllerNamespace).Get(
		ctx, podName, metav1.GetOptions{},
	)
	Expect(err).ToNot(HaveOccurred())

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == containerName {
			return int(cs.RestartCount)
		}
	}

	return -1
}

func waitForPodReady(ctx context.Context, podName string) {
	Eventually(func() (bool, error) {
		pod, err := clientManaged.CoreV1().Pods(controllerNamespace).Get(
			ctx, podName, metav1.GetOptions{},
		)
		if err != nil {
			return false, err
		}

		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady {
				return cond.Status == corev1.ConditionTrue, nil
			}
		}

		return false, nil
	}, defaultTimeoutSeconds, 1).Should(BeTrue())
}
