package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
	common "open-cluster-management.io/config-policy-controller/pkg/common"
)

// updateStatus takes one condition to update, and related objects for that condition. The related
// objects given will replace all existing relatedObjects with the same gvk. If a condition is
// changed, the compliance will be recalculated and a compliance event will be emitted. The
// condition and related objects can match what is already in the status - in that case, no API
// calls are made. The `lastTransitionTime` on a condition is not considered when checking if the
// condition has changed. If not provided, the `lastTransitionTime` will use "now". It also handles
// preserving the `CreatedByPolicy` property on relatedObjects.
//
// This function requires that all given related objects are of the same kind.
//
// Note that only changing the related objects will not emit a new compliance event, but will update
// the status.
func (r *OperatorPolicyReconciler) updateStatus(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	updatedCondition metav1.Condition,
	updatedRelatedObjs ...policyv1.RelatedObject,
) error {
	condChanged := false

	if updatedCondition.LastTransitionTime.IsZero() {
		updatedCondition.LastTransitionTime = metav1.Now()
	}

	condIdx, existingCondition := policy.Status.GetCondition(updatedCondition.Type)
	if condIdx == -1 {
		condChanged = true

		// Just append, the conditions will be sorted later.
		policy.Status.Conditions = append(policy.Status.Conditions, updatedCondition)
	} else if conditionChanged(updatedCondition, existingCondition) {
		condChanged = true

		policy.Status.Conditions[condIdx] = updatedCondition
	}

	if condChanged {
		updatedComplianceCondition := calculateComplianceCondition(policy)

		compCondIdx, _ := policy.Status.GetCondition(updatedComplianceCondition.Type)
		if compCondIdx == -1 {
			policy.Status.Conditions = append(policy.Status.Conditions, updatedComplianceCondition)
		} else {
			policy.Status.Conditions[compCondIdx] = updatedComplianceCondition
		}

		// Sort the conditions based on their type.
		sort.SliceStable(policy.Status.Conditions, func(i, j int) bool {
			return policy.Status.Conditions[i].Type < policy.Status.Conditions[j].Type
		})

		if updatedComplianceCondition.Status == metav1.ConditionTrue {
			policy.Status.ComplianceState = policyv1.Compliant
		} else {
			policy.Status.ComplianceState = policyv1.NonCompliant
		}

		err := r.emitComplianceEvent(ctx, policy, updatedComplianceCondition)
		if err != nil {
			return err
		}
	}

	relObjsChanged := false

	prevRelObjs := make(map[int]policyv1.RelatedObject)
	if len(updatedRelatedObjs) != 0 {
		prevRelObjs = policy.Status.RelatedObjsOfKind(updatedRelatedObjs[0].Object.Kind)
	}

	for _, prevObj := range prevRelObjs {
		nameFound := false

		for i, updatedObj := range updatedRelatedObjs {
			if prevObj.Object.Metadata.Name != updatedObj.Object.Metadata.Name {
				continue
			}

			nameFound = true

			if updatedObj.Properties != nil && prevObj.Properties != nil {
				if updatedObj.Properties.UID != prevObj.Properties.UID {
					relObjsChanged = true
				} else if prevObj.Properties.CreatedByPolicy != nil {
					// There is an assumption here that it will never need to transition to false.
					updatedRelatedObjs[i].Properties.CreatedByPolicy = prevObj.Properties.CreatedByPolicy
				}
			}

			if prevObj.Compliant != updatedObj.Compliant || prevObj.Reason != updatedObj.Reason {
				relObjsChanged = true
			}
		}

		if !nameFound {
			relObjsChanged = true
		}
	}

	// Catch the case where there is a new object in updatedRelatedObjs
	if len(prevRelObjs) != len(updatedRelatedObjs) {
		relObjsChanged = true
	}

	if relObjsChanged {
		// start with the related objects which do not match the currently considered kind
		newRelObjs := make([]policyv1.RelatedObject, 0)

		for idx, relObj := range policy.Status.RelatedObjects {
			if _, matchedIdx := prevRelObjs[idx]; !matchedIdx {
				newRelObjs = append(newRelObjs, relObj)
			}
		}

		// add the new related objects
		newRelObjs = append(newRelObjs, updatedRelatedObjs...)

		// sort the related objects by kind and name
		sort.SliceStable(newRelObjs, func(i, j int) bool {
			if newRelObjs[i].Object.Kind != newRelObjs[j].Object.Kind {
				return newRelObjs[i].Object.Kind < newRelObjs[j].Object.Kind
			}

			return newRelObjs[i].Object.Metadata.Name < newRelObjs[j].Object.Metadata.Name
		})

		policy.Status.RelatedObjects = newRelObjs
	}

	if condChanged || relObjsChanged {
		if policy.Status.RelatedObjects == nil {
			policy.Status.RelatedObjects = []policyv1.RelatedObject{}
		}

		return r.Status().Update(ctx, policy)
	}

	return nil
}

