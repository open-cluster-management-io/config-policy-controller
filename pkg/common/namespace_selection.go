// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

var log = ctrl.Log

// Parse *Target.MatchLabels and *Target.MatchExpressions into metav1.LabelSelector for the k8s API
func parseToLabelSelector(selector policyv1.Target) metav1.LabelSelector {
	// Build LabelSelector from provided MatchLabels and MatchExpressions
	var labelSelector metav1.LabelSelector

	// Handle when MatchLabels/MatchExpressions were not provided to prevent nil pointer dereference.
	// This is needed so that `include` can function independently. Not fetching any objects is the
	// responsibility of the calling function for when MatchLabels/MatchExpressions are both nil.
	matchLabels := map[string]string{}
	matchExpressions := []metav1.LabelSelectorRequirement{}

	if selector.MatchLabels != nil {
		matchLabels = *selector.MatchLabels
	}

	if selector.MatchExpressions != nil {
		matchExpressions = *selector.MatchExpressions
	}

	labelSelector = metav1.LabelSelector{
		MatchLabels:      matchLabels,
		MatchExpressions: matchExpressions,
	}

	return labelSelector
}

// GetSelectedNamespaces returns the list of filtered namespaces according to the policy namespace selector.
func GetSelectedNamespaces(client kubernetes.Interface, selector policyv1.Target) ([]string, error) {
	// Build LabelSelector from provided MatchLabels and MatchExpressions
	labelSelector := parseToLabelSelector(selector)

	// get all namespaces matching selector
	allNamespaces, err := GetAllNamespaces(client, labelSelector)
	if err != nil {
		log.Error(err, "error retrieving namespaces")

		return []string{}, err
	}

	// filter the list based on the included/excluded list of patterns
	included := selector.Include
	excluded := selector.Exclude
	log.V(2).Info("Filtering namespace list using include/exclude lists", "include", included, "exclude", excluded)

	finalList, err := Matches(allNamespaces, included, excluded)
	if err != nil {
		return []string{}, err
	}

	if len(finalList) == 0 {
		log.V(2).Info("Filtered namespace list is empty.")
	}

	log.V(2).Info("Returning final filtered namespace list", "namespaces", finalList)

	return finalList, err
}

// GetAllNamespaces gets the list of all namespaces from k8s that matches the input label selector.
func GetAllNamespaces(client kubernetes.Interface, labelSelector metav1.LabelSelector) ([]string, error) {
	parsedSelector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return nil, fmt.Errorf("error parsing namespace LabelSelector: %w", err)
	}

	listOpt := metav1.ListOptions{
		LabelSelector: parsedSelector.String(),
	}

	log.V(2).Info("Retrieving namespaces with LabelSelector", "LabelSelector", parsedSelector.String())

	nsList, err := client.CoreV1().Namespaces().List(context.TODO(), listOpt)
	if err != nil {
		log.Error(err, "could not list namespaces from the API server")

		return nil, err
	}

	var namespacesNames []string

	for _, n := range nsList.Items {
		namespacesNames = append(namespacesNames, n.Name)
	}

	return namespacesNames, nil
}

// SelectorReconciler keeps a cache of NamespaceSelector results, which it should update when
// namespaces are created, deleted, or re-labeled.
type SelectorReconciler interface {
	// Get returns the items matching the given Target for the given name. If no selection for that
	// name and Target has been calculated, it will be calculated now. Otherwise, a cached value
	// may be used.
	Get(string, policyv1.Target) ([]string, error)

	// HasUpdate indicates when the cached selection for this name has been changed since the last
	// time that Get was called for that name.
	HasUpdate(string) bool

	// Stop tells the SelectorReconciler to stop updating the cached selection for the name.
	Stop(string)
}

type NamespaceSelectorReconciler struct {
	Client     client.Client
	selections map[string]namespaceSelection
	lock       sync.RWMutex
}

