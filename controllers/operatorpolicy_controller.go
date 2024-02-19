// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
)

const (
	OperatorControllerName string = "operator-policy-controller"
	CatalogSourceReady     string = "READY"
)

var (
	subscriptionGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	}
	operatorGroupGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1",
		Kind:    "OperatorGroup",
	}
	clusterServiceVersionGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
	}
	deploymentGVK = schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	catalogSrcGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "CatalogSource",
	}
	installPlanGVK = schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "InstallPlan",
	}
)

// OperatorPolicyReconciler reconciles a OperatorPolicy object
type OperatorPolicyReconciler struct {
	client.Client
	DynamicWatcher   depclient.DynamicWatcher
	InstanceName     string
	DefaultNamespace string
}

// SetupWithManager sets up the controller with the Manager and will reconcile when the dynamic watcher
// sees that an object is updated
func (r *OperatorPolicyReconciler) SetupWithManager(mgr ctrl.Manager, depEvents *source.Channel) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(OperatorControllerName).
		For(
			&policyv1beta1.OperatorPolicy{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			depEvents,
			&handler.EnqueueRequestForObject{}).
		Complete(r)
}

// blank assignment to verify that OperatorPolicyReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &OperatorPolicyReconciler{}

//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=operatorpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=operatorpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=operatorpolicies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// (user): Modify the Reconcile function to compare the state specified by
// the OperatorPolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *OperatorPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	OpLog := ctrl.LoggerFrom(ctx)
	policy := &policyv1beta1.OperatorPolicy{}
	watcher := opPolIdentifier(req.Namespace, req.Name)

	// Get the applied OperatorPolicy
	err := r.Get(ctx, req.NamespacedName, policy)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			OpLog.Info("Operator policy could not be found")

			err = r.DynamicWatcher.RemoveWatcher(watcher)
			if err != nil {
				OpLog.Error(err, "Error updating dependency watcher. Ignoring the failure.")
			}

			return reconcile.Result{}, nil
		}

		OpLog.Error(err, "Failed to get operator policy")

		return reconcile.Result{}, err
	}

	// Start query batch for caching and watching related objects
	err = r.DynamicWatcher.StartQueryBatch(watcher)
	if err != nil {
		OpLog.Error(err, "Could not start query batch for the watcher")

		return reconcile.Result{}, err
	}

	defer func() {
		err := r.DynamicWatcher.EndQueryBatch(watcher)
		if err != nil {
			OpLog.Error(err, "Could not end query batch for the watcher")
		}
	}()

	// handle the policy
	OpLog.Info("Reconciling OperatorPolicy")

	desiredSub, desiredOG, err := r.buildResources(ctx, policy)
	if err != nil {
		OpLog.Error(err, "Error building desired resources")

		return reconcile.Result{}, err
	}

	if err := r.handleOpGroup(ctx, policy, desiredOG); err != nil {
		OpLog.Error(err, "Error handling OperatorGroup")

		return reconcile.Result{}, err
	}

	subscription, err := r.handleSubscription(ctx, policy, desiredSub)
	if err != nil {
		OpLog.Error(err, "Error handling Subscription")

		return reconcile.Result{}, err
	}

	if err := r.handleInstallPlan(ctx, policy, subscription); err != nil {
		OpLog.Error(err, "Error handling InstallPlan")

		return reconcile.Result{}, err
	}

	csv, err := r.handleCSV(ctx, policy, subscription)
	if err != nil {
		OpLog.Error(err, "Error handling CSVs")

		return reconcile.Result{}, err
	}

	if err := r.handleDeployment(ctx, policy, csv); err != nil {
		OpLog.Error(err, "Error handling Deployments")

		return reconcile.Result{}, err
	}

	if err := r.handleCatalogSource(ctx, policy, subscription); err != nil {
		OpLog.Error(err, "Error handling CatalogSource")

		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// buildResources builds desired states for the Subscription and OperatorGroup, and
// checks if the policy's spec is valid. It returns an error if it couldn't update the
// validation condition in the policy's status.
func (r *OperatorPolicyReconciler) buildResources(ctx context.Context, policy *policyv1beta1.OperatorPolicy) (
	*operatorv1alpha1.Subscription, *operatorv1.OperatorGroup, error,
) {
	validationErrors := make([]error, 0)

	sub, subErr := buildSubscription(policy, r.DefaultNamespace)
	if subErr != nil {
		validationErrors = append(validationErrors, subErr)
	}

	opGroupNS := r.DefaultNamespace
	if sub != nil && sub.Namespace != "" {
		opGroupNS = sub.Namespace
	}

	opGroup, ogErr := buildOperatorGroup(policy, opGroupNS)
	if ogErr != nil {
		validationErrors = append(validationErrors, ogErr)
	}

	return sub, opGroup, r.updateStatus(ctx, policy, validationCond(validationErrors))
}

// buildSubscription bootstraps the subscription spec defined in the operator policy
// with the apiversion and kind in preparation for resource creation.
// If an error is returned, it will include details on why the policy spec if invalid and
// why the desired subscription can't be determined.
func buildSubscription(
	policy *policyv1beta1.OperatorPolicy, defaultNS string,
) (*operatorv1alpha1.Subscription, error) {
	subscription := new(operatorv1alpha1.Subscription)

	sub := make(map[string]interface{})

	err := json.Unmarshal(policy.Spec.Subscription.Raw, &sub)
	if err != nil {
		return nil, fmt.Errorf("the policy spec.subscription is invalid: %w", err)
	}

	ns, ok := sub["namespace"].(string)
	if !ok {
		if defaultNS == "" {
			return nil, fmt.Errorf("namespace is required in spec.subscription")
		}

		ns = defaultNS
	}

	if validationErrs := validation.IsDNS1123Label(ns); len(validationErrs) != 0 {
		return nil, fmt.Errorf("the namespace '%v' used for the subscription is not a valid namespace identifier", ns)
	}

	spec := new(operatorv1alpha1.SubscriptionSpec)

	err = json.Unmarshal(policy.Spec.Subscription.Raw, spec)
	if err != nil {
		return nil, fmt.Errorf("the policy spec.subscription is invalid: %w", err)
	}

	subscription.SetGroupVersionKind(subscriptionGVK)
	subscription.ObjectMeta.Name = spec.Package
	subscription.ObjectMeta.Namespace = ns
	subscription.Spec = spec

	// If the policy is in `enforce` mode and the allowed CSVs are restricted,
	// the InstallPlanApproval will be set to Manual so that upgrades can be controlled.
	if policy.Spec.RemediationAction.IsEnforce() && len(policy.Spec.Versions) > 0 {
		subscription.Spec.InstallPlanApproval = operatorv1alpha1.ApprovalManual
	}

	return subscription, nil
}

// buildOperatorGroup bootstraps the OperatorGroup spec defined in the operator policy
// with the apiversion and kind in preparation for resource creation
func buildOperatorGroup(
	policy *policyv1beta1.OperatorPolicy, namespace string,
) (*operatorv1.OperatorGroup, error) {
	operatorGroup := new(operatorv1.OperatorGroup)

	operatorGroup.Status.LastUpdated = &metav1.Time{} // without this, some conversions can panic
	operatorGroup.SetGroupVersionKind(operatorGroupGVK)

	// Create a default OperatorGroup if one wasn't specified in the policy
	if policy.Spec.OperatorGroup == nil {
		operatorGroup.ObjectMeta.SetNamespace(namespace)
		operatorGroup.ObjectMeta.SetGenerateName(namespace + "-") // This matches what the console creates
		operatorGroup.Spec.TargetNamespaces = []string{}

		return operatorGroup, nil
	}

	opGroup := make(map[string]interface{})

	if err := json.Unmarshal(policy.Spec.OperatorGroup.Raw, &opGroup); err != nil {
		return nil, fmt.Errorf("the policy spec.operatorGroup is invalid: %w", err)
	}

	if specifiedNS, ok := opGroup["namespace"].(string); ok && specifiedNS != "" {
		if specifiedNS != namespace {
			return nil, fmt.Errorf("the namespace specified in spec.operatorGroup ('%v') must match "+
				"the namespace used for the subscription ('%v')", specifiedNS, namespace)
		}
	}

	name, ok := opGroup["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name is required in spec.operatorGroup")
	}

	spec := new(operatorv1.OperatorGroupSpec)

	if err := json.Unmarshal(policy.Spec.OperatorGroup.Raw, spec); err != nil {
		return nil, fmt.Errorf("the policy spec.operatorGroup is invalid: %w", err)
	}

	operatorGroup.ObjectMeta.SetName(name)
	operatorGroup.ObjectMeta.SetNamespace(namespace)
	operatorGroup.Spec = *spec

	return operatorGroup, nil
}

