package controllers

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
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
	plcTempsProcessSecondsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "config_policy_templates_process_seconds_total",
			Help: "The total seconds taken while processing the configuration policy templates. Use this alongside " +
				"config_policy_templates_process_total.",
		},
		[]string{"name"},
	)
	plcTempsProcessCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "config_policy_templates_process_total",
			Help: "The total number of processes of the configuration policy templates. Use this alongside " +
				"config_policy_templates_process_seconds_total.",
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
	policyUserErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policy_user_errors_total",
			Help: "The number of user errors encountered while processing policies",
		},
		[]string{
			"policy",
			"template",
			"type",
		},
	)
	policySystemErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "policy_system_errors_total",
			Help: "The number of system errors encountered while processing policies",
		},
		[]string{
			"policy",
			"template",
			"type",
		},
	)
)

func init() {
	// Register custom metrics with the global Prometheus registry
	metrics.Registry.MustRegister(policyEvalSecondsCounter)
	metrics.Registry.MustRegister(policyEvalCounter)
	metrics.Registry.MustRegister(plcTempsProcessSecondsCounter)
	metrics.Registry.MustRegister(plcTempsProcessCounter)
	metrics.Registry.MustRegister(compareObjSecondsCounter)
	metrics.Registry.MustRegister(compareObjEvalCounter)
	// Error metrics may already be registered by template sync
	alreadyReg := &prometheus.AlreadyRegisteredError{}

	regErr := metrics.Registry.Register(policySystemErrorsCounter)
	if regErr != nil && !errors.As(regErr, alreadyReg) {
		panic(regErr)
	}

	regErr = metrics.Registry.Register(policyUserErrorsCounter)
	if regErr != nil && !errors.As(regErr, alreadyReg) {
		panic(regErr)
	}
}