type namespaceSelection struct {
	target     policyv1.Target
	namespaces []string
	hasUpdate  bool
	err        error
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceSelectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.selections = make(map[string]namespaceSelection)

	neverEnqueue := predicate.NewPredicateFuncs(func(o client.Object) bool { return false })

	// Instead of reconciling for each Namespace, just reconcile once
	// - that reconcile will do a list on all the Namespaces anyway.
	mapToSingleton := func(_ client.Object) []reconcile.Request {
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
			&source.Kind{Type: &corev1.Namespace{}},
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
	if err := r.Client.List(ctx, &namespaces); err != nil {
		log.Error(err, "Unable to list namespaces from the cache")

		return ctrl.Result{}, err
	}

	for name, oldSelection := range oldSelections {
		newNamespaces, err := filter(namespaces, oldSelection.target)
		if err != nil {
			log.Error(err, "Unable to filter namespaces for policy", "name", name)

			r.update(name, namespaceSelection{
				target:     oldSelection.target,
				namespaces: newNamespaces,
				hasUpdate:  oldSelection.err == nil, // it has an update if the error state changed
				err:        err,
			})

			continue
		}

		if !reflect.DeepEqual(newNamespaces, oldSelection.namespaces) {
			log.V(2).Info("Updating selection from Reconcile", "policy", name, "selection", newNamespaces)

			r.update(name, namespaceSelection{
				target:     oldSelection.target,
				namespaces: newNamespaces,
				hasUpdate:  true,
				err:        nil,
			})
		}
	}

	return ctrl.Result{}, nil
}

// Get returns the items matching the given Target for the given policy. If no selection for that
// policy and Target has been calculated, it will be calculated now. Otherwise, a cached value
// may be used.
func (r *NamespaceSelectorReconciler) Get(name string, t policyv1.Target) ([]string, error) {
	log := logf.Log.WithValues("Reconciler", "NamespaceSelector")

	r.lock.Lock()

	// If found, and target has not been changed
	if selection, found := r.selections[name]; found && selection.target.String() == t.String() {
		selection.hasUpdate = false
		r.selections[name] = selection

		r.lock.Unlock()

		return selection.namespaces, selection.err
	}

	// unlock for now, the list filtering could take a non-trivial amount of time
	r.lock.Unlock()

	// New, or the target has changed.
	nsList := corev1.NamespaceList{}

	labelSelector := parseToLabelSelector(t)

	selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return nil, fmt.Errorf("error parsing namespace LabelSelector: %w", err)
	}

	// This List will be from the cache
	if err := r.Client.List(context.TODO(), &nsList, &client.ListOptions{LabelSelector: selector}); err != nil {
		log.Error(err, "Unable to list namespaces from the cache")

		return nil, err
	}

	nsToMatch := make([]string, len(nsList.Items))
	for i, ns := range nsList.Items {
		nsToMatch[i] = ns.Name
	}

	selected, err := Matches(nsToMatch, t.Include, t.Exclude)
	sort.Strings(selected)

	log.V(2).Info("Updating selection from Reconcile", "policy", name, "selection", selected)

	r.update(name, namespaceSelection{
		target:     t,
		namespaces: selected,
		hasUpdate:  false,
		err:        err,
	})

	return selected, err
}

// HasUpdate indicates when the cached selection for this policy has been changed since the last
// time that Get was called for that policy.
func (r *NamespaceSelectorReconciler) HasUpdate(name string) bool {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return r.selections[name].hasUpdate
}

// Stop tells the SelectorReconciler to stop updating the cached selection for the name.
func (r *NamespaceSelectorReconciler) Stop(name string) {
	r.lock.Lock()
	defer r.lock.Unlock()

	delete(r.selections, name)
}

func (r *NamespaceSelectorReconciler) update(name string, sel namespaceSelection) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.selections[name] = sel
}

func filter(allNSList corev1.NamespaceList, t policyv1.Target) ([]string, error) {
	labelSelector := parseToLabelSelector(t)

	selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return nil, fmt.Errorf("error parsing namespace LabelSelector: %w", err)
	}

	nsToFilter := make([]string, 0)

	for _, ns := range allNSList.Items {
		if selector.Matches(labels.Set(ns.GetLabels())) {
			nsToFilter = append(nsToFilter, ns.Name)
		}
	}

	namespaces, err := Matches(nsToFilter, t.Include, t.Exclude)
	sort.Strings(namespaces)

	return namespaces, err
}