func (r *OperatorPolicyReconciler) handleOpGroup(
	ctx context.Context, policy *policyv1beta1.OperatorPolicy, desiredOpGroup *operatorv1.OperatorGroup,
) error {
	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	if desiredOpGroup == nil {
		// Note: existing related objects will not be removed by this status update
		err := r.updateStatus(ctx, policy, invalidCausingUnknownCond("OperatorGroup"))
		if err != nil {
			return fmt.Errorf("error updating the status when the OperatorGroup could not be determined: %w", err)
		}
	}

	foundOpGroups, err := r.DynamicWatcher.List(
		watcher, operatorGroupGVK, desiredOpGroup.Namespace, labels.Everything())
	if err != nil {
		return fmt.Errorf("error listing OperatorGroups: %w", err)
	}

	switch len(foundOpGroups) {
	case 0:
		// Missing OperatorGroup: report NonCompliance
		err := r.updateStatus(ctx, policy, missingWantedCond("OperatorGroup"), missingWantedObj(desiredOpGroup))
		if err != nil {
			return fmt.Errorf("error updating the status for a missing OperatorGroup: %w", err)
		}

		if policy.Spec.RemediationAction.IsEnforce() {
			err = r.Create(ctx, desiredOpGroup)
			if err != nil {
				return fmt.Errorf("error creating the OperatorGroup: %w", err)
			}

			desiredOpGroup.SetGroupVersionKind(operatorGroupGVK) // Create stripped this information

			// Now the OperatorGroup should match, so report Compliance
			err = r.updateStatus(ctx, policy, createdCond("OperatorGroup"), createdObj(desiredOpGroup))
			if err != nil {
				return fmt.Errorf("error updating the status for a created OperatorGroup: %w", err)
			}
		}
	case 1:
		opGroup := foundOpGroups[0]

		// Check if what's on the cluster matches what the policy wants (whether it's specified or not)

		emptyNameMatch := desiredOpGroup.Name == "" && opGroup.GetGenerateName() == desiredOpGroup.GenerateName

		if !(opGroup.GetName() == desiredOpGroup.Name || emptyNameMatch) {
			if policy.Spec.OperatorGroup == nil {
				// The policy doesn't specify what the OperatorGroup should look like, but what is already
				// there is not the default one the policy would create.
				// FUTURE: check if the one operator group is compatible with the desired subscription.
				// For an initial implementation, assume if an OperatorGroup already exists, then it's a good one.
				err := r.updateStatus(ctx, policy, opGroupPreexistingCond, matchedObj(&opGroup))
				if err != nil {
					return fmt.Errorf("error updating the status for a pre-existing OperatorGroup: %w", err)
				}

				return nil
			}

			// There is an OperatorGroup in the namespace that does not match the name of what is in the policy.
			// Just creating a new one would cause the "TooManyOperatorGroups" failure.
			// So, just report a NonCompliant status.
			missing := missingWantedObj(desiredOpGroup)
			badExisting := mismatchedObj(&opGroup)

			err := r.updateStatus(ctx, policy, mismatchCond("OperatorGroup"), missing, badExisting)
			if err != nil {
				return fmt.Errorf("error updating the status for an OperatorGroup with the wrong name: %w", err)
			}

			return nil
		}

		// check whether the specs match
		desiredUnstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desiredOpGroup)
		if err != nil {
			return fmt.Errorf("error converting desired OperatorGroup to an Unstructured: %w", err)
		}

		merged := opGroup.DeepCopy() // Copy it so that the value in the cache is not changed

		updateNeeded, skipUpdate, err := r.mergeObjects(
			ctx, desiredUnstruct, merged, string(policy.Spec.ComplianceType),
		)
		if err != nil {
			return fmt.Errorf("error checking if the OperatorGroup needs an update: %w", err)
		}

		if !updateNeeded {
			// Everything relevant matches!
			err := r.updateStatus(ctx, policy, matchesCond("OperatorGroup"), matchedObj(&opGroup))
			if err != nil {
				return fmt.Errorf("error updating the status for an OperatorGroup that matches: %w", err)
			}

			return nil
		}

		// Specs don't match.

		if policy.Spec.OperatorGroup == nil {
			// The policy doesn't specify what the OperatorGroup should look like, but what is already
			// there is not the default one the policy would create.
			// FUTURE: check if the one operator group is compatible with the desired subscription.
			// For an initial implementation, assume if an OperatorGroup already exists, then it's a good one.
			err := r.updateStatus(ctx, policy, opGroupPreexistingCond, matchedObj(&opGroup))
			if err != nil {
				return fmt.Errorf("error updating the status for a pre-existing OperatorGroup: %w", err)
			}

			return nil
		}

		if policy.Spec.RemediationAction.IsEnforce() && skipUpdate {
			err = r.updateStatus(ctx, policy, mismatchCondUnfixable("OperatorGroup"), mismatchedObj(&opGroup))
			if err != nil {
				return fmt.Errorf("error updating status for an unenforceable mismatched OperatorGroup: %w", err)
			}

			return nil
		}

		// The names match, but the specs don't: report NonCompliance
		err = r.updateStatus(ctx, policy, mismatchCond("OperatorGroup"), mismatchedObj(&opGroup))
		if err != nil {
			return fmt.Errorf("error updating the status for an OperatorGroup that does not match: %w", err)
		}

		if policy.Spec.RemediationAction.IsEnforce() {
			desiredOpGroup.ResourceVersion = opGroup.GetResourceVersion()

			err := r.Update(ctx, merged)
			if err != nil {
				return fmt.Errorf("error updating the OperatorGroup: %w", err)
			}

			desiredOpGroup.SetGroupVersionKind(operatorGroupGVK) // Update stripped this information

			// It was updated and should match now, so report Compliance
			err = r.updateStatus(ctx, policy, updatedCond("OperatorGroup"), updatedObj(desiredOpGroup))
			if err != nil {
				return fmt.Errorf("error updating the status after updating the OperatorGroup: %w", err)
			}
		}
	default:
		// This situation will always lead to a "TooManyOperatorGroups" failure on the CSV.
		// Consider improving this in the future: perhaps this could suggest one of the OperatorGroups to keep.
		err := r.updateStatus(ctx, policy, opGroupTooManyCond, opGroupTooManyObjs(foundOpGroups)...)
		if err != nil {
			return fmt.Errorf("error updating the status when there are multiple OperatorGroups: %w", err)
		}
	}

	return nil
}

