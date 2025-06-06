// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package utils

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"open-cluster-management.io/governance-policy-propagator/test/utils"
)

// ParseYaml read given yaml file and unmarshal it to &unstructured.Unstructured{}
func ParseYaml(file string) *unstructured.Unstructured {
	yamlFile, err := os.ReadFile(file)
	Expect(err).ToNot(HaveOccurred())

	yamlPlc := &unstructured.Unstructured{}
	err = yaml.Unmarshal(yamlFile, yamlPlc)
	Expect(err).ToNot(HaveOccurred())

	return yamlPlc
}

// GetClusterLevelWithTimeout keeps polling to get the object for timeout seconds until wantFound is met
// (true for found, false for not found)
func GetClusterLevelWithTimeout(
	clientHubDynamic dynamic.Interface,
	gvr schema.GroupVersionResource,
	name string,
	wantFound bool,
	timeout int,
) *unstructured.Unstructured {
	GinkgoHelper()

	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	Eventually(func() error {
		var err error
		namespace := clientHubDynamic.Resource(gvr)
		obj, err = namespace.Get(context.TODO(), name, metav1.GetOptions{})
		if wantFound && err != nil {
			return err
		}
		if !wantFound && err == nil {
			return errors.New("expected to return IsNotFound error")
		}
		if !wantFound && err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	}, timeout, 1).ShouldNot(HaveOccurred())

	if wantFound {
		return obj
	}

	return nil
}

// GetWithTimeout keeps polling to get the object for timeout seconds until wantFound is met
// (true for found, false for not found)
func GetWithTimeout(
	clientHubDynamic dynamic.Interface,
	gvr schema.GroupVersionResource,
	name, namespace string,
	wantFound bool,
	timeout int,
) *unstructured.Unstructured {
	GinkgoHelper()

	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	Eventually(func() error {
		var err error
		obj, err = clientHubDynamic.Resource(gvr).Namespace(namespace).
			Get(context.TODO(), name, metav1.GetOptions{})
		if wantFound && err != nil {
			return err
		}
		if !wantFound && err == nil {
			return fmt.Errorf("expected '%s/%s' in namespace '%s' to return IsNotFound error",
				gvr.Resource, name, namespace)
		}
		if !wantFound && err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	}, timeout, 1).ShouldNot(HaveOccurred())

	if wantFound {
		return obj
	}

	return nil
}

func GetMatchingEvents(
	client kubernetes.Interface, namespace, objName, reasonRegex, msgRegex string, timeout int,
) []corev1.Event {
	GinkgoHelper()

	var eventList *corev1.EventList

	Eventually(func() error {
		var err error
		eventList, err = client.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})

		return err
	}, timeout, 1).ShouldNot(HaveOccurred())

	matchingEvents := make([]corev1.Event, 0)
	msgMatcher := regexp.MustCompile(msgRegex)
	reasonMatcher := regexp.MustCompile(reasonRegex)

	for _, event := range eventList.Items {
		if event.InvolvedObject.Name == objName && reasonMatcher.MatchString(event.Reason) &&
			msgMatcher.MatchString(event.Message) {
			matchingEvents = append(matchingEvents, event)
		}
	}

	return matchingEvents
}

// Kubectl executes kubectl commands
func Kubectl(args ...string) {
	if !strings.HasPrefix(args[len(args)-1], "--kubeconfig=") {
		args = append(args, "--kubeconfig="+"../../kubeconfig_managed_e2e")
	}

	cmd := exec.Command("kubectl", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// in case of failure, print command output (including error)
		Fail(fmt.Sprintf("Error running '%s'\n: %s: %v", strings.Join(cmd.Args, " "), output, err), 1)
	}
}

// KubectlJSONPatchToFile patches a manifest using a JSON patch and returns a
// temporary filepath with the resulting YAML. It is the responsibility of the
// caller to call os.Remove(result) when done with the file.
func KubectlJSONPatchToFile(patch string, args ...string) string {
	patchArgs := []string{
		"patch", "--local=true", "--type=json", "--patch=" + patch, "--output=yaml",
	}

	newYAML, err := utils.KubectlWithOutput(
		append(patchArgs, args...)...)
	if err != nil {
		Fail(err.Error(), 1)
	}

	tmpFile, err := os.CreateTemp("", "e2e-patch")
	if err != nil {
		Fail(err.Error(), 1)
	}

	_, err = tmpFile.WriteString(newYAML)
	if err != nil {
		Fail(err.Error(), 1)
	}

	return tmpFile.Name()
}

