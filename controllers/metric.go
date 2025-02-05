package controllers

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	v1 "open-cluster-management.io/config-policy-controller/api/v1"
)

var (
	configPolicyStatusGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "configurationpolicy_governance_info",
			Help: "The compliance status of the named ConfigurationPolicy. " +
				"0 == Compliant. 1 == NonCompliant. -1 == Unknown/Pending",
		},
		[]string{
			"policy",           // The name of the configuration policy
			"policy_namespace", // The namespace where the configuration policy is defined
		},
	)
	operatorPolicyStatusGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "operatorpolicy_governance_info",
			Help: "The compliance status of the named OperatorPolicy. " +
				"0 == Compliant. 1 == NonCompliant. -1 == Unknown/Pending",
		},
		[]string{
			"policy",           // The name of the operator policy
			"policy_namespace", // The namespace where the operator policy is defined
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
	metrics.Registry.MustRegister(
		configPolicyStatusGauge,
		operatorPolicyStatusGauge,
		policyEvalSecondsCounter,
		policyEvalCounter,
		compareObjSecondsCounter,
		compareObjEvalCounter,
	)
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

func getStatusValue(complianceState v1.ComplianceState) float64 {
	if complianceState == v1.Compliant {
		return 0
	} else if complianceState == v1.NonCompliant {
		return 1
	}

	return -1
}

func removeOperatorPolicyMetrics(request ctrl.Request) {
	_ = operatorPolicyStatusGauge.DeletePartialMatch(prometheus.Labels{
		"policy":           request.Name,
		"policy_namespace": request.Namespace,
	})
}

func removeConfigPolicyMetrics(request ctrl.Request) {
	// If a metric has an error while deleting, that means the policy was never evaluated so it can be ignored.
	_ = configPolicyStatusGauge.DeletePartialMatch(prometheus.Labels{
		"policy":           request.Name,
		"policy_namespace": request.Namespace,
	})
	_ = policyEvalSecondsCounter.DeleteLabelValues(request.Name)
	_ = policyEvalCounter.DeleteLabelValues(request.Name)
	_ = compareObjEvalCounter.DeletePartialMatch(prometheus.Labels{"config_policy_name": request.Name})
	_ = compareObjSecondsCounter.DeletePartialMatch(prometheus.Labels{"config_policy_name": request.Name})
	_ = policyUserErrorsCounter.DeletePartialMatch(prometheus.Labels{"template": request.Name})
	_ = policySystemErrorsCounter.DeletePartialMatch(prometheus.Labels{"template": request.Name})
}