func (r *OperatorPolicyReconciler) handleSubscription(
	ctx context.Context, policy *policyv1beta1.OperatorPolicy, desiredSub *operatorv1alpha1.Subscription,
) (*operatorv1alpha1.Subscription, error) {
	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	if desiredSub == nil {
		// Note: existing related objects will not be removed by this status update
		err := r.updateStatus(ctx, policy, invalidCausingUnknownCond("Subscription"))
		if err != nil {
			return nil, fmt.Errorf("error updating the status when the Subscription could not be determined: %w", err)
		}

		return nil, nil
	}

	foundSub, err := r.DynamicWatcher.Get(watcher, subscriptionGVK, desiredSub.Namespace, desiredSub.Name)
	if err != nil {
		return nil, fmt.Errorf("error getting the Subscription: %w", err)
	}

	if foundSub == nil {
		// Missing Subscription: report NonCompliance
		err := r.updateStatus(ctx, policy, missingWantedCond("Subscription"), missingWantedObj(desiredSub))
		if err != nil {
			return nil, fmt.Errorf("error updating status for a missing Subscription: %w", err)
		}

		if policy.Spec.RemediationAction.IsEnforce() {
			err := r.Create(ctx, desiredSub)
			if err != nil {
				return nil, fmt.Errorf("error creating the Subscription: %w", err)
			}

			desiredSub.SetGroupVersionKind(subscriptionGVK) // Create stripped this information

			// Now it should match, so report Compliance
			err = r.updateStatus(ctx, policy, createdCond("Subscription"), createdObj(desiredSub))
			if err != nil {
				return nil, fmt.Errorf("error updating the status for a created Subscription: %w", err)
			}
		}

		return desiredSub, nil
	}

	// Subscription found; check if specs match
	desiredUnstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desiredSub)
	if err != nil {
		return nil, fmt.Errorf("error converting desired Subscription to an Unstructured: %w", err)
	}

	merged := foundSub.DeepCopy() // Copy it so that the value in the cache is not changed

	updateNeeded, skipUpdate, err := r.mergeObjects(ctx, desiredUnstruct, merged, string(policy.Spec.ComplianceType))
	if err != nil {
		return nil, fmt.Errorf("error checking if the Subscription needs an update: %w", err)
	}

	mergedSub := new(operatorv1alpha1.Subscription)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(merged.Object, mergedSub); err != nil {
		return nil, fmt.Errorf("error converting the retrieved Subscription to the go type: %w", err)
	}

	if !updateNeeded {
		subResFailed := mergedSub.Status.GetCondition(operatorv1alpha1.SubscriptionResolutionFailed)

		if subResFailed.Status == corev1.ConditionTrue {
			cond := metav1.Condition{
				Type:    subConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  subResFailed.Reason,
				Message: subResFailed.Message,
			}

			if subResFailed.LastTransitionTime != nil {
				cond.LastTransitionTime = *subResFailed.LastTransitionTime
			}

			err := r.updateStatus(ctx, policy, cond, nonCompObj(foundSub, subResFailed.Reason))
			if err != nil {
				return nil, fmt.Errorf("error setting the ResolutionFailed status for a Subscription: %w", err)
			}

			return mergedSub, nil
		}

		err := r.updateStatus(ctx, policy, matchesCond("Subscription"), matchedObj(foundSub))
		if err != nil {
			return nil, fmt.Errorf("error updating the status for a Subscription that matches: %w", err)
		}

		return mergedSub, nil
	}

	// Specs don't match.
	if policy.Spec.RemediationAction.IsEnforce() && skipUpdate {
		err = r.updateStatus(ctx, policy, mismatchCondUnfixable("Subscription"), mismatchedObj(foundSub))
		if err != nil {
			return nil, fmt.Errorf(
				"error updating status for a mismatched Subscription that can't be enforced: %w", err)
		}

		return mergedSub, nil
	}

	err = r.updateStatus(ctx, policy, mismatchCond("Subscription"), mismatchedObj(foundSub))
	if err != nil {
		return nil, fmt.Errorf("error updating status for a mismatched Subscription: %w", err)
	}

	if policy.Spec.RemediationAction.IsEnforce() {
		err := r.Update(ctx, merged)
		if err != nil {
			return nil, fmt.Errorf("error updating the Subscription: %w", err)
		}

		merged.SetGroupVersionKind(subscriptionGVK) // Update stripped this information

		err = r.updateStatus(ctx, policy, updatedCond("Subscription"), updatedObj(merged))
		if err != nil {
			return nil, fmt.Errorf("error updating status after updating the Subscription: %w", err)
		}
	}

	return mergedSub, nil
}

