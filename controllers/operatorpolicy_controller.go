// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"
	"strings"

	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	defaultOGSuffix        string = "-default-og"
)

var (
	subscriptionGVK  = schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1alpha1", Kind: "Subscription"}
	operatorGroupGVK = schema.GroupVersionKind{Group: "operators.coreos.com", Version: "v1", Kind: "OperatorGroup"}
)

var OpLog = ctrl.Log.WithName(OperatorControllerName)

// OperatorPolicyReconciler reconciles a OperatorPolicy object
type OperatorPolicyReconciler struct {
	client.Client
	DynamicWatcher depclient.DynamicWatcher
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
	policy := &policyv1beta1.OperatorPolicy{}

	watcher := depclient.ObjectIdentifier{
		Group:     operatorv1.GroupVersion.Group,
		Version:   operatorv1.GroupVersion.Version,
		Kind:      "OperatorPolicy",
		Namespace: req.Namespace,
		Name:      req.Name,
	}

	// Get the applied OperatorPolicy
	err := r.Get(ctx, req.NamespacedName, policy)
	if err != nil {
		if errors.IsNotFound(err) {
			OpLog.Info("Operator policy could not be found", "name", req.Name, "namespace", req.Namespace)

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
		OpLog.Error(err, "Could not start query batch for the watcher", "watcher", watcher.Name,
			"watcherKind", watcher.Kind)

		return reconcile.Result{}, err
	}

	defer func() {
		err := r.DynamicWatcher.EndQueryBatch(watcher)
		if err != nil {
			OpLog.Error(err, "Could not end query batch for the watcher", "watcher", watcher.Name,
				"watcherKind", watcher.Kind)
		}
	}()

	// handle the policy
	OpLog.Info("Reconciling OperatorPolicy", "policy", policy.Name)

	// Check if specified Subscription exists
	cachedSubscription, err := r.DynamicWatcher.Get(watcher, subscriptionGVK, policy.Spec.Subscription.Namespace,
		policy.Spec.Subscription.SubscriptionSpec.Package)
	if err != nil {
		OpLog.Error(err, "Could not get subscription", "kind", subscriptionGVK.Kind,
			"name", policy.Spec.Subscription.SubscriptionSpec.Package,
			"namespace", policy.Spec.Subscription.Namespace)

		return reconcile.Result{}, err
	}

	subExists := cachedSubscription != nil

	// Check if any OperatorGroups exist in the namespace
	ogNamespace := policy.Spec.Subscription.Namespace
	if policy.Spec.OperatorGroup != nil {
		ogNamespace = policy.Spec.OperatorGroup.Namespace
	}

	cachedOperatorGroups, err := r.DynamicWatcher.List(watcher, operatorGroupGVK, ogNamespace, nil)
	if err != nil {
		OpLog.Error(err, "Could not list operator group", "kind", operatorGroupGVK.Kind,
			"namespace", ogNamespace)

		return reconcile.Result{}, err
	}

	ogExists := len(cachedOperatorGroups) != 0

	// Exists indicates if a Subscription or Operatorgroup need to be created
	exists := subExists && ogExists
	shouldExist := strings.EqualFold(string(policy.Spec.ComplianceType), string(policyv1.MustHave))

	// Case 1: policy has just been applied and related resources have yet to be created
	if !exists && shouldExist {
		OpLog.Info("The object does not exist but should exist")

		return r.createPolicyResources(ctx, policy, cachedSubscription, cachedOperatorGroups)
	}

	// Case 2: Resources exist, but should not exist (i.e. mustnothave or deletion)
	if exists && !shouldExist {
		// Future implementation: clean up resources and delete watches if mustnothave, otherwise inform
		OpLog.Info("The object exists but should not exist")
	}

	// Case 3: Resources do not exist, and should not exist
	if !exists && !shouldExist {
		// Future implementation: Possibly emit a success event
		OpLog.Info("The object does not exist and is compliant with the mustnothave compliance type")
	}

	// Case 4: Resources exist, and should exist (i.e. update)
	if exists && shouldExist {
		// Future implementation: Verify the specs of the object matches the one on the cluster
		OpLog.Info("The object already exists. Checking fields to verify matching specs")
	}

	return reconcile.Result{}, nil
}

// createPolicyResources encapsulates the logic for creating resources specified within an operator policy.
// This should normally happen when the policy is initially applied to the cluster. Creates an OperatorGroup
// if specified, otherwise defaults to using an allnamespaces OperatorGroup.
func (r *OperatorPolicyReconciler) createPolicyResources(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	cachedSubscription *unstructured.Unstructured,
	cachedOGList []unstructured.Unstructured,
) (ctrl.Result, error) {
	//  Create og, then trigger reconcile
	if len(cachedOGList) == 0 {
		if strings.EqualFold(string(policy.Spec.RemediationAction), string(policyv1.Enforce)) {
			ogSpec := buildOperatorGroup(policy)

			err := r.Create(ctx, ogSpec)
			if err != nil {
				OpLog.Error(err, "Error while creating OperatorGroup")
				r.setCompliance(ctx, policy, policyv1.NonCompliant)

				return reconcile.Result{}, err
			}

			// Created successfully, requeue result
			r.setCompliance(ctx, policy, policyv1.NonCompliant)

			return reconcile.Result{Requeue: true}, err
		} else if policy.Spec.OperatorGroup != nil {
			// If inform mode, keep going and return at the end without requeue. Before
			// continuing, should check if required og spec is created to set compliance state

			// Set to non compliant because operatorgroup does not exist on cluster, but
			// should still go to check subscription
			r.setCompliance(ctx, policy, policyv1.NonCompliant)
		}
	}

	// Create new subscription
	if cachedSubscription == nil {
		if strings.EqualFold(string(policy.Spec.RemediationAction), string(policyv1.Enforce)) {
			subscriptionSpec := buildSubscription(policy)

			err := r.Create(ctx, subscriptionSpec)
			if err != nil {
				OpLog.Error(err, "Could not handle missing musthave object")
				r.setCompliance(ctx, policy, policyv1.NonCompliant)

				return reconcile.Result{}, err
			}

			// Future work: Check availability/status of all resources
			// managed by the OperatorPolicy before setting compliance state
			r.setCompliance(ctx, policy, policyv1.Compliant)

			return reconcile.Result{}, nil
		}
		// inform
		r.setCompliance(ctx, policy, policyv1.NonCompliant)
	}

	// Will only reach this if in inform mode
	return reconcile.Result{}, nil
}

// updatePolicyStatus updates the status of the operatorPolicy.
// In the future, a condition should be added as well, and this should generate events.
func (r *OperatorPolicyReconciler) updatePolicyStatus(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
) error {
	updatedStatus := policy.Status

	err := r.Get(ctx, types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}, policy)
	if err != nil {
		OpLog.Info(fmt.Sprintf("Failed to refresh policy; using previously fetched version: %s", err))
	} else {
		policy.Status = updatedStatus
	}

	err = r.Status().Update(ctx, policy)
	if err != nil {
		OpLog.Info(fmt.Sprintf("Failed to update policy status: %s", err))

		return err
	}

	return nil
}

