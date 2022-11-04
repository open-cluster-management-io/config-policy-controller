package controllers

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
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