// KubectlApplyAndLabel takes a string label value and an array of arguments
// following `kubectl apply` and performs `kubectl apply <args>...` followed by
// `kubectl label e2e-test=<label> args...`.
func KubectlApplyAndLabel(label string, args ...string) {
	applyArgs := []string{"apply"}
	Kubectl(append(applyArgs, args...)...)

	labelArgs := []string{"label", "--overwrite", "e2e-test=" + label}
	Kubectl(append(labelArgs, args...)...)
}

// KubectlDelete executes a delete command, ignoring not found errors,
// and skipping the wait if not provided
func KubectlDelete(args ...string) {
	deleteArgs := []string{
		"delete", "--ignore-not-found",
	}

	// Append default for wait if not provided
	hasWait := slices.ContainsFunc(args, func(arg string) bool {
		return strings.HasPrefix(arg, "--wait")
	})
	if !hasWait {
		deleteArgs = append(deleteArgs, "--wait=false")
	}

	Kubectl(append(deleteArgs, args...)...)
}

// EnforceConfigurationPolicy patches the remediationAction to "enforce"
func EnforceConfigurationPolicy(name string, namespace string) {
	Kubectl("patch", "configurationpolicy", name, `--type=json`,
		`-p=[{"op":"replace","path":"/spec/remediationAction","value":"enforce"}]`, "-n", namespace)
}

// EnforceConfigurationPolicy patches the remediationAction to "enforce"
func EnforceOperatorPolicy(name string, namespace string) {
	Kubectl("patch", "operatorpolicy", name, `--type=json`,
		`-p=[{"op":"replace","path":"/spec/remediationAction","value":"enforce"}]`, "-n", namespace)
}

// GetComplianceState parses status field of configurationPolicy to get compliance
func GetComplianceState(managedPlc *unstructured.Unstructured) (result interface{}) {
	if managedPlc.Object["status"] != nil {
		return managedPlc.Object["status"].(map[string]interface{})["compliant"]
	}

	return nil
}

// GetStatusMessage parses status field to get message
func GetStatusMessage(managedPlc *unstructured.Unstructured) (result interface{}) {
	if managedPlc.Object["status"] != nil {
		status, ok := managedPlc.Object["status"].(map[string]interface{})
		if !ok {
			return nil
		}

		complianceDetails, ok := status["compliancyDetails"].([]interface{})
		if !ok || len(complianceDetails) == 0 {
			return nil
		}

		detail := complianceDetails[0]

		return detail.(map[string]interface{})["conditions"].([]interface{})[0].(map[string]interface{})["message"]
	}

	return nil
}

func CheckComplianceStatus(g Gomega, managedPlc *unstructured.Unstructured, expectedCompliance string) {
	GinkgoHelper()

	complianceStr, _ := GetComplianceState(managedPlc).(string)
	msgStr, _ := GetStatusMessage(managedPlc).(string)

	g.Expect(expectedCompliance).Should(Equal(complianceStr),
		"Unexpected compliance state. Status message: "+msgStr)
}

// GetFieldFromSecret parses data field of secrets for the specified field
func GetFieldFromSecret(secret *unstructured.Unstructured, field string) (result interface{}) {
	if secret.Object["data"] != nil {
		return secret.Object["data"].(map[string]interface{})[field]
	}

	return nil
}

// GetLastEvaluated parses the configuration policy and returns the status.lastEvaluated and
// status.lastEvaluatedGeneration fields. If they are not set, then default null values are returned.
func GetLastEvaluated(configPolicy *unstructured.Unstructured) (string, int64) {
	status, ok := configPolicy.Object["status"].(map[string]interface{})
	if !ok {
		return "", 0
	}

	lastEvaluatedGeneration, ok := status["lastEvaluatedGeneration"].(int64)
	if !ok {
		return "", 0
	}

	lastEvaluated, ok := status["lastEvaluated"].(string)
	if !ok {
		return "", lastEvaluatedGeneration
	}

	return lastEvaluated, lastEvaluatedGeneration
}