func conditionChanged(updatedCondition, existingCondition metav1.Condition) bool {
	if updatedCondition.Message != existingCondition.Message {
		return true
	}

	if updatedCondition.Reason != existingCondition.Reason {
		return true
	}

	if updatedCondition.Status != existingCondition.Status {
		return true
	}

	return false
}

// The Compliance condition is calculated by going through the known conditions in a consistent
// order, checking if there are any reasons the policy should be NonCompliant, and accumulating
// the reasons into one string to reflect the whole status.
func calculateComplianceCondition(policy *policyv1beta1.OperatorPolicy) metav1.Condition {
	foundNonCompliant := false
	messages := make([]string, 0)

	idx, cond := policy.Status.GetCondition(validPolicyConditionType)
	if idx == -1 {
		messages = append(messages, "the validity of the policy is unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionTrue {
			foundNonCompliant = true
		}
	}

	idx, cond = policy.Status.GetCondition(opGroupConditionType)
	if idx == -1 {
		messages = append(messages, "the status of the OperatorGroup is unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionTrue {
			foundNonCompliant = true
		}
	}

	idx, cond = policy.Status.GetCondition(subConditionType)
	if idx == -1 {
		messages = append(messages, "the status of the Subscription is unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionTrue {
			foundNonCompliant = true
		}
	}

	idx, cond = policy.Status.GetCondition(installPlanConditionType)
	if idx == -1 {
		messages = append(messages, "the status of the InstallPlan is unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionTrue {
			foundNonCompliant = true
		}
	}

	idx, cond = policy.Status.GetCondition(csvConditionType)
	if idx == -1 {
		messages = append(messages, "the status of the ClusterServiceVersion is unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionTrue {
			foundNonCompliant = true
		}
	}

	idx, cond = policy.Status.GetCondition(deploymentConditionType)
	if idx == -1 {
		messages = append(messages, "the status of the Deployments are unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionTrue {
			foundNonCompliant = true
		}
	}

	idx, cond = policy.Status.GetCondition(catalogSrcConditionType)
	if idx == -1 {
		messages = append(messages, "the status of the CatalogSource is unknown")
		foundNonCompliant = true
	} else {
		messages = append(messages, cond.Message)

		if cond.Status != metav1.ConditionFalse {
			foundNonCompliant = true
		}
	}

	if foundNonCompliant {
		return metav1.Condition{
			Type:               compliantConditionType,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "NonCompliant",
			Message:            "NonCompliant; " + strings.Join(messages, ", "),
		}
	}

	return metav1.Condition{
		Type:               compliantConditionType,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "Compliant",
		Message:            "Compliant; " + strings.Join(messages, ", "),
	}
}

