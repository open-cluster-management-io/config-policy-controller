// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
)

const (
	OperatorControllerName string = "operator-policy-controller"
)

var OpLog = ctrl.Log.WithName(OperatorControllerName)

// OperatorPolicyReconciler reconciles a OperatorPolicy object
type OperatorPolicyReconciler struct {
	client.Client
}

// SetupWithManager sets up the controller with the Manager.
func (r *OperatorPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(OperatorControllerName).
		For(&policyv1beta1.OperatorPolicy{}).
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

	err := r.Get(ctx, req.NamespacedName, policy)
	if err != nil {
		if errors.IsNotFound(err) {
			OpLog.Info("Operator policy could not be found")

			return reconcile.Result{}, nil
		}
	}

	return reconcile.Result{}, nil
}

// PeriodicallyExecOperatorPolicies loops through all operatorpolicies in the target namespace and triggers
// template handling for each one. This function drives all the work the operator policy controller does.
func (r *OperatorPolicyReconciler) PeriodicallyExecOperatorPolicies(freq uint, elected <-chan struct{},
) {
	OpLog.Info("Waiting for leader election before periodically evaluating operator policies")
	<-elected

	exiting := false
	for !exiting {
		start := time.Now()

		var skipLoop bool
		if !skipLoop {
			policiesList := policyv1beta1.OperatorPolicyList{}

			err := r.List(context.TODO(), &policiesList)
			if err != nil {
				OpLog.Error(err, "Failed to list the OperatorPolicy objects to evaluate")
			} else {
				for i := range policiesList.Items {
					policy := policiesList.Items[i]
					if !r.shouldEvaluatePolicy(&policy) {
						continue
					}

					// handle policy
					err := r.handleSinglePolicy(&policy)
					if err != nil {
						OpLog.Error(err, "Error while evaluating operator policy")
					}
				}
			}
		}

		elapsed := time.Since(start).Seconds()
		if float64(freq) > elapsed {
			remainingSleep := float64(freq) - elapsed
			sleepTime := time.Duration(remainingSleep) * time.Second
			OpLog.V(2).Info("Sleeping before reprocessing the operator policies", "seconds", sleepTime)
			time.Sleep(sleepTime)
		}
	}
}

// handleSinglePolicy encapsulates the logic for processing a single operatorPolicy.
// Currently, the controller is able to create an OLM Subscription from the operatorPolicy specs,
// create an OperatorGroup in the ns as the Subscription, set the compliance status, and log the policy.
//
// In the future, more reconciliation logic will be added. For reference:
// https://github.com/JustinKuli/ocm-enhancements/blob/89-operator-policy/enhancements/sig-policy/89-operator-policy-kind/README.md
func (r *OperatorPolicyReconciler) handleSinglePolicy(
	policy *policyv1beta1.OperatorPolicy,
) error {
	OpLog.Info("Handling OperatorPolicy", "policy", policy.Name)

	subscriptionSpec := new(operatorv1alpha1.Subscription)
	err := r.Get(context.TODO(),
		types.NamespacedName{Namespace: subscriptionSpec.Namespace, Name: subscriptionSpec.Name},
		subscriptionSpec)
	exists := !errors.IsNotFound(err)
	shouldExist := strings.EqualFold(string(policy.Spec.ComplianceType), string(policyv1.MustHave))

	// Object does not exist but it should exist, create object
	if !exists && shouldExist {
		if strings.EqualFold(string(policy.Spec.RemediationAction), string(policyv1.Enforce)) {
			OpLog.Info("creating kind " + subscriptionSpec.Kind + " in ns " + subscriptionSpec.Namespace)
			subscriptionSpec := buildSubscription(policy, subscriptionSpec)
			err = r.Create(context.TODO(), subscriptionSpec)

			if err != nil {
				r.setCompliance(policy, policyv1.NonCompliant)
				OpLog.Error(err, "Could not handle missing musthave object")

				return err
			}

			// Currently creates an OperatorGroup for every Subscription
			// in the same ns, and defaults to targeting all ns.
			// Future implementations will enable targeting ns based on
			// installModes supported by the CSV. Also, only one OperatorGroup
			// should exist in each ns
			operatorGroup := buildOperatorGroup(policy)
			err = r.Create(context.TODO(), operatorGroup)

			if err != nil {
				r.setCompliance(policy, policyv1.NonCompliant)
				OpLog.Error(err, "Could not handle missing musthave object")

				return err
			}

			r.setCompliance(policy, policyv1.Compliant)

			return nil
		}

		// Inform
		r.setCompliance(policy, policyv1.NonCompliant)

		return nil
	}

	// Object exists but it should not exist, delete object
	// Deleting related objects will be added in the future
	if exists && !shouldExist {
		if strings.EqualFold(string(policy.Spec.RemediationAction), string(policyv1.Enforce)) {
			OpLog.Info("deleting kind " + subscriptionSpec.Kind + " in ns " + subscriptionSpec.Namespace)
			err = r.Delete(context.TODO(), subscriptionSpec)

			if err != nil {
				r.setCompliance(policy, policyv1.NonCompliant)
				OpLog.Error(err, "Could not handle existing musthave object")

				return err
			}

			r.setCompliance(policy, policyv1.Compliant)

			return nil
		}

		// Inform
		r.setCompliance(policy, policyv1.NonCompliant)

		return nil
	}

	// Object does not exist and it should not exist, emit success event
	if !exists && !shouldExist {
		OpLog.Info("The object does not exist and is compliant with the mustnothave compliance type")
		// Future implementation: Possibly emit a success event

		return nil
	}

	// Object exists, now need to validate field to make sure they match
	if exists {
		OpLog.Info("The object already exists. Checking fields to verify matching specs")
		// Future implementation: Verify the specs of the object matches the one on the cluster

		return nil
	}

	return nil
}