// GetMetrics execs into the config-policy-controller pod and curls the metrics endpoint, filters
// the response with the given patterns, and returns the value(s) for the matching
// metric(s).
func GetMetrics(metricPatterns ...string) []string {
	podCmd := exec.Command("kubectl", "get", "pod", "-n=open-cluster-management-agent-addon",
		"-l=name=config-policy-controller", "--no-headers", "--kubeconfig=../../kubeconfig_managed_e2e")

	propPodInfo, err := podCmd.CombinedOutput()
	if err != nil {
		return []string{
			fmt.Sprintf("error running '%s'\n: %s: %s", strings.Join(podCmd.Args, " "), propPodInfo, err.Error()),
		}
	}

	var cmd *exec.Cmd

	metricFilter := " | grep " + strings.Join(metricPatterns, " | grep ")
	metricsCmd := `curl localhost:8383/metrics` + metricFilter

	// The pod name is "No" when the response is "No resources found"
	propPodName := strings.Split(string(propPodInfo), " ")[0]
	if propPodName == "No" || propPodName == "" {
		// A missing pod could mean the controller is running locally
		cmd = exec.Command("bash", "-c", metricsCmd)
	} else {
		cmd = exec.Command("kubectl", "exec", "-n=open-cluster-management-agent-addon", propPodName, "-c",
			"config-policy-controller", "--kubeconfig=../../kubeconfig_managed_e2e", "--", "bash", "-c", metricsCmd)
	}

	matchingMetricsRaw, err := cmd.Output()
	if err != nil {
		if err.Error() == "exit status 1" {
			return []string{} // exit 1 indicates that grep couldn't find a match.
		}

		return []string{
			fmt.Sprintf("error running '%s'\n: %s: %s", strings.Join(cmd.Args, " "), matchingMetricsRaw, err.Error()),
		}
	}

	matchingMetrics := strings.Split(strings.TrimSpace(string(matchingMetricsRaw)), "\n")
	values := make([]string, len(matchingMetrics))

	for i, metric := range matchingMetrics {
		fields := strings.Fields(metric)
		if len(fields) > 0 {
			values[i] = fields[len(fields)-1]
		}
	}

	return values
}

func getConfigPolicyStatusMessages(clientHubDynamic dynamic.Interface,
	gvrConfigPolicy schema.GroupVersionResource, namespace string, policyName string, templateIdx int,
) (string, bool, error) {
	empty := ""
	policyInterface := clientHubDynamic.Resource(gvrConfigPolicy).Namespace(namespace)

	configPolicy, err := policyInterface.Get(context.TODO(), policyName, metav1.GetOptions{})
	if err != nil {
		return empty, false, errors.New("error in getting policy")
	}

	details, found, err := unstructured.NestedSlice(configPolicy.Object, "status", "compliancyDetails")
	if !found || err != nil || len(details) <= templateIdx {
		return empty, false, errors.New("error in getting status")
	}

	templateDetails, ok := details[templateIdx].(map[string]interface{})
	if !ok {
		return empty, false, errors.New("error in getting detail")
	}

	conditions, ok, _ := unstructured.NestedSlice(templateDetails, "conditions")

	if !ok {
		return empty, false, errors.New("error conditions not found")
	}

	condition := conditions[0].(map[string]interface{})

	message, ok, err := unstructured.NestedString(condition, "message")

	return message, ok, err
}

func DoConfigPolicyMessageTest(clientHubDynamic dynamic.Interface,
	gvrConfigPolicy schema.GroupVersionResource, namespace string, policyName string,
	templateIdx int, timeout int, expectedMsg string,
) {
	GinkgoHelper()

	Eventually(func(g Gomega) string {
		message, _, err := getConfigPolicyStatusMessages(clientHubDynamic,
			gvrConfigPolicy, namespace, policyName, templateIdx)
		g.Expect(err).ShouldNot(HaveOccurred())

		return message
	}, timeout, 1).Should(Equal(expectedMsg))
}

func GetServerVersion(clientManaged kubernetes.Interface) string {
	serverVersion, err := clientManaged.Discovery().ServerVersion()
	Expect(err).ToNot(HaveOccurred())

	return serverVersion.String()
}