func (r *OperatorPolicyReconciler) handleInstallPlan(
	ctx context.Context, policy *policyv1beta1.OperatorPolicy, sub *operatorv1alpha1.Subscription,
) error {
	if sub == nil {
		// Note: existing related objects will not be removed by this status update
		err := r.updateStatus(ctx, policy, invalidCausingUnknownCond("InstallPlan"))
		if err != nil {
			return fmt.Errorf("error updating the status when the InstallPlan could not be determined: %w", err)
		}

		return nil
	}

	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	foundInstallPlans, err := r.DynamicWatcher.List(
		watcher, installPlanGVK, sub.Namespace, labels.Everything())
	if err != nil {
		return fmt.Errorf("error listing InstallPlans: %w", err)
	}

	ownedInstallPlans := make([]unstructured.Unstructured, 0, len(foundInstallPlans))

	for _, installPlan := range foundInstallPlans {
		for _, owner := range installPlan.GetOwnerReferences() {
			match := owner.Name == sub.Name &&
				owner.Kind == subscriptionGVK.Kind &&
				owner.APIVersion == subscriptionGVK.GroupVersion().String()
			if match {
				ownedInstallPlans = append(ownedInstallPlans, installPlan)

				break
			}
		}
	}

	// InstallPlans are generally kept in order to provide a history of actions on the cluster, but
	// they can be deleted without impacting the installed operator. So, not finding any should not
	// be considered a reason for NonCompliance.
	if len(ownedInstallPlans) == 0 {
		err := r.updateStatus(ctx, policy, noInstallPlansCond, noInstallPlansObj(sub.Namespace))
		if err != nil {
			return fmt.Errorf("error updating status when no relevant InstallPlans were found: %w", err)
		}

		return nil
	}

	OpLog := ctrl.LoggerFrom(ctx)
	relatedInstallPlans := make([]policyv1.RelatedObject, len(ownedInstallPlans))
	ipsRequiringApproval := make([]unstructured.Unstructured, 0)
	anyInstalling := false
	currentPlanFailed := false

	// Construct the relevant relatedObjects, and collect any that might be considered for approval
	for i, installPlan := range ownedInstallPlans {
		phase, ok, err := unstructured.NestedString(installPlan.Object, "status", "phase")
		if !ok && err == nil {
			err = errors.New("the phase of the InstallPlan was not found")
		}

		if err != nil {
			OpLog.Error(err, "Unable to determine the phase of the related InstallPlan",
				"InstallPlan.Name", installPlan.GetName())

			// The InstallPlan will be added as unknown
			phase = ""
		}

		// consider some special phases
		switch phase {
		case string(operatorv1alpha1.InstallPlanPhaseRequiresApproval):
			ipsRequiringApproval = append(ipsRequiringApproval, installPlan)
		case string(operatorv1alpha1.InstallPlanPhaseInstalling):
			anyInstalling = true
		case string(operatorv1alpha1.InstallPlanFailed):
			// Generally, a failed InstallPlan is not a reason for NonCompliance, because it could be from
			// an old installation. But if the current InstallPlan is failed, we should alert the user.
			if sub.Status.InstallPlanRef != nil && sub.Status.InstallPlanRef.Name == installPlan.GetName() {
				currentPlanFailed = true
			}
		}

		relatedInstallPlans[i] = existingInstallPlanObj(&ownedInstallPlans[i], phase)
	}

	if currentPlanFailed {
		err := r.updateStatus(ctx, policy, installPlanFailed, relatedInstallPlans...)
		if err != nil {
			return fmt.Errorf("error updating status when the current InstallPlan has failed: %w", err)
		}

		return nil
	}

	if anyInstalling {
		err := r.updateStatus(ctx, policy, installPlanInstallingCond, relatedInstallPlans...)
		if err != nil {
			return fmt.Errorf("error updating status when an installing InstallPlan was found: %w", err)
		}

		return nil
	}

	if len(ipsRequiringApproval) == 0 {
		err := r.updateStatus(ctx, policy, installPlansNoApprovals, relatedInstallPlans...)
		if err != nil {
			return fmt.Errorf("error updating status when InstallPlans were fine: %w", err)
		}

		return nil
	}

	allUpgradeVersions := make([]string, len(ipsRequiringApproval))

	for i, installPlan := range ipsRequiringApproval {
		csvNames, ok, err := unstructured.NestedStringSlice(installPlan.Object,
			"spec", "clusterServiceVersionNames")
		if !ok && err == nil {
			err = errors.New("the clusterServiceVersionNames field of the InstallPlan was not found")
		}

		if err != nil {
			OpLog.Error(err, "Unable to determine the csv names of the related InstallPlan",
				"InstallPlan.Name", installPlan.GetName())

			csvNames = []string{"unknown"}
		}

		allUpgradeVersions[i] = fmt.Sprintf("%v", csvNames)
	}

	// Only report this status in `inform` mode, because otherwise it could easily oscillate between this and
	// another condition below when being enforced.
	if policy.Spec.RemediationAction.IsInform() {
		// FUTURE: check policy.spec.statusConfig.upgradesAvailable to determine `compliant`.
		// For now this condition assumes it is set to 'NonCompliant'
		err := r.updateStatus(ctx, policy, installPlanUpgradeCond(allUpgradeVersions, nil), relatedInstallPlans...)
		if err != nil {
			return fmt.Errorf("error updating status when an InstallPlan requiring approval was found: %w", err)
		}

		return nil
	}

	approvedVersion := "" // this will only be accurate when there is only one approvable InstallPlan
	approvableInstallPlans := make([]unstructured.Unstructured, 0)

	for _, installPlan := range ipsRequiringApproval {
		ipCSVs, ok, err := unstructured.NestedStringSlice(installPlan.Object,
			"spec", "clusterServiceVersionNames")
		if !ok && err == nil {
			err = errors.New("the clusterServiceVersionNames field of the InstallPlan was not found")
		}

		if err != nil {
			OpLog.Error(err, "Unable to determine the csv names of the related InstallPlan",
				"InstallPlan.Name", installPlan.GetName())

			continue
		}

		if len(ipCSVs) != 1 {
			continue // Don't automate approving any InstallPlans for multiple CSVs
		}

		matchingCSV := len(policy.Spec.Versions) == 0 // true if `spec.versions` is not specified

		for _, acceptableCSV := range policy.Spec.Versions {
			if string(acceptableCSV) == ipCSVs[0] {
				matchingCSV = true

				break
			}
		}

		if matchingCSV {
			approvedVersion = ipCSVs[0]

			approvableInstallPlans = append(approvableInstallPlans, installPlan)
		}
	}

	if len(approvableInstallPlans) != 1 {
		err := r.updateStatus(ctx, policy,
			installPlanUpgradeCond(allUpgradeVersions, approvableInstallPlans), relatedInstallPlans...)
		if err != nil {
			return fmt.Errorf("error updating status when an InstallPlan can't be automatically approved: %w", err)
		}

		return nil
	}

	if err := unstructured.SetNestedField(approvableInstallPlans[0].Object, true, "spec", "approved"); err != nil {
		return fmt.Errorf("error approving InstallPlan: %w", err)
	}

	if err := r.Update(ctx, &approvableInstallPlans[0]); err != nil {
		return fmt.Errorf("error updating approved InstallPlan: %w", err)
	}

	err = r.updateStatus(ctx, policy, installPlanApprovedCond(approvedVersion), relatedInstallPlans...)
	if err != nil {
		return fmt.Errorf("error updating status after approving an InstallPlan: %w", err)
	}

	return nil
}

