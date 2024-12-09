// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// SelectorReconciler keeps a cache of NamespaceSelector results, which it should update when
// namespaces are created, deleted, or re-labeled.
type SelectorReconciler interface {
	// Get returns the items matching the given Target for the given object. If there's  no selection for that object,
	// and Target has been calculated, it will be calculated now. Otherwise, a cached value may be used.
	Get(objNS string, objName string, t policyv1.Target) ([]string, error)

	// HasUpdate indicates when the cached selection for this namespace and name has been changed since the last
	// time that Get was called for that name.
	HasUpdate(string, string) bool

	// Stop tells the SelectorReconciler to stop updating the cached selection for the namespace and name.
	Stop(string, string)
}

type NamespaceSelectorReconciler struct {
	client        client.Client
	updateChannel chan<- event.GenericEvent
	selections    map[string]namespaceSelection
	lock          sync.RWMutex
}

func NewNamespaceSelectorReconciler(
	k8sClient client.Client, updateChannel chan<- event.GenericEvent,
) NamespaceSelectorReconciler {
	return NamespaceSelectorReconciler{
		client:        k8sClient,
		updateChannel: updateChannel,
		selections:    make(map[string]namespaceSelection),
	}
}

type namespaceSelection struct {
	target     policyv1.Target
	namespaces []string
	hasUpdate  bool
	err        error
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceSelectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	neverEnqueue := predicate.NewPredicateFuncs(func(o client.Object) bool { return false })

	// Instead of reconciling for each Namespace, just reconcile once
	// - that reconcile will do a list on all the Namespaces anyway.
	mapToSingleton := func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Name: "NamespaceSelector",
		}}}
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("NamespaceSelector").
		For( // This is a workaround because a `For` is required, but doesn't allow the enqueueing to be customized
			&corev1.Namespace{},
			builder.WithPredicates(neverEnqueue)).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(mapToSingleton),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Complete(r)
}

// Reconcile runs whenever a namespace on the target cluster is created, deleted, or has a change in
// labels. It updates the cached selections for NamespaceSelectors that it knows about.
func (r *NamespaceSelectorReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := logf.Log.WithValues("Reconciler", "NamespaceSelector")
	oldSelections := make(map[string]namespaceSelection)

	r.lock.RLock()

	for name, selection := range r.selections {
		oldSelections[name] = selection
	}

	r.lock.RUnlock()

	// No selections to populate, just skip.
	if len(r.selections) == 0 {
		return ctrl.Result{}, nil
	}

	namespaces := corev1.NamespaceList{}

	// This List will be from the cache
	if err := r.client.List(ctx, &namespaces); err != nil {
		log.Error(err, "Unable to list namespaces from the cache")

		return ctrl.Result{}, err
	}

	for nsName, oldSelection := range oldSelections {
		policyNs, policyName := splitKey(nsName)

		newNamespaces, err := filter(namespaces, oldSelection.target)
		if err != nil {
			log.Error(err, "Unable to filter namespaces for policy", "namespace", policyNs, "name", policyName)

			r.update(policyNs, policyName, namespaceSelection{
				target:     oldSelection.target,
				namespaces: newNamespaces,
				hasUpdate:  oldSelection.err == nil, // it has an update if the error state changed
				err:        err,
			})

			continue
		}

		if !reflect.DeepEqual(newNamespaces, oldSelection.namespaces) {
			log.V(2).Info(
				"Updating selection from Reconcile",
				"namespace", policyNs,
				"name", policyName,
				"selection", newNamespaces,
			)

			r.update(policyNs, policyName, namespaceSelection{
				target:     oldSelection.target,
				namespaces: newNamespaces,
				hasUpdate:  true,
				err:        nil,
			})
		}
	}

	return ctrl.Result{}, nil
}

