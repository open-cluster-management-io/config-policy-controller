package controllers

import (
	"testing"

	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
)

func TestCalculateComplianceConditionShortCircuitOnInvalid(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	// Simulate invalid validation condition only
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidPolicySpec",
			Message: "spec is invalid: installPlanApproval is prohibited in spec.subscription",
		},
		{
			Type:    opGroupConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "OperatorGroupCompliant",
			Message: "the OperatorGroup is compliant",
		},
	}

	cond := calculateComplianceCondition(pol)

	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Contains(t, cond.Message, "NonCompliant; ")
	assert.Contains(t, cond.Message, "spec is invalid: installPlanApproval is prohibited in spec.subscription")
	assert.NotContains(t, cond.Message, "the status of the OperatorGroup could not be determined")
	assert.NotContains(t, cond.Message, "the status of the Subscription could not be determined")
	assert.NotContains(t, cond.Message, "the status of the InstallPlan could not be determined")
	// When invalid, short-circuit immediately with only the validation message.
	assert.NotContains(t, cond.Message, "the OperatorGroup is compliant")
}

func TestCalculateComplianceConditionInvalidOperatorNamespaceDoesNotShortCircuit(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	// When the invalid validation message is about the operator namespace, it should not short-circuit.
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidPolicySpec",
			Message: "the operator namespace ('airflow-helm') does not exist",
		},
		{
			Type:    opGroupConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "OperatorGroupCompliant",
			Message: "the OperatorGroup required by the policy was created",
		},
		{
			Type:    subConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "SubscriptionMatches",
			Message: "the Subscription required by the policy was created",
		},
	}

	cond := calculateComplianceCondition(pol)

	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Contains(t, cond.Message, "NonCompliant; ")
	assert.Contains(t, cond.Message, "the operator namespace ('airflow-helm') does not exist")
	// Not short-circuited: should include later condition messages too.
	assert.Contains(t, cond.Message, "the OperatorGroup required by the policy was created")
	assert.Contains(t, cond.Message, "the Subscription required by the policy was created")
}

func TestShortCircuitOnMissingValidPolicySpec(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}

	cond := calculateComplianceCondition(pol)

	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "NonCompliant;", cond.Message)
}

func TestCalculateComplianceConditionWithSubscriptionCreated(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "PolicyValidated",
			Message: "the policy spec is valid",
		},
		{
			Type:    opGroupConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "OperatorGroupCompliant",
			Message: "the OperatorGroup is compliant",
		},
		createdCond("Subscription"),
	}

	cond := calculateComplianceCondition(pol)

	expected := "NonCompliant; the policy spec is valid, the OperatorGroup is compliant, " +
		createdCond("Subscription").Message

	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, expected, cond.Message)
}

func TestShortCircuitOnMissingOperatorGroup(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "PolicyValidated",
			Message: "the policy spec is valid",
		},
	}

	cond := calculateComplianceCondition(pol)

	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "NonCompliant; the policy spec is valid", cond.Message)
}

func TestShortCircuitOnMissingSubscription(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "PolicyValidated",
			Message: "the policy spec is valid",
		},
		{
			Type:    opGroupConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "OperatorGroupCompliant",
			Message: "the OperatorGroup is compliant",
		},
	}

	cond := calculateComplianceCondition(pol)

	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "NonCompliant; the policy spec is valid, the OperatorGroup is compliant", cond.Message)
}

func TestCalculateComplianceConditionAggregatesAllCompliant(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "PolicyValidated",
			Message: "the policy spec is valid",
		},
		{
			Type:    opGroupConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "OperatorGroupMatches",
			Message: "the OperatorGroup matches what is required by the policy",
		},
		{
			Type:    subConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "SubscriptionMatches",
			Message: "the Subscription matches what is required by the policy",
		},
		{
			Type:    installPlanConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "NoInstallPlansFound",
			Message: "there are no relevant InstallPlans in the namespace",
		},
		{
			Type:    csvConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "Succeeded",
			Message: "the ClusterServiceVersion is succeeded",
		},
		{
			Type:    crdConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "RelevantCRDFound",
			Message: "there are CRDs present for the operator",
		},
		{
			Type:    deploymentConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "DeploymentsAvailable",
			Message: "all operator Deployments have their minimum availability",
		},
		// CatalogSource polarity: False means healthy/non-blocking for compliance aggregation
		{
			Type:    catalogSrcConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  "CatalogSourcesFound",
			Message: "CatalogSource was found",
		},
	}

	cond := calculateComplianceCondition(pol)

	expected := "Compliant; " +
		"the policy spec is valid, " +
		"the OperatorGroup matches what is required by the policy, " +
		"the Subscription matches what is required by the policy, " +
		"there are no relevant InstallPlans in the namespace, " +
		"the ClusterServiceVersion is succeeded, " +
		"there are CRDs present for the operator, " +
		"all operator Deployments have their minimum availability, " +
		"CatalogSource was found"

	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, expected, cond.Message)
}