func (r *OperatorPolicyReconciler) handleCSV(ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	sub *operatorv1alpha1.Subscription,
) (*operatorv1alpha1.ClusterServiceVersion, error) {
	// case where subscription is nil
	if sub == nil {
		// need to report lack of existing CSV
		err := r.updateStatus(ctx, policy, noCSVCond, noExistingCSVObj)
		if err != nil {
			return nil, fmt.Errorf("error updating the status for Deployments: %w", err)
		}

		return nil, err
	}

	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	// case where subscription status has not been populated yet
	if sub.Status.InstalledCSV == "" {
		err := r.updateStatus(ctx, policy, noCSVCond, noExistingCSVObj)
		if err != nil {
			return nil, fmt.Errorf("error updating the status for Deployments: %w", err)
		}

		return nil, err
	}

	// Get the CSV related to the object
	foundCSV, err := r.DynamicWatcher.Get(watcher, clusterServiceVersionGVK, sub.Namespace,
		sub.Status.InstalledCSV)
	if err != nil {
		return nil, err
	}

	// CSV has not yet been created by OLM
	if foundCSV == nil {
		err := r.updateStatus(ctx, policy,
			missingWantedCond("ClusterServiceVersion"), missingCSVObj(sub.Name, sub.Namespace))
		if err != nil {
			return nil, fmt.Errorf("error updating the status for a missing ClusterServiceVersion: %w", err)
		}

		return nil, err
	}

	// Check CSV most recent condition
	unstructured := foundCSV.UnstructuredContent()
	var csv operatorv1alpha1.ClusterServiceVersion

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &csv)
	if err != nil {
		return nil, err
	}

	err = r.updateStatus(ctx, policy, buildCSVCond(&csv), existingCSVObj(&csv))
	if err != nil {
		return &csv, fmt.Errorf("error updating the status for an existing ClusterServiceVersion: %w", err)
	}

	return &csv, nil
}