// updatePolicyStatus updates the status of the operatorPolicy.
//
// In the future, a condition should be added as well, and this should generate events.
func (r *OperatorPolicyReconciler) updatePolicyStatus(
	policy *policyv1beta1.OperatorPolicy,
) error {
	updatedStatus := policy.Status

	err := r.Get(context.TODO(), types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}, policy)
	if err != nil {
		OpLog.Info(fmt.Sprintf("Failed to refresh policy; using previously fetched version: %s", err))
	} else {
		policy.Status = updatedStatus
	}

	err = r.Status().Update(context.TODO(), policy)
	if err != nil {
		OpLog.Info(fmt.Sprintf("Failed to update policy status: %s", err))

		return err
	}

	return nil
}

// shouldEvaluatePolicy will determine if the policy is ready for evaluation by checking
// for the compliance status. If it is already compliant, then evaluation will be skipped.
// It will be evaluated otherwise.
//
// In the future, other mechanisms for determining evaluation should be considered.
func (r *OperatorPolicyReconciler) shouldEvaluatePolicy(
	policy *policyv1beta1.OperatorPolicy,
) bool {
	if policy.Status.ComplianceState == policyv1.Compliant {
		OpLog.Info(fmt.Sprintf("%s is already compliant, skipping evaluation", policy.Name))

		return false
	}

	return true
}

// buildSubscription bootstraps the subscription spec defined in the operator policy
// with the apiversion and kind in preparation for resource creation
func buildSubscription(
	policy *policyv1beta1.OperatorPolicy,
	subscription *operatorv1alpha1.Subscription,
) *operatorv1alpha1.Subscription {
	gvk := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	}

	subscription.SetGroupVersionKind(gvk)
	subscription.ObjectMeta.Name = policy.Spec.Subscription.Package
	subscription.ObjectMeta.Namespace = policy.Spec.Subscription.Namespace
	subscription.Spec = policy.Spec.Subscription.SubscriptionSpec.DeepCopy()

	return subscription
}

// Sets the compliance of the policy
func (r *OperatorPolicyReconciler) setCompliance(
	policy *policyv1beta1.OperatorPolicy,
	compliance policyv1.ComplianceState,
) {
	policy.Status.ComplianceState = compliance

	err := r.updatePolicyStatus(policy)
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

	gvk := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1",
		Kind:    "OperatorGroup",
	}

	operatorGroup.SetGroupVersionKind(gvk)
	operatorGroup.ObjectMeta.SetName(policy.Spec.Subscription.Package + "-operator-group")
	operatorGroup.ObjectMeta.SetNamespace(policy.Spec.Subscription.Namespace)
	operatorGroup.Spec.TargetNamespaces = []string{"*"}

	return operatorGroup
}