func (r *OperatorPolicyReconciler) emitComplianceEvent(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	complianceCondition metav1.Condition,
) error {
	if len(policy.OwnerReferences) == 0 {
		return nil // there is nothing to do, since no owner is set
	}

	ownerRef := policy.OwnerReferences[0]
	now := time.Now()
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			// This event name matches the convention of recorders from client-go
			Name:      fmt.Sprintf("%v.%x", ownerRef.Name, now.UnixNano()),
			Namespace: policy.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       ownerRef.Kind,
			Namespace:  policy.Namespace, // k8s ensures owners are always in the same namespace
			Name:       ownerRef.Name,
			UID:        ownerRef.UID,
			APIVersion: ownerRef.APIVersion,
		},
		Reason:  fmt.Sprintf(eventFmtStr, policy.Namespace, policy.Name),
		Message: complianceCondition.Message,
		Source: corev1.EventSource{
			Component: ControllerName,
			Host:      r.InstanceName,
		},
		FirstTimestamp: metav1.NewTime(now),
		LastTimestamp:  metav1.NewTime(now),
		Count:          1,
		Type:           "Normal",
		Action:         "ComplianceStateUpdate",
		Related: &corev1.ObjectReference{
			Kind:       policy.Kind,
			Namespace:  policy.Namespace,
			Name:       policy.Name,
			UID:        policy.UID,
			APIVersion: policy.APIVersion,
		},
		ReportingController: ControllerName,
		ReportingInstance:   r.InstanceName,
	}

	eventAnnotations := map[string]string{}

	policyAnnotations := policy.GetAnnotations()
	if policyAnnotations[common.ParentDBIDAnnotation] != "" {
		eventAnnotations[common.ParentDBIDAnnotation] = policyAnnotations[common.ParentDBIDAnnotation]
	}

	if policyAnnotations[common.PolicyDBIDAnnotation] != "" {
		eventAnnotations[common.PolicyDBIDAnnotation] = policyAnnotations[common.PolicyDBIDAnnotation]
	}

	if len(eventAnnotations) > 0 {
		event.Annotations = eventAnnotations
	}

	if policy.Status.ComplianceState != policyv1.Compliant {
		event.Type = "Warning"
	}

	return r.Create(ctx, event)
}

const (
	compliantConditionType   = "Compliant"
	validPolicyConditionType = "ValidPolicySpec"
	opGroupConditionType     = "OperatorGroupCompliant"
	subConditionType         = "SubscriptionCompliant"
	csvConditionType         = "ClusterServiceVersionCompliant"
	deploymentConditionType  = "DeploymentCompliant"
	catalogSrcConditionType  = "CatalogSourcesUnhealthy"
	installPlanConditionType = "InstallPlanCompliant"
)

func condType(kind string) string {
	switch kind {
	case "OperatorGroup":
		return opGroupConditionType
	case "Subscription":
		return subConditionType
	case "InstallPlan":
		return installPlanConditionType
	case "ClusterServiceVersion":
		return csvConditionType
	case "Deployment":
		return deploymentConditionType
	case "CatalogSource":
		return catalogSrcConditionType
	default:
		panic("Unknown condition type for kind " + kind)
	}
}

func invalidCausingUnknownCond(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionUnknown,
		Reason:  "InvalidPolicySpec",
		Message: "the status of the " + kind + " could not be determined because the policy is invalid",
	}
}

// missingWantedCond returns a NonCompliant condition, with a Reason like '____Missing'
// and a Message like 'the ____ required by the policy was not found'
func missingWantedCond(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionFalse,
		Reason:  kind + "Missing",
		Message: "the " + kind + " required by the policy was not found",
	}
}

// createdCond returns a Compliant condition, with a Reason like'____Created',
// and a Message like 'the ____ required by the policy was created'
func createdCond(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionTrue,
		Reason:  kind + "Created",
		Message: "the " + kind + " required by the policy was created",
	}
}

// matchesCond returns a Compliant condition, with a Reason like'____Matches',
// and a Message like 'the ____ matches what is required by the policy'
func matchesCond(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionTrue,
		Reason:  kind + "Matches",
		Message: "the " + kind + " matches what is required by the policy",
	}
}

// mismatchCond returns a NonCompliant condition with a Reason like '____Mismatch',
// and a Message like 'the ____ found on the cluster does not match the policy'
func mismatchCond(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionFalse,
		Reason:  kind + "Mismatch",
		Message: "the " + kind + " found on the cluster does not match the policy",
	}
}

// mismatchCondUnfixable returns a NonCompliant condition with a Reason like '____Mismatch',
// and a Message like 'the ____ found on the cluster does not match the policy and can't be enforced'
func mismatchCondUnfixable(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionFalse,
		Reason:  kind + "Mismatch",
		Message: "the " + kind + " found on the cluster does not match the policy and can't be enforced",
	}
}