func (r *OperatorPolicyReconciler) handleDeployment(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	csv *operatorv1alpha1.ClusterServiceVersion,
) error {
	// case where csv is nil
	if csv == nil {
		// need to report lack of existing Deployments
		err := r.updateStatus(ctx, policy, noDeploymentsCond, noExistingDeploymentObj)
		if err != nil {
			return fmt.Errorf("error updating the status for Deployments: %w", err)
		}

		return err
	}

	OpLog := ctrl.LoggerFrom(ctx)

	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	var relatedObjects []policyv1.RelatedObject
	var unavailableDeployments []appsv1.Deployment

	depNum := 0

	for _, dep := range csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		foundDep, err := r.DynamicWatcher.Get(watcher, deploymentGVK, csv.Namespace, dep.Name)
		if err != nil {
			return fmt.Errorf("error getting the Deployment: %w", err)
		}

		// report missing deployment in relatedObjects list
		if foundDep == nil {
			relatedObjects = append(relatedObjects, missingDeploymentObj(dep.Name, csv.Namespace))

			continue
		}

		unstructured := foundDep.UnstructuredContent()
		var dep appsv1.Deployment

		err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructured, &dep)
		if err != nil {
			OpLog.Error(err, "Unable to convert unstructured Deployment to typed", "Deployment.Name", dep.Name)

			continue
		}

		// check for unavailable deployments and build relatedObjects list
		if dep.Status.UnavailableReplicas > 0 {
			unavailableDeployments = append(unavailableDeployments, dep)
		}

		depNum++

		relatedObjects = append(relatedObjects, existingDeploymentObj(&dep))
	}

	err := r.updateStatus(ctx, policy,
		buildDeploymentCond(depNum > 0, unavailableDeployments), relatedObjects...)
	if err != nil {
		return fmt.Errorf("error updating the status for Deployments: %w", err)
	}

	return nil
}

