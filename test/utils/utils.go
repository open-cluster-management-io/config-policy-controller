// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Pause sleep for given seconds
func Pause(s uint) {
	if s < 1 {
		s = 1
	}

	time.Sleep(time.Duration(float64(s)) * time.Second)
}

// ParseYaml read given yaml file and unmarshal it to &unstructured.Unstructured{}
func ParseYaml(file string) *unstructured.Unstructured {
	yamlFile, err := os.ReadFile(file)
	Expect(err).To(BeNil())

	yamlPlc := &unstructured.Unstructured{}
	err = yaml.Unmarshal(yamlFile, yamlPlc)
	Expect(err).To(BeNil())

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
			return fmt.Errorf("expected to return IsNotFound error")
		}
		if !wantFound && err != nil && !errors.IsNotFound(err) {
			return err
		}

		return nil
	}, timeout, 1).Should(BeNil())

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
	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	Eventually(func() error {
		var err error
		namespace := clientHubDynamic.Resource(gvr).Namespace(namespace)
		obj, err = namespace.Get(context.TODO(), name, metav1.GetOptions{})
		if wantFound && err != nil {
			return err
		}
		if !wantFound && err == nil {
			return fmt.Errorf("expected to return IsNotFound error")
		}
		if !wantFound && err != nil && !errors.IsNotFound(err) {
			return err
		}

		return nil
	}, timeout, 1).Should(BeNil())

	if wantFound {
		return obj
	}

	return nil
}

// ListWithTimeout keeps polling to get the object for timeout seconds until wantFound is met
// (true for found, false for not found)
func ListWithTimeout(
	clientHubDynamic dynamic.Interface,
	gvr schema.GroupVersionResource,
	opts metav1.ListOptions,
	size int,
	wantFound bool,
	timeout int,
) *unstructured.UnstructuredList {
	if timeout < 1 {
		timeout = 1
	}
	var list *unstructured.UnstructuredList

	Eventually(func() error {
		var err error
		list, err = clientHubDynamic.Resource(gvr).List(context.TODO(), opts)
		if err != nil {
			return err
		} else if len(list.Items) != size {
			return fmt.Errorf("list size doesn't match, expected %d actual %d", size, len(list.Items))
		}

		return nil
	}, timeout, 1).Should(BeNil())

	if wantFound {
		return list
	}

	return nil
}

func GetMatchingEvents(
	client kubernetes.Interface, namespace, objName, reasonRegex, msgRegex string, timeout int,
) []corev1.Event {
	var eventList *corev1.EventList

	Eventually(func() error {
		var err error
		eventList, err = client.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})

		return err
	}, timeout, 1).Should(BeNil())

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
	cmd := exec.Command("kubectl", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// in case of failure, print command output (including error)
		//nolint:forbidigo
		fmt.Printf("%s\n", output)
		Fail(fmt.Sprintf("Error: %v", err))
	}
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
		detail := managedPlc.Object["status"].(map[string]interface{})["compliancyDetails"].([]interface{})[0]

		return detail.(map[string]interface{})["conditions"].([]interface{})[0].(map[string]interface{})["message"]
	}

	return nil
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
		"-l=name=config-policy-controller", "--no-headers")

	propPodInfo, err := podCmd.Output()
	if err != nil {
		return []string{err.Error()}
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
			"config-policy-controller", "--", "bash", "-c", metricsCmd)
	}

	matchingMetricsRaw, err := cmd.Output()
	if err != nil {
		if err.Error() == "exit status 1" {
			return []string{} // exit 1 indicates that grep couldn't find a match.
		}

		return []string{err.Error()}
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