func TestCalculateComplianceConditionWithOperatorGroupCreated(t *testing.T) {
	pol := &policyv1beta1.OperatorPolicy{}
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "PolicyValidated",
			Message: "the policy spec is valid",
		},
		createdCond("OperatorGroup"),
		matchesCond("Subscription"),
		{
			Type:    installPlanConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "NoInstallPlansFound",
			Message: "there are no relevant InstallPlans in the namespace",
		},
		{
			Type:    csvConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "Succeeded",
			Message: "the ClusterServiceVersion is succeeded",
		},
		{
			Type:    crdConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "RelevantCRDFound",
			Message: "there are CRDs present for the operator",
		},
		{
			Type:    deploymentConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "DeploymentsAvailable",
			Message: "all operator Deployments have their minimum availability",
		},
		// CatalogSource polarity: False means healthy/non-blocking for compliance aggregation
		{
			Type:    catalogSrcConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  "CatalogSourcesFound",
			Message: "CatalogSource was found",
		},
	}

	cond := calculateComplianceCondition(pol)

	expected := "Compliant; " +
		"the policy spec is valid, " +
		createdCond("OperatorGroup").Message + ", " +
		matchesCond("Subscription").Message + ", " +
		"there are no relevant InstallPlans in the namespace, " +
		"the ClusterServiceVersion is succeeded, " +
		"there are CRDs present for the operator, " +
		"all operator Deployments have their minimum availability, " +
		"CatalogSource was found"

	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, expected, cond.Message)
}

func TestExistingInstallPlanObj(t *testing.T) {
	// Empty InstallPlan
	testIP := &operatorv1alpha1.InstallPlan{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{},
		Spec:       operatorv1alpha1.InstallPlanSpec{},
		Status:     operatorv1alpha1.InstallPlanStatus{},
	}

	// Test available upgrades with Compliant complianceConfig
	phase := "RequiresApproval"
	complianceConfig := policyv1beta1.ComplianceConfigAction("Compliant")
	res := existingInstallPlanObj(testIP, phase, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)

	// Test available upgrades with NonCompliant complianceConfig
	phase = "RequiresApproval"
	complianceConfig = "NonCompliant"
	res = existingInstallPlanObj(testIP, phase, complianceConfig)
	assert.Equal(t, "NonCompliant", res.Compliant)

	// Test installing phase with Compliant complianceConfig
	phase = "Installing"
	complianceConfig = policyv1beta1.ComplianceConfigAction("Compliant")
	res = existingInstallPlanObj(testIP, phase, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)

	// Test installing phase with NonCompliant complianceConfig
	phase = "Installing"
	complianceConfig = "NonCompliant"
	res = existingInstallPlanObj(testIP, phase, complianceConfig)
	assert.Equal(t, "NonCompliant", res.Compliant)
}

func TestBuildDeploymentCond(t *testing.T) {
	complianceConfig := policyv1beta1.Compliant
	depsExist := true                                  // if any deployments
	unavailableDeps := make([]appsv1.Deployment, 0, 1) // deployments are there, but no available

	// Test Compliant complianceConfig with available deployments
	cond := buildDeploymentCond(complianceConfig, depsExist, unavailableDeps)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)

	// Test NonCompliant complianceConfig with available deployments
	complianceConfig = policyv1beta1.NonCompliant
	cond = buildDeploymentCond(complianceConfig, depsExist, unavailableDeps)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, "all operator Deployments have their minimum availability", cond.Message)

	// Test Compliant complianceConfig with unavailable deployments
	testDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "TestDeployment",
		},
		Spec: appsv1.DeploymentSpec{},
		Status: appsv1.DeploymentStatus{
			UnavailableReplicas: 1,
		},
	}
	complianceConfig = policyv1beta1.Compliant

	unavailableDeps = append(unavailableDeps, testDeployment)
	cond = buildDeploymentCond(complianceConfig, depsExist, unavailableDeps)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)

	// Test NonCompliance complianceConfig with unavailable deployments
	complianceConfig = policyv1beta1.NonCompliant
	cond = buildDeploymentCond(complianceConfig, depsExist, unavailableDeps)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "the deployments TestDeployment do not have their minimum availability", cond.Message)
}