func (r *OperatorPolicyReconciler) handleCatalogSource(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	subscription *operatorv1alpha1.Subscription,
) error {
	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	if subscription == nil {
		// Note: existing related objects will not be removed by this status update
		err := r.updateStatus(ctx, policy, invalidCausingUnknownCond("CatalogSource"))
		if err != nil {
			return fmt.Errorf("error updating the status when the  could not be determined: %w", err)
		}

		return nil
	}

	catalogName := subscription.Spec.CatalogSource
	catalogNS := subscription.Spec.CatalogSourceNamespace

	// Check if CatalogSource exists
	foundCatalogSrc, err := r.DynamicWatcher.Get(watcher, catalogSrcGVK,
		catalogNS, catalogName)
	if err != nil {
		return fmt.Errorf("error getting CatalogSource: %w", err)
	}

	isMissing := foundCatalogSrc == nil
	isUnhealthy := isMissing

	if !isMissing {
		// CatalogSource is found, initiate health check
		catalogSrcUnstruct := foundCatalogSrc.DeepCopy()
		catalogSrc := new(operatorv1alpha1.CatalogSource)

		err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(catalogSrcUnstruct.Object, catalogSrc)
		if err != nil {
			return fmt.Errorf("error converting the retrieved CatalogSource to the Go type: %w", err)
		}

		if catalogSrc.Status.GRPCConnectionState == nil {
			// Unknown State
			err := r.updateStatus(ctx, policy, catalogSourceUnknownCond, catalogSrcUnknownObj(catalogName, catalogNS))
			if err != nil {
				return fmt.Errorf("error retrieving the status for a CatalogSource: %w", err)
			}

			return nil
		}

		CatalogSrcState := catalogSrc.Status.GRPCConnectionState.LastObservedState
		isUnhealthy = (CatalogSrcState != CatalogSourceReady)
	}

	err = r.updateStatus(ctx, policy, catalogSourceFindCond(isUnhealthy, isMissing),
		catalogSourceObj(catalogName, catalogNS, isUnhealthy, isMissing))
	if err != nil {
		return fmt.Errorf("error updating the status for a CatalogSource: %w", err)
	}

	return nil
}