// updatedCond returns a Compliant condition, with a Reason like'____Updated',
// and a Message like 'the ____ was updated to match the policy'
func updatedCond(kind string) metav1.Condition {
	return metav1.Condition{
		Type:    condType(kind),
		Status:  metav1.ConditionTrue,
		Reason:  kind + "Updated",
		Message: "the " + kind + " was updated to match the policy",
	}
}

func validationCond(validationErrors []error) metav1.Condition {
	if len(validationErrors) == 0 {
		return metav1.Condition{
			Type:    validPolicyConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  "PolicyValidated",
			Message: "the policy spec is valid",
		}
	}

	msgs := make([]string, len(validationErrors))

	for i, err := range validationErrors {
		msgs[i] = err.Error()
	}

	return metav1.Condition{
		Type:    validPolicyConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  "InvalidPolicySpec",
		Message: strings.Join(msgs, ", "),
	}
}

// opGroupPreexistingCond is a Compliant condition with Reason 'PreexistingOperatorGroupFound',
// and Message 'the policy does not specify an OperatorGroup but one already exists in the
// namespace - assuming that OperatorGroup is correct'
var opGroupPreexistingCond = metav1.Condition{
	Type:   opGroupConditionType,
	Status: metav1.ConditionTrue,
	Reason: "PreexistingOperatorGroupFound",
	Message: "the policy does not specify an OperatorGroup but one already exists in the namespace - " +
		"assuming that OperatorGroup is correct",
}

// opGroupTooManyCond is a NonCompliant condition with Reason 'TooManyOperatorGroups',
// and Message 'there is more than one OperatorGroup in the namespace'
var opGroupTooManyCond = metav1.Condition{
	Type:    opGroupConditionType,
	Status:  metav1.ConditionFalse,
	Reason:  "TooManyOperatorGroups",
	Message: "there is more than one OperatorGroup in the namespace",
}

// noInstallPlansCond is a Compliant condition with Reason 'NoInstallPlansFound',
// and Message 'there are no relevant InstallPlans in the namespace'
var noInstallPlansCond = metav1.Condition{
	Type:    installPlanConditionType,
	Status:  metav1.ConditionTrue,
	Reason:  "NoInstallPlansFound",
	Message: "there are no relevant InstallPlans in the namespace",
}

// installPlanFailed is a NonCompliant condition with Reason 'InstallPlanFailed'
// and message 'the current InstallPlan has failed'
var installPlanFailed = metav1.Condition{
	Type:    installPlanConditionType,
	Status:  metav1.ConditionFalse,
	Reason:  "InstallPlanFailed",
	Message: "the current InstallPlan has failed",
}

// installPlanInstallingCond is a NonCompliant condition with Reason 'InstallPlansInstalling'
// and message 'a relevant InstallPlan is actively installing'
var installPlanInstallingCond = metav1.Condition{
	Type:    installPlanConditionType,
	Status:  metav1.ConditionFalse,
	Reason:  "InstallPlansInstalling",
	Message: "a relevant InstallPlan is actively installing",
}

// installPlansNoApprovals is a Compliant condition with Reason 'NoInstallPlansRequiringApproval'
// and message 'no InstallPlans requiring approval were found'
var installPlansNoApprovals = metav1.Condition{
	Type:    installPlanConditionType,
	Status:  metav1.ConditionTrue,
	Reason:  "NoInstallPlansRequiringApproval",
	Message: "no InstallPlans requiring approval were found",
}

// installPlanUpgradeCond is a NonCompliant condition with Reason 'InstallPlanRequiresApproval'
// and a message detailing which possible updates are available
func installPlanUpgradeCond(versions []string, approvableIPs []unstructured.Unstructured) metav1.Condition {
	// FUTURE: check policy.spec.statusConfig.upgradesAvailable to determine `compliant`.
	// For now this condition assumes it is set to 'NonCompliant'
	cond := metav1.Condition{
		Type:   installPlanConditionType,
		Status: metav1.ConditionFalse,
		Reason: "InstallPlanRequiresApproval",
	}

	if len(versions) == 1 {
		cond.Message = fmt.Sprintf("an InstallPlan to update to %v is available for approval", versions[0])
	} else {
		cond.Message = fmt.Sprintf("there are multiple InstallPlans available for approval (%v)",
			strings.Join(versions, ", or "))
	}

	if approvableIPs != nil && len(approvableIPs) == 0 {
		cond.Message += " but not allowed by the specified versions in the policy"
	}

	if len(approvableIPs) > 1 {
		cond.Message += " but multiple of those match the versions specified in the policy"
	}

	return cond
}

