// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
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
// Currently, this just means setting the status to Compliant and logging the name
// of the operatorPolicy.
//
// In the future, more reconciliation logic will be added. For reference:
// https://github.com/JustinKuli/ocm-enhancements/blob/89-operator-policy/enhancements/sig-policy/89-operator-policy-kind/README.md
func (r *OperatorPolicyReconciler) handleSinglePolicy(
	policy *policyv1beta1.OperatorPolicy,
) error {
	policy.Status.ComplianceState = policyv1.Compliant

	err := r.updatePolicyStatus(policy)
	if err != nil {
		OpLog.Info("error while updating policy status")

		return err
	}

	OpLog.Info("Logging operator policy", "policy", policy.Name)

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