func opPolIdentifier(namespace, name string) depclient.ObjectIdentifier {
	return depclient.ObjectIdentifier{
		Group:     policyv1beta1.GroupVersion.Group,
		Version:   policyv1beta1.GroupVersion.Version,
		Kind:      "OperatorPolicy",
		Namespace: namespace,
		Name:      name,
	}
}

// mergeObjects takes fields from the desired object and sets/merges them on the
// existing object. It checks and returns whether an update is really necessary
// with a server-side dry-run.
func (r *OperatorPolicyReconciler) mergeObjects(
	ctx context.Context,
	desired map[string]interface{},
	existing *unstructured.Unstructured,
	complianceType string,
) (updateNeeded, updateIsForbidden bool, err error) {
	desiredObj := unstructured.Unstructured{Object: desired}

	// Use a copy since some values can be directly assigned to mergedObj in handleSingleKey.
	existingObjectCopy := existing.DeepCopy()
	removeFieldsForComparison(existingObjectCopy)

	_, errMsg, updateNeeded, _ := handleKeys(
		desiredObj, existing, existingObjectCopy, complianceType, "", false,
	)
	if errMsg != "" {
		return updateNeeded, false, errors.New(errMsg)
	}

	if updateNeeded {
		err := r.Update(ctx, existing, client.DryRunAll)
		if err != nil {
			if k8serrors.IsForbidden(err) {
				// This indicates the update would make a change, but the change is not allowed,
				// for example, the changed field might be immutable.
				// The policy should be marked as noncompliant, but an enforcement update would fail.
				return true, true, nil
			}

			return updateNeeded, false, err
		}

		removeFieldsForComparison(existing)

		if reflect.DeepEqual(existing.Object, existingObjectCopy.Object) {
			// The dry run indicates that there is not *really* a mismatch.
			updateNeeded = false
		}
	}

	return updateNeeded, false, nil
}