// installPlanApprovedCond is a Compliant condition with Reason 'InstallPlanApproved'
// and a message like 'the InstallPlan for _____ was approved'
func installPlanApprovedCond(version string) metav1.Condition {
	return metav1.Condition{
		Type:    installPlanConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "InstallPlanApproved",
		Message: fmt.Sprintf("the InstallPlan for %v was approved", version),
	}
}

// buildCSVCond takes a csv and returns a shortened version of its most recent Condition
func buildCSVCond(csv *operatorv1alpha1.ClusterServiceVersion) metav1.Condition {
	status := metav1.ConditionFalse
	if csv.Status.Phase == operatorv1alpha1.CSVPhaseSucceeded {
		status = metav1.ConditionTrue
	}

	return metav1.Condition{
		Type:    condType(csv.Kind),
		Status:  status,
		Reason:  string(csv.Status.Reason),
		Message: "ClusterServiceVersion - " + csv.Status.Message,
	}
}

var noCSVCond = metav1.Condition{
	Type:    csvConditionType,
	Status:  metav1.ConditionTrue,
	Reason:  "NoRelevantClusterServiceVersion",
	Message: "A relevant installed ClusterServiceVersion could not be found",
}

func buildDeploymentCond(
	depsExist bool,
	unavailableDeps []appsv1.Deployment,
) metav1.Condition {
	status := metav1.ConditionTrue
	reason := "DeploymentsAvailable"
	message := "All operator Deployments have their minimum availability"

	if !depsExist {
		reason = "NoExistingDeployments"
		message = "No existing operator Deployments"
	}

	if len(unavailableDeps) != 0 {
		status = metav1.ConditionFalse
		reason = "DeploymentsUnavailable"

		var depNames []string
		for _, dep := range unavailableDeps {
			depNames = append(depNames, dep.Name)
		}

		names := strings.Join(depNames, ", ")
		message = fmt.Sprintf("Deployments %s do not have their minimum availability", names)
	}

	return metav1.Condition{
		Type:    condType(deploymentGVK.Kind),
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}

var noDeploymentsCond = metav1.Condition{
	Type:    deploymentConditionType,
	Status:  metav1.ConditionTrue,
	Reason:  "NoRelevantDeployments",
	Message: "The ClusterServiceVersion is missing, thus meaning there are no relevant deployments",
}

// catalogSourceFindCond is a conditionally compliant condition with reason
// based on the `isUnhealthy` and `isMissing` parameters
func catalogSourceFindCond(isUnhealthy bool, isMissing bool) metav1.Condition {
	status := metav1.ConditionFalse
	reason := "CatalogSourcesFound"
	message := "CatalogSource was found"

	if isUnhealthy {
		status = metav1.ConditionTrue
		reason = "CatalogSourcesFoundUnhealthy"
		message = "CatalogSource was found but is unhealthy"
	}

	if isMissing {
		status = metav1.ConditionTrue
		reason = "CatalogSourcesNotFound"
		message = "CatalogSource was not found"
	}

	return metav1.Condition{
		Type:    "CatalogSourcesUnhealthy",
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}

// catalogSourceUnknownCond is a NonCompliant condition
var catalogSourceUnknownCond = metav1.Condition{
	Type:    "CatalogSourcesUnknownState",
	Status:  metav1.ConditionTrue,
	Reason:  "LastObservedUnknown",
	Message: "Could not determine last observed state of CatalogSource",
}

// missingWantedObj returns a NonCompliant RelatedObject with reason = 'Resource not found but should exist'
func missingWantedObj(obj client.Object) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(obj),
		Compliant: string(policyv1.NonCompliant),
		Reason:    reasonWantFoundDNE,
	}
}

// createdObj returns a Compliant RelatedObject with reason = 'K8s creation success'
func createdObj(obj client.Object) policyv1.RelatedObject {
	created := true

	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(obj),
		Compliant: string(policyv1.Compliant),
		Reason:    reasonWantFoundCreated,
		Properties: &policyv1.ObjectProperties{
			CreatedByPolicy: &created,
			UID:             string(obj.GetUID()),
		},
	}
}