// buildSubscription bootstraps the subscription spec defined in the operator policy
// with the apiversion and kind in preparation for resource creation
func buildSubscription(
	policy *policyv1beta1.OperatorPolicy,
) *operatorv1alpha1.Subscription {
	subscription := new(operatorv1alpha1.Subscription)

	subscription.SetGroupVersionKind(subscriptionGVK)
	subscription.ObjectMeta.Name = policy.Spec.Subscription.Package
	subscription.ObjectMeta.Namespace = policy.Spec.Subscription.Namespace
	subscription.Spec = policy.Spec.Subscription.SubscriptionSpec.DeepCopy()

	OpLog.Info("Creating the subscription", "kind", subscription.Kind,
		"namespace", subscription.Namespace)

	return subscription
}

// Sets the compliance of the policy
func (r *OperatorPolicyReconciler) setCompliance(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	compliance policyv1.ComplianceState,
) {
	policy.Status.ComplianceState = compliance

	err := r.updatePolicyStatus(ctx, policy)
	if err != nil {
		OpLog.Error(err, "error while updating policy status")
	}
}

// buildOperatorGroup bootstraps the OperatorGroup spec defined in the operator policy
// with the apiversion and kind in preparation for resource creation
func buildOperatorGroup(
	policy *policyv1beta1.OperatorPolicy,
) *operatorv1.OperatorGroup {
	operatorGroup := new(operatorv1.OperatorGroup)

	// Create a default OperatorGroup if one wasn't specified in the policy
	// Future work: Prevent creating multiple OperatorGroups in the same ns
	if policy.Spec.OperatorGroup == nil {
		operatorGroup.SetGroupVersionKind(operatorGroupGVK)
		operatorGroup.ObjectMeta.SetName(policy.Spec.Subscription.Package + defaultOGSuffix)
		operatorGroup.ObjectMeta.SetNamespace(policy.Spec.Subscription.Namespace)
		operatorGroup.Spec.TargetNamespaces = []string{}
	} else {
		operatorGroup.SetGroupVersionKind(operatorGroupGVK)
		operatorGroup.ObjectMeta.SetName(policy.Spec.OperatorGroup.Name)
		operatorGroup.ObjectMeta.SetNamespace(policy.Spec.OperatorGroup.Namespace)
		operatorGroup.Spec.TargetNamespaces = policy.Spec.OperatorGroup.Target.Namespace
	}

	OpLog.Info("Creating the operator group", "kind", operatorGroup.Kind,
		"namespace", operatorGroup.Namespace)

	return operatorGroup
}
