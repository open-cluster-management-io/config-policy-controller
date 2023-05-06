// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test that an array can be updated when using named objects", Ordered, func() {
	const (
		podName    = "pod-case22"
		policyName = "case22-pod-create"
		policyYAML = "../resources/case22_pod_update_image/policy.yaml"
	)

	It("Verifies an image on a pod container can be updated (RHBZ#2117728)", func() {
		By("Creating the " + podName + " pod")
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: podName,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "nginx",
						// The policy is going to apply 1.7.8
						Image: "nginx:1.7.9",
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 80,
							},
						},
					},
				},
			},
		}
		_, err := clientManaged.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Creating the " + policyName + " policy")
		utils.Kubectl("apply", "-f", policyYAML, "-n", testNamespace)

		By("Verifying that the " + policyName + " policy is compliant")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Verifying the pod's image was updated")
		pod, err = clientManaged.CoreV1().Pods("default").Get(context.TODO(), podName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(pod.Spec.Containers).To(HaveLen(1))
		Expect(pod.Spec.Containers[0].Image).To(Equal("nginx:1.7.8"))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})
		err := clientManaged.CoreV1().Pods("default").Delete(context.TODO(), podName, metav1.DeleteOptions{})
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})
})