// matchedObj returns a Compliant RelatedObject with reason = 'Resource found as expected'
func matchedObj(obj client.Object) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(obj),
		Compliant: string(policyv1.Compliant),
		Reason:    reasonWantFoundExists,
		Properties: &policyv1.ObjectProperties{
			UID: string(obj.GetUID()),
		},
	}
}

// mismatchedObj returns a NonCompliant RelatedObject with reason = 'Resource found but does not match'
func mismatchedObj(obj client.Object) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(obj),
		Compliant: string(policyv1.NonCompliant),
		Reason:    reasonWantFoundNoMatch,
		Properties: &policyv1.ObjectProperties{
			UID: string(obj.GetUID()),
		},
	}
}

// updatedObj returns a Compliant RelatedObject with reason = 'K8s update success'
func updatedObj(obj client.Object) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(obj),
		Compliant: string(policyv1.Compliant),
		Reason:    reasonUpdateSuccess,
		Properties: &policyv1.ObjectProperties{
			UID: string(obj.GetUID()),
		},
	}
}

func nonCompObj(obj client.Object, reason string) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(obj),
		Compliant: string(policyv1.NonCompliant),
		Reason:    reason,
		Properties: &policyv1.ObjectProperties{
			UID: string(obj.GetUID()),
		},
	}
}

// opGroupTooManyObjs returns a list of NonCompliant RelatedObjects, each with
// reason = 'There is more than one OperatorGroup in this namespace'
func opGroupTooManyObjs(opGroups []unstructured.Unstructured) []policyv1.RelatedObject {
	objs := make([]policyv1.RelatedObject, len(opGroups))

	for i, opGroup := range opGroups {
		opGroup := opGroup
		objs[i] = policyv1.RelatedObject{
			Object:    policyv1.ObjectResourceFromObj(&opGroups[i]),
			Compliant: string(policyv1.NonCompliant),
			Reason:    "There is more than one OperatorGroup in this namespace",
			Properties: &policyv1.ObjectProperties{
				UID: string(opGroup.GetUID()),
			},
		}
	}

	return objs
}

// noInstallPlansObj returns a compliant RelatedObject with
// reason = 'There are no relevant InstallPlans in this namespace'
func noInstallPlansObj(namespace string) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object: policyv1.ObjectResource{
			Kind:       installPlanGVK.Kind,
			APIVersion: installPlanGVK.GroupVersion().String(),
			Metadata: policyv1.ObjectMetadata{
				Name:      "*",
				Namespace: namespace,
			},
		},
		Compliant: string(policyv1.Compliant),
		Reason:    "There are no relevant InstallPlans in this namespace",
	}
}

func existingInstallPlanObj(ip client.Object, phase string) policyv1.RelatedObject {
	relObj := policyv1.RelatedObject{
		Object: policyv1.ObjectResourceFromObj(ip),
		Properties: &policyv1.ObjectProperties{
			UID: string(ip.GetUID()),
		},
	}

	if phase != "" {
		relObj.Reason = "The InstallPlan is " + phase
	} else {
		relObj.Reason = "The InstallPlan is Unknown"
	}

	switch phase {
	case string(operatorv1alpha1.InstallPlanPhaseRequiresApproval):
		// FUTURE: check policy.spec.statusConfig.upgradesAvailable to determine `compliant`.
		// For now, assume it is set to 'NonCompliant'
		relObj.Compliant = string(policyv1.NonCompliant)
	case string(operatorv1alpha1.InstallPlanPhaseInstalling):
		// if it's still installing, then it shouldn't be considered compliant yet.
		relObj.Compliant = string(policyv1.NonCompliant)
	}

	return relObj
}

func missingCSVObj(name string, namespace string) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object: policyv1.ObjectResource{
			Kind:       clusterServiceVersionGVK.Kind,
			APIVersion: clusterServiceVersionGVK.GroupVersion().String(),
			Metadata: policyv1.ObjectMetadata{
				Name:      name,
				Namespace: namespace,
			},
		},
		Compliant: string(policyv1.NonCompliant),
		Reason:    reasonWantFoundDNE,
	}
}