func TestExistingDeploymentObj(t *testing.T) {
	testDeployment := &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{},
		Spec:       appsv1.DeploymentSpec{},
		Status: appsv1.DeploymentStatus{
			UnavailableReplicas: 1,
		},
	}

	// Test Compliant complianceConfig with UnavailableReplicas > 0
	complianceConfig := policyv1beta1.ComplianceConfigAction("Compliant")
	res := existingDeploymentObj(testDeployment, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)
	assert.Equal(t, "Deployment Unavailable"+
		" (policy compliance is not impacted due to spec.complianceConfig.deploymentsUnavailable)", res.Reason)

	// Test Compliant complianceConfig with UnavailableReplicas = 0
	complianceConfig = "Compliant"
	testDeployment.Status.UnavailableReplicas = 0
	res = existingDeploymentObj(testDeployment, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)
	assert.Equal(t, "Deployment Available", res.Reason)

	// Test NonCompliant complianceConfig with UnavailableReplicas > 0
	complianceConfig = "NonCompliant"
	testDeployment.Status.UnavailableReplicas = 1
	res = existingDeploymentObj(testDeployment, complianceConfig)
	assert.Equal(t, "NonCompliant", res.Compliant)
	assert.Equal(t, "Deployment Unavailable", res.Reason)

	// Test NonCompliant complianceConfig with UnavailableReplicas = 0
	complianceConfig = "NonCompliant"
	testDeployment.Status.UnavailableReplicas = 0
	res = existingDeploymentObj(testDeployment, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)
	assert.Equal(t, "Deployment Available", res.Reason)
}

func TestCatalogSourceFindCond(t *testing.T) {
	complianceConfig := policyv1beta1.Compliant
	isUnhealthy := false
	isMissing := false
	name := "TestCatalog"

	// Test Compliant complianceConfig with healthy CatalogSource
	cond := catalogSourceFindCond(complianceConfig, isUnhealthy, isMissing, name)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)

	// Test Compliant complianceConfig with unhealthy CatalogSource
	isUnhealthy = true
	cond = catalogSourceFindCond(complianceConfig, isUnhealthy, isMissing, name)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)

	// Test NonCompliant complianceConfig with healthy CatalogSource
	isUnhealthy = false
	complianceConfig = policyv1beta1.NonCompliant
	cond = catalogSourceFindCond(complianceConfig, isUnhealthy, isMissing, name)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "CatalogSource was found", cond.Message)

	// Test NonCompliant complianceConfig with unhealthy CatalogSource
	isUnhealthy = true
	cond = catalogSourceFindCond(complianceConfig, isUnhealthy, isMissing, name)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, "CatalogSource was found but is unhealthy", cond.Message)
}

func TestCatalogSourceObj(t *testing.T) {
	catalogName := "TestCatalog"
	catalogNS := "TestNamespace"

	// Test Compliant complianceConfig with healthy CatalogSource
	isUnhealthy := false
	isMissing := false
	complianceConfig := policyv1beta1.ComplianceConfigAction("Compliant")
	res := catalogSourceObj(catalogName, catalogNS, isUnhealthy, isMissing, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)
	assert.Equal(t, "Resource found as expected", res.Reason)

	// Test Compliant complianceConfig with unhealthy CatalogSource
	isUnhealthy = true
	isMissing = false
	complianceConfig = "Compliant"
	res = catalogSourceObj(catalogName, catalogNS, isUnhealthy, isMissing, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)
	assert.Equal(t, "Resource found as expected but is unhealthy", res.Reason)

	// Test NonCompliant complianceConfig with healthy CatalogSource
	isUnhealthy = false
	isMissing = false
	complianceConfig = "NonCompliant"
	res = catalogSourceObj(catalogName, catalogNS, isUnhealthy, isMissing, complianceConfig)
	assert.Equal(t, "Compliant", res.Compliant)
	assert.Equal(t, "Resource found as expected", res.Reason)

	// Test NonCompliant complianceConfig with unhealthy CatalogSource
	isUnhealthy = true
	isMissing = false
	complianceConfig = "NonCompliant"
	res = catalogSourceObj(catalogName, catalogNS, isUnhealthy, isMissing, complianceConfig)
	assert.Equal(t, "NonCompliant", res.Compliant)
	assert.Equal(t, "Resource found as expected but is unhealthy", res.Reason)
}
