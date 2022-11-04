package controllers

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

var (
	evalLoopHistogram = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "config_policies_evaluation_duration_seconds",
			Help:    "The seconds that it takes to evaluate all configuration policies on the cluster",
			Buckets: []float64{1, 3, 9, 10.5, 15, 30, 60, 90, 120, 180, 300, 450, 600},
		},
	)
	policyEvalSecondsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "config_policy_evaluation_seconds_total",
			Help: "The total seconds taken while evaluating the configuration policy. Use this alongside " +
				"config_policy_evaluation_total.",
		},
		[]string{"name"},
	)
	policyEvalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "config_policy_evaluation_total",
			Help: "The total number of evaluations of the configuration policy. Use this alongside " +
				"config_policy_evaluation_seconds_total.",
		},
		[]string{"name"},
	)
	compareObjSecondsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "compare_objects_seconds_total",
			Help: "The total seconds taken while comparing policy objects. Use this alongside " +
				"compare_objects_evaluation_total.",
		},
		[]string{"config_policy_name", "namespace", "object"},
	)
	compareObjEvalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "compare_objects_evaluation_total",
			Help: "The total number of times the comparison algorithm is run on an object. " +
				"Use this alongside compare_objects_seconds_total.",
		},
		[]string{"config_policy_name", "namespace", "object"},
	)
	// The policyRelatedObjectMap collects a map of related objects to policies
	// in order to populate the gauge:
	//   <kind.version/namespace/name>: []<policy-namespace/policy-name>
	policyRelatedObjectMap   sync.Map
	policyRelatedObjectGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "common_related_objects",
			Help: "A gauge vector of related objects managed by multiple policies.",
		},
		[]string{
			"relatedObject",
			"policy",
		},
	)
)

func init() {
	// Register custom metrics with the global Prometheus registry
	metrics.Registry.MustRegister(evalLoopHistogram)
	metrics.Registry.MustRegister(policyEvalSecondsCounter)
	metrics.Registry.MustRegister(policyEvalCounter)
	metrics.Registry.MustRegister(compareObjSecondsCounter)
	metrics.Registry.MustRegister(compareObjEvalCounter)
	metrics.Registry.MustRegister(policyRelatedObjectGauge)
}

// updateRelatedObjectMetric iterates through the collected related object map, deletes any metrics
// that aren't duplications, and sets a metric for any related object that is handled by multiple
// policies to the number of policies that currently handles it.
func updateRelatedObjectMetric() {
	log.V(3).Info("Updating common_related_objects metric ...")

	policyRelatedObjectMap.Range(func(key any, value any) bool {
		relatedObj := key.(string)
		policies := value.([]string)

		for _, policy := range policies {
			if len(policies) == 1 {
				policyRelatedObjectGauge.DeleteLabelValues(relatedObj, policy)

				continue
			}

			gaugeInstance, err := policyRelatedObjectGauge.GetMetricWithLabelValues(relatedObj, policy)
			if err != nil {
				log.V(3).Error(err, "Failed to retrieve related object gauge")

				continue
			}

			gaugeInstance.Set(float64(len(policies)))
		}

		return true
	})
}

// getObjectString returns a string formatted as:
// <kind>.<version>/<namespace>/<name>
func getObjectString(obj policyv1.RelatedObject) string {
	return fmt.Sprintf("%s.%s/%s/%s",
		obj.Object.Kind, obj.Object.APIVersion,
		obj.Object.Metadata.Namespace, obj.Object.Metadata.Name)
}