func existingCSVObj(csv *operatorv1alpha1.ClusterServiceVersion) policyv1.RelatedObject {
	compliance := policyv1.NonCompliant
	if csv.Status.Phase == operatorv1alpha1.CSVPhaseSucceeded {
		compliance = policyv1.Compliant
	}

	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(csv),
		Compliant: string(compliance),
		Reason:    string(csv.Status.Reason),
		Properties: &policyv1.ObjectProperties{
			UID: string(csv.GetUID()),
		},
	}
}

// represents a lack of relevant CSV
var noExistingCSVObj = policyv1.RelatedObject{
	Object: policyv1.ObjectResource{
		Kind:       clusterServiceVersionGVK.Kind,
		APIVersion: clusterServiceVersionGVK.GroupVersion().String(),
		Metadata: policyv1.ObjectMetadata{
			Name: "*",
		},
	},
	Compliant: string(policyv1.UnknownCompliancy),
	Reason:    "No relevant ClusterServiceVersion found",
}

func missingDeploymentObj(name string, namespace string) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object: policyv1.ObjectResource{
			Kind:       deploymentGVK.Kind,
			APIVersion: deploymentGVK.GroupVersion().String(),
			Metadata: policyv1.ObjectMetadata{
				Name:      name,
				Namespace: namespace,
			},
		},
		Compliant: string(policyv1.NonCompliant),
		Reason:    reasonWantFoundDNE,
	}
}

func existingDeploymentObj(dep *appsv1.Deployment) policyv1.RelatedObject {
	compliance := policyv1.NonCompliant
	reason := "Deployment Unavailable"

	if dep.Status.UnavailableReplicas == 0 {
		compliance = policyv1.Compliant
		reason = "Deployment Available"
	}

	return policyv1.RelatedObject{
		Object:    policyv1.ObjectResourceFromObj(dep),
		Compliant: string(compliance),
		Reason:    reason,
		Properties: &policyv1.ObjectProperties{
			UID: string(dep.GetUID()),
		},
	}
}

// represents a lack of relevant deployments
var noExistingDeploymentObj = policyv1.RelatedObject{
	Object: policyv1.ObjectResource{
		Kind:       deploymentGVK.Kind,
		APIVersion: deploymentGVK.GroupVersion().String(),
		Metadata: policyv1.ObjectMetadata{
			Name: "*",
		},
	},
	Compliant: string(policyv1.UnknownCompliancy),
	Reason:    "No relevant deployments found",
}

// catalogSourceObj returns a conditionally compliant RelatedObject with reason based on the
// `isUnhealthy` and `isMissing` parameters
func catalogSourceObj(catalogName string, catalogNS string, isUnhealthy bool, isMissing bool) policyv1.RelatedObject {
	compliance := string(policyv1.Compliant)
	reason := reasonWantFoundExists

	if isUnhealthy {
		compliance = string(policyv1.NonCompliant)
		reason = reasonWantFoundExists + " but is unhealthy"
	}

	if isMissing {
		compliance = string(policyv1.NonCompliant)
		reason = reasonWantFoundDNE
	}

	return policyv1.RelatedObject{
		Object: policyv1.ObjectResource{
			Kind:       catalogSrcGVK.Kind,
			APIVersion: catalogSrcGVK.GroupVersion().String(),
			Metadata: policyv1.ObjectMetadata{
				Name:      catalogName,
				Namespace: catalogNS,
			},
		},
		Compliant: compliance,
		Reason:    reason,
	}
}

// catalogSrcUnknownObj returns a NonCompliant RelatedObject with
// reason = 'Resource found but current state is unknown'
func catalogSrcUnknownObj(catalogName string, catalogNS string) policyv1.RelatedObject {
	return policyv1.RelatedObject{
		Object: policyv1.ObjectResource{
			Kind:       catalogSrcGVK.Kind,
			APIVersion: catalogSrcGVK.GroupVersion().String(),
			Metadata: policyv1.ObjectMetadata{
				Name:      catalogName,
				Namespace: catalogNS,
			},
		},
		Compliant: string(policyv1.NonCompliant),
		Reason:    "Resource found but current state is unknown",
	}
}