// Get returns the items matching the given Target for the given object. If there's no selection for that object,
// and Target has been calculated, it will be calculated now. Otherwise, a cached value may be used.
func (r *NamespaceSelectorReconciler) Get(objNS string, objName string, t policyv1.Target) ([]string, error) {
	log := logf.Log.WithValues("Reconciler", "NamespaceSelector")

	r.lock.Lock()

	key := getKey(objNS, objName)

	// If found, and target has not been changed
	if selection, found := r.selections[key]; found && selection.target.String() == t.String() {
		selection.hasUpdate = false
		r.selections[key] = selection

		r.lock.Unlock()

		return selection.namespaces, selection.err
	}

	// unlock for now, the list filtering could take a non-trivial amount of time
	r.lock.Unlock()

	// Return no namespaces when both include and label selector are empty
	if t.IsEmpty() {
		log.V(2).Info("Updating selection from Reconcile for empty selector",
			"namespace", objNS, "policy", objName)

		r.update(objNS, objName, namespaceSelection{
			target:     t,
			namespaces: []string{},
			hasUpdate:  false,
			err:        nil,
		})

		return []string{}, nil
	}

	// New, or the target has changed.
	nsList := corev1.NamespaceList{}
	// Default to fetching all Namespaces
	listOpts := client.ListOptions{
		LabelSelector: labels.Everything(),
	}

	// Parse the label selector if it's provided
	if t.LabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(t.LabelSelector)
		if err != nil {
			err = fmt.Errorf("error parsing namespace LabelSelector: %w", err)

			log.V(2).Info("Updating selection from Reconcile with parsing error",
				"namespace", objNS, "policy", objName, "error", err)

			r.update(objNS, objName, namespaceSelection{
				target:     t,
				namespaces: []string{},
				hasUpdate:  false,
				err:        err,
			})

			return []string{}, fmt.Errorf("error parsing namespace LabelSelector: %w", err)
		}

		listOpts.LabelSelector = selector
	}

	// Fetch namespaces -- this List will be from the controller-runtime cache
	if err := r.client.List(context.TODO(), &nsList, &listOpts); err != nil {
		log.Error(err, "Unable to list namespaces from the cache")

		return nil, err
	}

	nsToMatch := make([]string, len(nsList.Items))
	for i, ns := range nsList.Items {
		nsToMatch[i] = ns.Name
	}

	selected, err := Matches(nsToMatch, t.Include, t.Exclude)
	slices.Sort(selected)

	log.V(2).Info("Updating selection from Reconcile with matches",
		"namespace", objNS, "policy", objName, "selection", selected, "error", err)

	r.update(objNS, objName, namespaceSelection{
		target:     t,
		namespaces: selected,
		hasUpdate:  false,
		err:        err,
	})

	return selected, err
}

// HasUpdate indicates when the cached selection for this policy has been changed since the last
// time that Get was called for that policy.
func (r *NamespaceSelectorReconciler) HasUpdate(namespace string, name string) bool {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.selections[getKey(namespace, name)].hasUpdate
}

// Stop tells the SelectorReconciler to stop updating the cached selection for the name.
func (r *NamespaceSelectorReconciler) Stop(namespace string, name string) {
	r.lock.Lock()
	defer r.lock.Unlock()

	delete(r.selections, getKey(namespace, name))
}

func getKey(namespace string, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func splitKey(key string) (string, string) {
	parts := strings.SplitN(key, "/", 2)

	if len(parts) != 2 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}

func (r *NamespaceSelectorReconciler) update(namespace string, name string, sel namespaceSelection) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.selections[getKey(namespace, name)] = sel

	if r.updateChannel != nil {
		policy := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": policyv1.GroupVersion.String(),
				"kind":       "ConfigurationPolicy",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
			},
		}

		r.updateChannel <- event.GenericEvent{Object: policy}
	}
}

func filter(allNSList corev1.NamespaceList, t policyv1.Target) ([]string, error) {
	// If MatchLabels and MatchExpressions are nil, the resulting label selector
	// matches all namespaces. This is to guard against that.
	if t.IsEmpty() {
		return []string{}, nil
	}

	// List all namespaces by default, otherwise use provided LabelSelector
	selector := labels.Everything()

	if t.LabelSelector != nil {
		var err error

		selector, err = metav1.LabelSelectorAsSelector(t.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("error parsing namespace LabelSelector: %w", err)
		}
	}

	nsToFilter := make([]string, 0)

	for _, ns := range allNSList.Items {
		if selector.Matches(labels.Set(ns.GetLabels())) {
			nsToFilter = append(nsToFilter, ns.Name)
		}
	}

	namespaces, err := Matches(nsToFilter, t.Include, t.Exclude)
	slices.Sort(namespaces)

	return namespaces, err
}
