// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	templates "github.com/stolostron/go-template-utils/v4/pkg/templates"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
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
	namespaceGVK = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	}
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
	customResourceDefinitionGVK = schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
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
	packageManifestGVR = schema.GroupVersionResource{
		Group:    "packages.operators.coreos.com",
		Version:  "v1",
		Resource: "packagemanifests",
	}
	ErrPackageManifest = errors.New("")
)

// OperatorPolicyReconciler reconciles a OperatorPolicy object
type OperatorPolicyReconciler struct {
	client.Client
	DynamicClient    dynamic.Interface
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
		WatchesRawSource(
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

	errs := make([]error, 0)

	conditionsToEmit, conditionChanged, err := r.handleResources(ctx, policy)
	if err != nil {
		errs = append(errs, err)
	}

	if conditionChanged {
		// Add an event for the "final" state of the policy, otherwise this only has the
		// "early" events (and possibly has zero events).
		conditionsToEmit = append(conditionsToEmit, calculateComplianceCondition(policy))

		if err := r.Status().Update(ctx, policy); err != nil {
			errs = append(errs, err)
		}
	}

	for _, cond := range conditionsToEmit {
		if err := r.emitComplianceEvent(ctx, policy, cond); err != nil {
			errs = append(errs, err)
		}
	}

	return reconcile.Result{}, utilerrors.NewAggregate(errs)
}

// handleResources determines the current desired state based on the policy, and
// determines status details for the policy based on the current state of
// resources in the cluster. If the policy is enforced, it will make updates
// on the cluster. This function returns:
//   - compliance conditions that should be emitted as events, detailing the
//     state before an action was taken
//   - whether the policy status needs to be updated, and a new compliance event
//     should be emitted
//   - an error, if one is encountered
func (r *OperatorPolicyReconciler) handleResources(ctx context.Context, policy *policyv1beta1.OperatorPolicy) (
	earlyComplianceEvents []metav1.Condition, condChanged bool, err error,
) {
	OpLog := ctrl.LoggerFrom(ctx)

	earlyComplianceEvents = make([]metav1.Condition, 0)

	desiredSub, desiredOG, changed, err := r.buildResources(ctx, policy)
	condChanged = changed

	if err != nil {
		OpLog.Error(err, "Error building desired resources")

		return earlyComplianceEvents, condChanged, err
	}

	desiredSubName := ""
	if desiredSub != nil {
		desiredSubName = desiredSub.Name
	}

	ogCorrect, earlyConds, changed, err := r.handleOpGroup(ctx, policy, desiredOG, desiredSubName)
	earlyComplianceEvents = append(earlyComplianceEvents, earlyConds...)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling OperatorGroup")

		return earlyComplianceEvents, condChanged, err
	}

	subscription, earlyConds, changed, err := r.handleSubscription(ctx, policy, desiredSub, ogCorrect)
	earlyComplianceEvents = append(earlyComplianceEvents, earlyConds...)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling Subscription")

		return earlyComplianceEvents, condChanged, err
	}

	changed, err = r.handleInstallPlan(ctx, policy, subscription)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling InstallPlan")

		return earlyComplianceEvents, condChanged, err
	}

	csv, earlyConds, changed, err := r.handleCSV(ctx, policy, subscription)
	earlyComplianceEvents = append(earlyComplianceEvents, earlyConds...)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling ClusterServiceVersions")

		return earlyComplianceEvents, condChanged, err
	}

	earlyConds, changed, err = r.handleCRDs(ctx, policy, subscription)
	earlyComplianceEvents = append(earlyComplianceEvents, earlyConds...)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling CustomResourceDefinitions")

		return earlyComplianceEvents, condChanged, err
	}

	changed, err = r.handleDeployment(ctx, policy, csv)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling Deployments")

		return earlyComplianceEvents, condChanged, err
	}

	changed, err = r.handleCatalogSource(policy, subscription)
	condChanged = condChanged || changed

	if err != nil {
		OpLog.Error(err, "Error handling CatalogSource")

		return earlyComplianceEvents, condChanged, err
	}

	return earlyComplianceEvents, condChanged, nil
}

// buildResources builds 'musthave' desired states for the Subscription and OperatorGroup, and
// checks if the policy's spec is valid. It returns:
//   - the built Subscription
//   - the built OperatorGroup
//   - whether the status has changed because of the validity condition
//   - an error if an API call failed
//
// The built objects can be used to find relevant objects for a 'mustnothave' policy.
func (r *OperatorPolicyReconciler) buildResources(ctx context.Context, policy *policyv1beta1.OperatorPolicy) (
	*operatorv1alpha1.Subscription, *operatorv1.OperatorGroup, bool, error,
) {
	var returnedErr error

	validationErrors := make([]error, 0)

	disableTemplates := false

	if disableAnnotation, ok := policy.GetAnnotations()["policy.open-cluster-management.io/disable-templates"]; ok {
		disableTemplates, _ = strconv.ParseBool(disableAnnotation) // on error, templates will not be disabled
	}

	var tmplResolver *templates.TemplateResolver

	if !disableTemplates {
		var err error

		tmplResolver, err = templates.NewResolverWithDynamicWatcher(r.DynamicWatcher, templates.Config{})
		if err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("unable to create template resolver: %w", err))
		}
	}

	sub, subErr := buildSubscription(policy, r.DefaultNamespace, tmplResolver)
	if subErr == nil {
		err := r.applySubscriptionDefaults(ctx, sub)
		if err != nil {
			sub = nil

			// If it's a PackageManifest API error, then that means it should be returned for the Reconcile method
			// to requeue the request. This is to workaround the PackageManifest API not supporting watches.
			if errors.Is(err, ErrPackageManifest) {
				returnedErr = err
			}

			validationErrors = append(validationErrors, err)
		}
	} else {
		validationErrors = append(validationErrors, subErr)
	}

	opGroupNS := r.DefaultNamespace
	if sub != nil && sub.Namespace != "" {
		opGroupNS = sub.Namespace
	}

	opGroup, ogErr := buildOperatorGroup(policy, opGroupNS, tmplResolver)
	if ogErr != nil {
		validationErrors = append(validationErrors, ogErr)
	} else {
		watcher := opPolIdentifier(policy.Namespace, policy.Name)

		gotNamespace, err := r.DynamicWatcher.Get(watcher, namespaceGVK, "", opGroupNS)
		if err != nil {
			return sub, opGroup, false, fmt.Errorf("error getting operator namespace: %w", err)
		}

		if gotNamespace == nil {
			validationErrors = append(validationErrors,
				fmt.Errorf("the operator namespace ('%v') does not exist", opGroupNS))
		}
	}

	return sub, opGroup, updateStatus(policy, validationCond(validationErrors)), returnedErr
}

// applySubscriptionDefaults will set the subscription channel, source, and sourceNamespace when they are unset by
// utilizing the PackageManifest API.
func (r *OperatorPolicyReconciler) applySubscriptionDefaults(
	ctx context.Context, subscription *operatorv1alpha1.Subscription,
) error {
	subSpec := subscription.Spec

	defaultsNeeded := subSpec.Channel == "" || subSpec.CatalogSource == "" || subSpec.CatalogSourceNamespace == ""

	if !defaultsNeeded {
		return nil
	}

	// PackageManifests come from an API server and not a Kubernetes resource, so the DynamicWatcher can't be used since
	// it utilizes watches. The namespace doesn't have any meaning but is required.
	packageManifest, err := r.DynamicClient.Resource(packageManifestGVR).Namespace("default").Get(
		ctx, subSpec.Package, metav1.GetOptions{},
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf(
				"%wthe subscription defaults could not be determined because the PackageManifest was not found",
				ErrPackageManifest,
			)
		}

		log.Error(err, "Failed to get the PackageManifest", "name", subSpec.Package)

		return fmt.Errorf(
			"%wthe subscription defaults could not be determined because the PackageManifest API returned an error",
			ErrPackageManifest,
		)
	}

	catalog, _, _ := unstructured.NestedString(packageManifest.Object, "status", "catalogSource")
	catalogNamespace, _, _ := unstructured.NestedString(packageManifest.Object, "status", "catalogSourceNamespace")

	if catalog == "" || catalogNamespace == "" {
		return errors.New(
			"the subscription defaults could not be determined because the PackageManifest didn't specify a catalog",
		)
	}

	if (subSpec.CatalogSource != "" && subSpec.CatalogSource != catalog) ||
		(subSpec.CatalogSourceNamespace != "" && subSpec.CatalogSourceNamespace != catalogNamespace) {
		return errors.New(
			"the subscription defaults could not be determined because the catalog specified in the policy does " +
				"not match what was found in the PackageManifest on the cluster",
		)
	}

	if subSpec.Channel == "" {
		defaultChannel, _, _ := unstructured.NestedString(packageManifest.Object, "status", "defaultChannel")
		if defaultChannel == "" {
			return errors.New(
				"the default channel could not be determined because the PackageManifest didn't specify one",
			)
		}

		subSpec.Channel = defaultChannel
	}

	if subSpec.CatalogSource == "" || subSpec.CatalogSourceNamespace == "" {
		subSpec.CatalogSource = catalog
		subSpec.CatalogSourceNamespace = catalogNamespace
	}

	return nil
}

// buildSubscription bootstraps the subscription spec defined in the operator policy
// with the apiversion and kind in preparation for resource creation.
// If an error is returned, it will include details on why the policy spec if invalid and
// why the desired subscription can't be determined.
func buildSubscription(
	policy *policyv1beta1.OperatorPolicy, defaultNS string, tmplResolver *templates.TemplateResolver,
) (*operatorv1alpha1.Subscription, error) {
	subscription := new(operatorv1alpha1.Subscription)

	rawSub := policy.Spec.Subscription.Raw

	if tmplResolver != nil && templates.HasTemplate(rawSub, "", false) {
		watcher := opPolIdentifier(policy.Namespace, policy.Name)

		resolvedTmpl, err := tmplResolver.ResolveTemplate(rawSub, nil, &templates.ResolveOptions{Watcher: &watcher})
		if err != nil {
			return nil, fmt.Errorf("could not build subscription: %w", err)
		}

		rawSub = resolvedTmpl.ResolvedJSON
	}

	sub := make(map[string]interface{})

	err := json.Unmarshal(rawSub, &sub)
	if err != nil {
		return nil, fmt.Errorf("the policy spec.subscription is invalid: %w", err)
	}

	name, ok := sub["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("name is required in spec.subscription")
	}

	if validationErrs := validation.IsDNS1123Label(name); len(validationErrs) != 0 {
		return nil, fmt.Errorf(
			"the name '%v' used for the subscription is invalid: %s", name, strings.Join(validationErrs, ", "),
		)
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

	// This field is not actually in the subscription spec
	delete(sub, "namespace")

	subSpec, err := json.Marshal(sub)
	if err != nil {
		return nil, fmt.Errorf("the policy spec.subscription is invalid: %w", err)
	}

	// Use a decoder to find fields that were erroneously set by the user.
	dec := json.NewDecoder(bytes.NewReader(subSpec))
	dec.DisallowUnknownFields()

	spec := new(operatorv1alpha1.SubscriptionSpec)

	if err := dec.Decode(spec); err != nil {
		return nil, fmt.Errorf("the policy spec.subscription is invalid: %w", err)
	}

	subscription.SetGroupVersionKind(subscriptionGVK)
	subscription.ObjectMeta.Name = spec.Package
	subscription.ObjectMeta.Namespace = ns
	subscription.Spec = spec

	// This is not validated by the CRD, so validate it here to prevent unexpected behavior.
	if !(spec.InstallPlanApproval == "Manual" || spec.InstallPlanApproval == "Automatic") {
		return nil, fmt.Errorf("the policy spec.subscription.installPlanApproval ('%v') is invalid: "+
			"must be 'Automatic' or 'Manual'", spec.InstallPlanApproval)
	}

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
	policy *policyv1beta1.OperatorPolicy, namespace string, tmplResolver *templates.TemplateResolver,
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

	rawOG := policy.Spec.OperatorGroup.Raw

	if tmplResolver != nil && templates.HasTemplate(rawOG, "", false) {
		watcher := opPolIdentifier(policy.Namespace, policy.Name)

		resolvedTmpl, err := tmplResolver.ResolveTemplate(rawOG, nil, &templates.ResolveOptions{Watcher: &watcher})
		if err != nil {
			return nil, fmt.Errorf("could not build operator group: %w", err)
		}

		rawOG = resolvedTmpl.ResolvedJSON
	}

	opGroup := make(map[string]interface{})

	if err := json.Unmarshal(rawOG, &opGroup); err != nil {
		return nil, fmt.Errorf("the policy spec.operatorGroup is invalid: %w", err)
	}

	if specifiedNS, ok := opGroup["namespace"].(string); ok && specifiedNS != "" {
		if specifiedNS != namespace && namespace != "" {
			return nil, fmt.Errorf("the namespace specified in spec.operatorGroup ('%v') must match "+
				"the namespace used for the subscription ('%v')", specifiedNS, namespace)
		}
	}

	name, ok := opGroup["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name is required in spec.operatorGroup")
	}

	// These fields are not actually in the operatorGroup spec
	delete(opGroup, "name")
	delete(opGroup, "namespace")

	opGroupSpec, err := json.Marshal(opGroup)
	if err != nil {
		return nil, fmt.Errorf("the policy spec.operatorGroup is invalid: %w", err)
	}

	// Use a decoder to find fields that were erroneously set by the user.
	dec := json.NewDecoder(bytes.NewReader(opGroupSpec))
	dec.DisallowUnknownFields()

	spec := new(operatorv1.OperatorGroupSpec)

	if err := dec.Decode(spec); err != nil {
		return nil, fmt.Errorf("the policy spec.operatorGroup is invalid: %w", err)
	}

	operatorGroup.ObjectMeta.SetName(name)
	operatorGroup.ObjectMeta.SetNamespace(namespace)
	operatorGroup.Spec = *spec

	return operatorGroup, nil
}

func (r *OperatorPolicyReconciler) handleOpGroup(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	desiredOpGroup *operatorv1.OperatorGroup,
	desiredSubName string,
) (bool, []metav1.Condition, bool, error) {
	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	if desiredOpGroup == nil || desiredOpGroup.Namespace == "" {
		// Note: existing related objects will not be removed by this status update
		return false, nil, updateStatus(policy, invalidCausingUnknownCond("OperatorGroup")), nil
	}

	foundOpGroups, err := r.DynamicWatcher.List(
		watcher, operatorGroupGVK, desiredOpGroup.Namespace, labels.Everything())
	if err != nil {
		return false, nil, false, fmt.Errorf("error listing OperatorGroups: %w", err)
	}

	if policy.Spec.ComplianceType.IsMustHave() {
		return r.musthaveOpGroup(ctx, policy, desiredOpGroup, foundOpGroups)
	}

	earlyConds, changed, err := r.mustnothaveOpGroup(ctx, policy, desiredOpGroup, foundOpGroups, desiredSubName)

	// In mustnothave mode, we can always act as if the OperatorGroup is not correct
	return false, earlyConds, changed, err
}

func (r *OperatorPolicyReconciler) musthaveOpGroup(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	desiredOpGroup *operatorv1.OperatorGroup,
	foundOpGroups []unstructured.Unstructured,
) (bool, []metav1.Condition, bool, error) {
	switch len(foundOpGroups) {
	case 0:
		// Missing OperatorGroup: report NonCompliance
		changed := updateStatus(policy, missingWantedCond("OperatorGroup"), missingWantedObj(desiredOpGroup))

		if policy.Spec.RemediationAction.IsInform() {
			return false, nil, changed, nil
		}

		earlyConds := []metav1.Condition{}

		if changed {
			earlyConds = append(earlyConds, calculateComplianceCondition(policy))
		}

		err := r.createWithNamespace(ctx, policy, desiredOpGroup)
		if err != nil {
			return false, nil, changed, fmt.Errorf("error creating the OperatorGroup: %w", err)
		}

		desiredOpGroup.SetGroupVersionKind(operatorGroupGVK) // Create stripped this information

		// Now the OperatorGroup should match, so report Compliance
		updateStatus(policy, createdCond("OperatorGroup"), createdObj(desiredOpGroup))

		return true, earlyConds, true, nil
	case 1:
		opGroup := foundOpGroups[0]

		// Check if what's on the cluster matches what the policy wants (whether it's specified or not)

		emptyNameMatch := desiredOpGroup.Name == "" && opGroup.GetGenerateName() == desiredOpGroup.GenerateName

		if !(opGroup.GetName() == desiredOpGroup.Name || emptyNameMatch) {
			if policy.Spec.OperatorGroup == nil {
				// The policy doesn't specify what the OperatorGroup should look like, but what is already
				// there is not the default one the policy would create.
				// FUTURE: check if the one operator group is compatible with the desired subscription.
				// For now, assume if an OperatorGroup already exists, then it's a good one.
				return true, nil, updateStatus(policy, opGroupPreexistingCond, matchedObj(&opGroup)), nil
			}

			// There is an OperatorGroup in the namespace that does not match the name of what is in the policy.
			// Just creating a new one would cause the "TooManyOperatorGroups" failure.
			// So, just report a NonCompliant status.
			missing := missingWantedObj(desiredOpGroup)
			badExisting := mismatchedObj(&opGroup)

			return false, nil, updateStatus(policy, mismatchCond("OperatorGroup"), missing, badExisting), nil
		}

		// check whether the specs match
		desiredUnstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desiredOpGroup)
		if err != nil {
			return false, nil, false, fmt.Errorf("error converting desired OperatorGroup to an Unstructured: %w", err)
		}

		updateNeeded, skipUpdate, err := r.mergeObjects(
			ctx, desiredUnstruct, &opGroup, string(policy.Spec.ComplianceType),
		)
		if err != nil {
			return false, nil, false, fmt.Errorf("error checking if the OperatorGroup needs an update: %w", err)
		}

		if !updateNeeded {
			// Everything relevant matches!
			return true, nil, updateStatus(policy, matchesCond("OperatorGroup"), matchedObj(&opGroup)), nil
		}

		// Specs don't match.

		if policy.Spec.OperatorGroup == nil {
			// The policy doesn't specify what the OperatorGroup should look like, but what is already
			// there is not the default one the policy would create.
			// FUTURE: check if the one operator group is compatible with the desired subscription.
			// For now, assume if an OperatorGroup already exists, then it's a good one.
			return true, nil, updateStatus(policy, opGroupPreexistingCond, matchedObj(&opGroup)), nil
		}

		if policy.Spec.RemediationAction.IsEnforce() && skipUpdate {
			changed := updateStatus(policy, mismatchCondUnfixable("OperatorGroup"), mismatchedObj(&opGroup))

			return false, nil, changed, nil
		}

		// The names match, but the specs don't: report NonCompliance
		changed := updateStatus(policy, mismatchCond("OperatorGroup"), mismatchedObj(&opGroup))

		if policy.Spec.RemediationAction.IsInform() {
			return false, nil, changed, nil
		}

		earlyConds := []metav1.Condition{}

		if changed {
			earlyConds = append(earlyConds, calculateComplianceCondition(policy))
		}

		desiredOpGroup.ResourceVersion = opGroup.GetResourceVersion()

		err = r.Update(ctx, &opGroup)
		if err != nil {
			return false, nil, changed, fmt.Errorf("error updating the OperatorGroup: %w", err)
		}

		desiredOpGroup.SetGroupVersionKind(operatorGroupGVK) // Update stripped this information

		updateStatus(policy, updatedCond("OperatorGroup"), updatedObj(desiredOpGroup))

		return true, earlyConds, true, nil
	default:
		// This situation will always lead to a "TooManyOperatorGroups" failure on the CSV.
		// Consider improving this in the future: perhaps this could suggest one of the OperatorGroups to keep.
		return false, nil, updateStatus(policy, opGroupTooManyCond, opGroupTooManyObjs(foundOpGroups)...), nil
	}
}

// createWithNamespace will create the input object and the object's namespace if needed.
func (r *OperatorPolicyReconciler) createWithNamespace(
	ctx context.Context, policy *policyv1beta1.OperatorPolicy, object client.Object,
) error {
	err := r.Create(ctx, object)
	if err == nil {
		return nil
	}

	// If the error is not due to a missing namespace or the namespace is not set on the object, return the error.
	if !isNamespaceNotFound(err) || object.GetNamespace() == "" {
		return err
	}

	log.Info("Creating the namespace since it didn't exist", "name", object.GetNamespace(), "policy", policy.Name)

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: object.GetNamespace(),
		},
	}

	err = r.Create(ctx, &ns)
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	// Try creating the object again now that the namespace was created.
	return r.Create(ctx, object)
}

// isNamespaceNotFound detects if the input error from r.Create failed due to the specified namespace not existing.
func isNamespaceNotFound(err error) bool {
	if !k8serrors.IsNotFound(err) {
		return false
	}

	statusErr := &k8serrors.StatusError{}
	if !errors.As(err, &statusErr) {
		return false
	}

	status := statusErr.Status()

	return status.Details != nil && status.Details.Kind == "namespaces"
}

func (r *OperatorPolicyReconciler) mustnothaveOpGroup(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	desiredOpGroup *operatorv1.OperatorGroup,
	allFoundOpGroups []unstructured.Unstructured,
	desiredSubName string,
) ([]metav1.Condition, bool, error) {
	if len(allFoundOpGroups) == 0 {
		// Missing OperatorGroup: report Compliance
		changed := updateStatus(policy, missingNotWantedCond("OperatorGroup"), missingNotWantedObj(desiredOpGroup))

		return nil, changed, nil
	}

	if len(allFoundOpGroups) > 1 {
		// Don't try to choose one, just report this as NonCompliant.
		return nil, updateStatus(policy, opGroupTooManyCond, opGroupTooManyObjs(allFoundOpGroups)...), nil
	}

	foundOpGroup := allFoundOpGroups[0]

	removalBehavior := policy.Spec.RemovalBehavior.ApplyDefaults()
	keep := removalBehavior.OperatorGroups.IsKeep()

	// if DeleteIfUnused and there are other subscriptions, then keep the operator group
	if removalBehavior.OperatorGroups.IsDeleteIfUnused() {
		// Check the namespace for any subscriptions, including the sub for this mustnothave policy,
		// since deleting the OperatorGroup before that could cause problems
		watcher := opPolIdentifier(policy.Namespace, policy.Name)

		foundSubscriptions, err := r.DynamicWatcher.List(
			watcher, subscriptionGVK, desiredOpGroup.Namespace, labels.Everything())
		if err != nil {
			return nil, false, fmt.Errorf("error listing Subscriptions: %w", err)
		}

		anotherSubFound := false

		for _, sub := range foundSubscriptions {
			if sub.GetName() != desiredSubName {
				anotherSubFound = true

				break
			}
		}

		if anotherSubFound {
			keep = true
		}
	}

	if keep {
		return nil, updateStatus(policy, keptCond("OperatorGroup"), leftoverObj(&foundOpGroup)), nil
	}

	emptyNameMatch := desiredOpGroup.Name == "" && foundOpGroup.GetGenerateName() == desiredOpGroup.GenerateName

	if !(foundOpGroup.GetName() == desiredOpGroup.Name || emptyNameMatch) {
		// no found OperatorGroup matches what the policy is looking for, report Compliance.
		changed := updateStatus(policy, missingNotWantedCond("OperatorGroup"), missingNotWantedObj(desiredOpGroup))

		return nil, changed, nil
	}

	desiredOpGroup.SetName(foundOpGroup.GetName()) // set it for the generateName case

	if len(foundOpGroup.GetOwnerReferences()) != 0 {
		// the OperatorGroup specified in the policy might be used or managed by something else
		// so we will keep it.
		return nil, updateStatus(policy, keptCond("OperatorGroup"), leftoverObj(desiredOpGroup)), nil
	}

	// The found OperatorGroup matches what is *not* wanted by the policy. Report NonCompliance.
	changed := updateStatus(policy, foundNotWantedCond("OperatorGroup"), foundNotWantedObj(desiredOpGroup))

	if policy.Spec.RemediationAction.IsInform() {
		return nil, changed, nil
	}

	if foundOpGroup.GetDeletionTimestamp() != nil {
		// No "early" condition because that would cause the status to flap
		return nil, updateStatus(policy, deletingCond("OperatorGroup"), deletingObj(desiredOpGroup)), nil
	}

	earlyConds := []metav1.Condition{}

	if changed {
		earlyConds = append(earlyConds, calculateComplianceCondition(policy))
	}

	err := r.Delete(ctx, desiredOpGroup)
	if err != nil {
		return earlyConds, changed, fmt.Errorf("error deleting the OperatorGroup: %w", err)
	}

	desiredOpGroup.SetGroupVersionKind(operatorGroupGVK) // Delete stripped this information

	updateStatus(policy, deletedCond("OperatorGroup"), deletedObj(desiredOpGroup))

	return earlyConds, true, nil
}

func (r *OperatorPolicyReconciler) handleSubscription(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	desiredSub *operatorv1alpha1.Subscription,
	ogCorrect bool,
) (*operatorv1alpha1.Subscription, []metav1.Condition, bool, error) {
	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	if desiredSub == nil {
		// Note: existing related objects will not be removed by this status update
		return nil, nil, updateStatus(policy, invalidCausingUnknownCond("Subscription")), nil
	}

	foundSub, err := r.DynamicWatcher.Get(watcher, subscriptionGVK, desiredSub.Namespace, desiredSub.Name)
	if err != nil {
		return nil, nil, false, fmt.Errorf("error getting the Subscription: %w", err)
	}

	if policy.Spec.ComplianceType.IsMustHave() {
		return r.musthaveSubscription(ctx, policy, desiredSub, foundSub, ogCorrect)
	}

	return r.mustnothaveSubscription(ctx, policy, desiredSub, foundSub)
}

func (r *OperatorPolicyReconciler) musthaveSubscription(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	desiredSub *operatorv1alpha1.Subscription,
	foundSub *unstructured.Unstructured,
	ogCorrect bool,
) (*operatorv1alpha1.Subscription, []metav1.Condition, bool, error) {
	if foundSub == nil {
		// Missing Subscription: report NonCompliance
		changed := updateStatus(policy, missingWantedCond("Subscription"), missingWantedObj(desiredSub))

		// If informing, or if the OperatorGroup is not correct, don't make any changes
		if policy.Spec.RemediationAction.IsInform() || !ogCorrect {
			return desiredSub, nil, changed, nil
		}

		earlyConds := []metav1.Condition{}

		if changed {
			earlyConds = append(earlyConds, calculateComplianceCondition(policy))
		}

		err := r.createWithNamespace(ctx, policy, desiredSub)
		if err != nil {
			return nil, nil, changed, fmt.Errorf("error creating the Subscription: %w", err)
		}

		desiredSub.SetGroupVersionKind(subscriptionGVK) // Create stripped this information

		// Now it should match, so report Compliance
		updateStatus(policy, createdCond("Subscription"), createdObj(desiredSub))

		return desiredSub, earlyConds, true, nil
	}

	// Subscription found; check if specs match
	desiredUnstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desiredSub)
	if err != nil {
		return nil, nil, false, fmt.Errorf("error converting desired Subscription to an Unstructured: %w", err)
	}

	updateNeeded, skipUpdate, err := r.mergeObjects(ctx, desiredUnstruct, foundSub, string(policy.Spec.ComplianceType))
	if err != nil {
		return nil, nil, false, fmt.Errorf("error checking if the Subscription needs an update: %w", err)
	}

	mergedSub := new(operatorv1alpha1.Subscription)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(foundSub.Object, mergedSub); err != nil {
		return nil, nil, false, fmt.Errorf("error converting the retrieved Subscription to the go type: %w", err)
	}

	if !updateNeeded {
		subResFailed := mergedSub.Status.GetCondition(operatorv1alpha1.SubscriptionResolutionFailed)

		// OLM includes the status of all subscriptions in the namespace. For example, if you have two subscriptions,
		// where one is referencing a valid operator and the other isn't, both will have a failed subscription
		// resolution condition.
		if subResFailed.Status == corev1.ConditionTrue {
			includesSubscription, err := messageIncludesSubscription(mergedSub, subResFailed.Message)
			if err != nil {
				log.Info(
					"Failed to determine if the condition applied to this subscription. Assuming it does.",
					"error", err.Error(), "subscription", mergedSub.Name, "package", mergedSub.Spec.Package,
					"message", subResFailed.Message,
				)

				includesSubscription = true
			}

			if includesSubscription {
				// a "constraints not satisfiable" message has nondeterministic clauses, and thus
				// need to be sorted in order to check that they are not duplicates of the current message.
				if constraintMessageMatch(policy, &subResFailed) {
					return mergedSub, nil, false, nil
				}

				return mergedSub, nil, updateStatus(
					policy, subResFailedCond(subResFailed), nonCompObj(foundSub, subResFailed.Reason)), nil
			}

			return mergedSub, nil, false, nil
		}

		return mergedSub, nil, updateStatus(policy, matchesCond("Subscription"), matchedObj(foundSub)), nil
	}

	// Specs don't match.
	if policy.Spec.RemediationAction.IsEnforce() && skipUpdate {
		changed := updateStatus(policy, mismatchCondUnfixable("Subscription"), mismatchedObj(foundSub))

		return mergedSub, nil, changed, nil
	}

	changed := updateStatus(policy, mismatchCond("Subscription"), mismatchedObj(foundSub))

	if policy.Spec.RemediationAction.IsInform() {
		return mergedSub, nil, changed, nil
	}

	earlyConds := []metav1.Condition{}

	if changed {
		earlyConds = append(earlyConds, calculateComplianceCondition(policy))
	}

	err = r.Update(ctx, foundSub)
	if err != nil {
		return mergedSub, nil, changed, fmt.Errorf("error updating the Subscription: %w", err)
	}

	foundSub.SetGroupVersionKind(subscriptionGVK) // Update stripped this information

	updateStatus(policy, updatedCond("Subscription"), updatedObj(foundSub))

	return mergedSub, earlyConds, true, nil
}

func (r *OperatorPolicyReconciler) mustnothaveSubscription(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	desiredSub *operatorv1alpha1.Subscription,
	foundUnstructSub *unstructured.Unstructured,
) (*operatorv1alpha1.Subscription, []metav1.Condition, bool, error) {
	if foundUnstructSub == nil {
		// Missing Subscription: report Compliance
		changed := updateStatus(policy, missingNotWantedCond("Subscription"), missingNotWantedObj(desiredSub))

		return desiredSub, nil, changed, nil
	}

	foundSub := new(operatorv1alpha1.Subscription)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(foundUnstructSub.Object, foundSub); err != nil {
		return nil, nil, false, fmt.Errorf("error converting the retrieved Subscription to the go type: %w", err)
	}

	if policy.Spec.RemovalBehavior.ApplyDefaults().Subscriptions.IsKeep() {
		changed := updateStatus(policy, keptCond("Subscription"), leftoverObj(foundSub))

		return foundSub, nil, changed, nil
	}

	// Subscription found, not wanted: report NonCompliance.
	changed := updateStatus(policy, foundNotWantedCond("Subscription"), foundNotWantedObj(foundSub))

	if policy.Spec.RemediationAction.IsInform() {
		return foundSub, nil, changed, nil
	}

	if foundSub.GetDeletionTimestamp() != nil {
		// No "early" condition because that would cause the status to flap
		return foundSub, nil, updateStatus(policy, deletingCond("Subscription"), deletingObj(foundSub)), nil
	}

	earlyConds := []metav1.Condition{}

	if changed {
		earlyConds = append(earlyConds, calculateComplianceCondition(policy))
	}

	err := r.Delete(ctx, foundUnstructSub)
	if err != nil {
		return foundSub, earlyConds, changed, fmt.Errorf("error deleting the Subscription: %w", err)
	}

	updateStatus(policy, deletedCond("Subscription"), deletedObj(desiredSub))

	return foundSub, earlyConds, true, nil
}

// messageIncludesSubscription checks if the ConstraintsNotSatisfiable message includes the input
// subscription or package. Some examples that it catches:
// https://github.com/operator-framework/operator-lifecycle-manager/blob/dc0c564f62d526bae0467d53f439e1c91a17ed8a/pkg/controller/registry/resolver/resolver.go#L257-L267
// - no operators found from catalog %s in namespace %s referenced by subscription %s
// - no operators found in package %s in the catalog referenced by subscription %s
// - no operators found in channel %s of package %s in the catalog referenced by subscription %s
// - no operators found with name %s in channel %s of package %s in the catalog referenced by subscription %s
// - multiple name matches for status.installedCSV of subscription %s/%s: %s
func messageIncludesSubscription(subscription *operatorv1alpha1.Subscription, message string) (bool, error) {
	safeNs := regexp.QuoteMeta(subscription.Namespace)
	safeSubName := regexp.QuoteMeta(subscription.Name)
	safeSubNameWithNs := safeNs + `\/` + safeSubName
	safePackageName := regexp.QuoteMeta(subscription.Spec.Package)
	safePackageNameWithNs := safeNs + `\/` + safePackageName
	// Craft a regex that looks for mention of the subscription or package. Notice that after the package or
	// subscription name, it must either be the end of the string, white space, or a comma. This so that
	// "gatekeeper-operator" doesn't erroneously match "gatekeeper-operator-product".
	regex := fmt.Sprintf(
		`(?:subscription (?:%s|%s)|package (?:%s|%s))(?:$|\s|,|:)`,
		safeSubName, safeSubNameWithNs, safePackageName, safePackageNameWithNs,
	)

	return regexp.MatchString(regex, message)
}

// constraintMessageMatch checks if the ConstraintsNotSatisfiable message is actually different
// from the old one by sorting the clauses of the message
func constraintMessageMatch(policy *policyv1beta1.OperatorPolicy, cond *operatorv1alpha1.SubscriptionCondition) bool {
	const cnfPrefix = "constraints not satisfiable: "

	var policyMessage, subMessage string

	for _, statusCond := range policy.Status.Conditions {
		if strings.Contains(statusCond.Message, cnfPrefix) {
			policyMessage = statusCond.Message
		}
	}

	if policyMessage == "" || !strings.Contains(cond.Message, cnfPrefix) {
		return false
	}

	policyMessage = strings.TrimPrefix(policyMessage, cnfPrefix)
	subMessage = strings.TrimPrefix(cond.Message, cnfPrefix)

	// The ConstraintsNotSatisfiable message is always formatted as follows:
	// constraints not satisfiable: clause1, clause2, clause3 ...
	policyMessageSlice := strings.Split(policyMessage, ", ")
	slices.Sort(policyMessageSlice)

	subMessageSlice := strings.Split(subMessage, ", ")
	slices.Sort(subMessageSlice)

	return reflect.DeepEqual(policyMessageSlice, subMessageSlice)
}

func (r *OperatorPolicyReconciler) handleInstallPlan(
	ctx context.Context, policy *policyv1beta1.OperatorPolicy, sub *operatorv1alpha1.Subscription,
) (bool, error) {
	if sub == nil {
		// Note: existing related objects will not be removed by this status update
		return updateStatus(policy, invalidCausingUnknownCond("InstallPlan")), nil
	}

	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	foundInstallPlans, err := r.DynamicWatcher.List(
		watcher, installPlanGVK, sub.Namespace, labels.Everything())
	if err != nil {
		return false, fmt.Errorf("error listing InstallPlans: %w", err)
	}

	ownedInstallPlans := make([]unstructured.Unstructured, 0, len(foundInstallPlans))
	selector := subLabelSelector(sub)

	for _, installPlan := range foundInstallPlans {
		// sometimes the OwnerReferences aren't correct, but the label should be
		if selector.Matches(labels.Set(installPlan.GetLabels())) {
			ownedInstallPlans = append(ownedInstallPlans, installPlan)

			break
		}

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
	// be considered a reason for NonCompliance, regardless of musthave or mustnothave.
	if len(ownedInstallPlans) == 0 {
		return updateStatus(policy, noInstallPlansCond, noInstallPlansObj(sub.Namespace)), nil
	}

	if policy.Spec.ComplianceType.IsMustHave() {
		changed, err := r.musthaveInstallPlan(ctx, policy, sub, ownedInstallPlans)

		return changed, err
	}

	return r.mustnothaveInstallPlan(policy, ownedInstallPlans)
}

func (r *OperatorPolicyReconciler) musthaveInstallPlan(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	sub *operatorv1alpha1.Subscription,
	ownedInstallPlans []unstructured.Unstructured,
) (bool, error) {
	OpLog := ctrl.LoggerFrom(ctx)
	relatedInstallPlans := make([]policyv1.RelatedObject, 0, len(ownedInstallPlans))
	ipsRequiringApproval := make([]unstructured.Unstructured, 0)
	anyInstalling := false
	currentPlanFailed := false
	selector := subLabelSelector(sub)

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
			// only consider InstallPlans with this label for approval - this label is supposed to
			// indicate the "current" InstallPlan for this subscription.
			if selector.Matches(labels.Set(installPlan.GetLabels())) {
				ipsRequiringApproval = append(ipsRequiringApproval, installPlan)
			}
		case string(operatorv1alpha1.InstallPlanPhaseInstalling):
			anyInstalling = true
		case string(operatorv1alpha1.InstallPlanFailed):
			// Generally, a failed InstallPlan is not a reason for NonCompliance, because it could be from
			// an old installation. But if the current InstallPlan is failed, we should alert the user.
			if sub.Status.InstallPlanRef != nil && sub.Status.InstallPlanRef.Name == installPlan.GetName() {
				currentPlanFailed = true
			}
		}

		relatedInstallPlans = append(relatedInstallPlans, existingInstallPlanObj(&ownedInstallPlans[i], phase))
	}

	if currentPlanFailed {
		return updateStatus(policy, installPlanFailed, relatedInstallPlans...), nil
	}

	if anyInstalling {
		return updateStatus(policy, installPlanInstallingCond, relatedInstallPlans...), nil
	}

	if len(ipsRequiringApproval) == 0 {
		return updateStatus(policy, installPlansNoApprovals, relatedInstallPlans...), nil
	}

	allUpgradeVersions := make([]string, 0, len(ipsRequiringApproval))

	for _, installPlan := range ipsRequiringApproval {
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

		allUpgradeVersions = append(allUpgradeVersions, fmt.Sprintf("%v", csvNames))
	}

	// Only report this status in `inform` mode, because otherwise it could easily oscillate between this and
	// another condition below when being enforced.
	if policy.Spec.RemediationAction.IsInform() {
		// FUTURE: check policy.spec.statusConfig.upgradesAvailable to determine `compliant`.
		// For now this condition assumes it is set to 'NonCompliant'
		return updateStatus(policy, installPlanUpgradeCond(allUpgradeVersions, nil), relatedInstallPlans...), nil
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
		allowedVersions := make([]policyv1.NonEmptyString, 0, len(policy.Spec.Versions)+1)
		allowedVersions = append(allowedVersions, policy.Spec.Versions...)

		if sub.Spec.StartingCSV != "" {
			allowedVersions = append(allowedVersions, policyv1.NonEmptyString(sub.Spec.StartingCSV))
		}

		for _, acceptableCSV := range allowedVersions {
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
		changed := updateStatus(policy,
			installPlanUpgradeCond(allUpgradeVersions, approvableInstallPlans), relatedInstallPlans...)

		return changed, nil
	}

	if err := unstructured.SetNestedField(approvableInstallPlans[0].Object, true, "spec", "approved"); err != nil {
		return false, fmt.Errorf("error approving InstallPlan: %w", err)
	}

	if err := r.Update(ctx, &approvableInstallPlans[0]); err != nil {
		return false, fmt.Errorf("error updating approved InstallPlan: %w", err)
	}

	return updateStatus(policy, installPlanApprovedCond(approvedVersion), relatedInstallPlans...), nil
}

func (r *OperatorPolicyReconciler) mustnothaveInstallPlan(
	policy *policyv1beta1.OperatorPolicy,
	ownedInstallPlans []unstructured.Unstructured,
) (bool, error) {
	// Let OLM handle removing install plans
	relatedInstallPlans := make([]policyv1.RelatedObject, 0, len(ownedInstallPlans))

	for i := range ownedInstallPlans {
		relatedInstallPlans = append(relatedInstallPlans, foundNotApplicableObj(&ownedInstallPlans[i]))
	}

	changed := updateStatus(policy, notApplicableCond("InstallPlan"), relatedInstallPlans...)

	return changed, nil
}

func (r *OperatorPolicyReconciler) handleCSV(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	sub *operatorv1alpha1.Subscription,
) (*operatorv1alpha1.ClusterServiceVersion, []metav1.Condition, bool, error) {
	// case where subscription is nil
	if sub == nil {
		// need to report lack of existing CSV
		return nil, nil, updateStatus(policy, noCSVCond, noExistingCSVObj), nil
	}

	watcher := opPolIdentifier(policy.Namespace, policy.Name)
	selector := subLabelSelector(sub)

	csvList, err := r.DynamicWatcher.List(watcher, clusterServiceVersionGVK, sub.Namespace, selector)
	if err != nil {
		return nil, nil, false, fmt.Errorf("error listing CSVs: %w", err)
	}

	var foundCSV *operatorv1alpha1.ClusterServiceVersion

	for _, csv := range csvList {
		if csv.GetName() == sub.Status.InstalledCSV {
			matchedCSV := operatorv1alpha1.ClusterServiceVersion{}

			err = runtime.DefaultUnstructuredConverter.FromUnstructured(csv.UnstructuredContent(), &matchedCSV)
			if err != nil {
				return nil, nil, false, err
			}

			foundCSV = &matchedCSV

			break
		}
	}

	if policy.Spec.ComplianceType.IsMustNotHave() {
		earlyConds, changed, err := r.mustnothaveCSV(ctx, policy, csvList, sub.Namespace)

		return foundCSV, earlyConds, changed, err
	}

	// CSV has not yet been created by OLM
	if foundCSV == nil {
		changed := updateStatus(policy,
			missingWantedCond("ClusterServiceVersion"), missingCSVObj(sub.Name, sub.Namespace))

		return foundCSV, nil, changed, nil
	}

	return foundCSV, nil, updateStatus(policy, buildCSVCond(foundCSV), existingCSVObj(foundCSV)), nil
}

func (r *OperatorPolicyReconciler) mustnothaveCSV(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	csvList []unstructured.Unstructured,
	namespace string,
) ([]metav1.Condition, bool, error) {
	if len(csvList) == 0 {
		changed := updateStatus(policy, missingNotWantedCond("ClusterServiceVersion"),
			missingNotWantedCSVObj(namespace))

		return nil, changed, nil
	}

	relatedCSVs := make([]policyv1.RelatedObject, 0, len(csvList))

	if policy.Spec.RemovalBehavior.ApplyDefaults().CSVs.IsKeep() {
		for i := range csvList {
			relatedCSVs = append(relatedCSVs, leftoverObj(&csvList[i]))
		}

		return nil, updateStatus(policy, keptCond("ClusterServiceVersion"), relatedCSVs...), nil
	}

	csvNames := make([]string, 0, len(csvList))

	for i := range csvList {
		relatedCSVs = append(relatedCSVs, foundNotWantedObj(&csvList[i]))
		csvNames = append(csvNames, csvList[i].GetName())
	}

	changed := updateStatus(policy, foundNotWantedCond("ClusterServiceVersion", csvNames...), relatedCSVs...)

	if policy.Spec.RemediationAction.IsInform() {
		return nil, changed, nil
	}

	earlyConds := []metav1.Condition{}

	if changed {
		earlyConds = append(earlyConds, calculateComplianceCondition(policy))
	}

	anyAlreadyDeleting := false

	for i := range csvList {
		if csvList[i].GetDeletionTimestamp() != nil {
			anyAlreadyDeleting = true
			relatedCSVs[i] = deletingObj(&csvList[i])

			// Add a watch specifically for this CSV: the existing watch uses a label selector,
			// and does not necessarily get notified events when the object is fully removed.
			watcher := opPolIdentifier(policy.Namespace, policy.Name)

			_, err := r.DynamicWatcher.Get(watcher, clusterServiceVersionGVK,
				csvList[i].GetNamespace(), csvList[i].GetName())
			if err != nil {
				return earlyConds, changed, fmt.Errorf("error watching the deleting CSV: %w", err)
			}

			continue
		}

		err := r.Delete(ctx, &csvList[i])
		if err != nil {
			changed := updateStatus(policy, foundNotWantedCond("ClusterServiceVersion", csvNames...), relatedCSVs...)

			if anyAlreadyDeleting {
				// reset the "early" conditions to avoid flapping
				earlyConds = []metav1.Condition{}
			}

			return earlyConds, changed, fmt.Errorf("error deleting ClusterServiceVersion: %w", err)
		}

		csvList[i].SetGroupVersionKind(clusterServiceVersionGVK)
		relatedCSVs[i] = deletedObj(&csvList[i])
	}

	if anyAlreadyDeleting {
		// reset the "early" conditions to avoid flapping
		earlyConds = []metav1.Condition{}

		return earlyConds, updateStatus(policy, deletingCond("ClusterServiceVersion", csvNames...), relatedCSVs...), nil
	}

	updateStatus(policy, deletedCond("ClusterServiceVersion", csvNames...), relatedCSVs...)

	return earlyConds, true, nil
}

func (r *OperatorPolicyReconciler) handleDeployment(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	csv *operatorv1alpha1.ClusterServiceVersion,
) (bool, error) {
	// case where csv is nil
	if csv == nil {
		// need to report lack of existing Deployments
		if policy.Spec.ComplianceType.IsMustHave() {
			return updateStatus(policy, noDeploymentsCond, noExistingDeploymentObj), nil
		}

		return updateStatus(policy, notApplicableCond("Deployment")), nil
	}

	OpLog := ctrl.LoggerFrom(ctx)

	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	var relatedObjects []policyv1.RelatedObject
	var unavailableDeployments []appsv1.Deployment

	depNum := 0

	for _, dep := range csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		foundDep, err := r.DynamicWatcher.Get(watcher, deploymentGVK, csv.Namespace, dep.Name)
		if err != nil {
			return false, fmt.Errorf("error getting the Deployment: %w", err)
		}

		// report missing deployment in relatedObjects list
		if foundDep == nil {
			relatedObjects = append(relatedObjects,
				missingObj(dep.Name, csv.Namespace, policy.Spec.ComplianceType, deploymentGVK))

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

		if policy.Spec.ComplianceType.IsMustNotHave() {
			relatedObjects = append(relatedObjects, foundNotApplicableObj(&dep))
		} else {
			relatedObjects = append(relatedObjects, existingDeploymentObj(&dep))
		}
	}

	if policy.Spec.ComplianceType.IsMustNotHave() {
		return updateStatus(policy, notApplicableCond("Deployment"), relatedObjects...), nil
	}

	return updateStatus(policy, buildDeploymentCond(depNum > 0, unavailableDeployments), relatedObjects...), nil
}

func (r *OperatorPolicyReconciler) handleCRDs(
	ctx context.Context,
	policy *policyv1beta1.OperatorPolicy,
	sub *operatorv1alpha1.Subscription,
) ([]metav1.Condition, bool, error) {
	if sub == nil {
		return nil, updateStatus(policy, noCRDCond, noExistingCRDObj), nil
	}

	watcher := opPolIdentifier(policy.Namespace, policy.Name)
	selector := subLabelSelector(sub)

	crdList, err := r.DynamicWatcher.List(watcher, customResourceDefinitionGVK, sub.Namespace, selector)
	if err != nil {
		return nil, false, fmt.Errorf("error listing CRDs: %w", err)
	}

	// Same condition for musthave and mustnothave
	if len(crdList) == 0 {
		return nil, updateStatus(policy, noCRDCond, noExistingCRDObj), nil
	}

	relatedCRDs := make([]policyv1.RelatedObject, 0, len(crdList))

	if policy.Spec.ComplianceType.IsMustHave() {
		for i := range crdList {
			relatedCRDs = append(relatedCRDs, matchedObj(&crdList[i]))
		}

		return nil, updateStatus(policy, crdFoundCond, relatedCRDs...), nil
	}

	if policy.Spec.RemovalBehavior.ApplyDefaults().CRDs.IsKeep() {
		for i := range crdList {
			relatedCRDs = append(relatedCRDs, leftoverObj(&crdList[i]))
		}

		return nil, updateStatus(policy, keptCond("CustomResourceDefinition"), relatedCRDs...), nil
	}

	for i := range crdList {
		relatedCRDs = append(relatedCRDs, foundNotWantedObj(&crdList[i]))
	}

	changed := updateStatus(policy, foundNotWantedCond("CustomResourceDefinition"), relatedCRDs...)

	if policy.Spec.RemediationAction.IsInform() {
		return nil, changed, nil
	}

	earlyConds := []metav1.Condition{}

	if changed {
		earlyConds = append(earlyConds, calculateComplianceCondition(policy))
	}

	anyAlreadyDeleting := false

	for i := range crdList {
		if crdList[i].GetDeletionTimestamp() != nil {
			anyAlreadyDeleting = true
			relatedCRDs[i] = deletingObj(&crdList[i])

			// Add a watch specifically for this CRD: the existing watch uses a label selector,
			// and does not necessarily get notified events when the object is fully removed.
			_, err := r.DynamicWatcher.Get(watcher, customResourceDefinitionGVK, sub.Namespace, crdList[i].GetName())
			if err != nil {
				return earlyConds, changed, fmt.Errorf("error watching the deleting CRD: %w", err)
			}

			continue
		}

		err := r.Delete(ctx, &crdList[i])
		if err != nil {
			changed := updateStatus(policy, foundNotWantedCond("CustomResourceDefinition"), relatedCRDs...)

			if anyAlreadyDeleting {
				// reset the "early" conditions to avoid flapping.
				earlyConds = []metav1.Condition{}
			}

			return earlyConds, changed, fmt.Errorf("error deleting the CRD: %w", err)
		}

		crdList[i].SetGroupVersionKind(customResourceDefinitionGVK)
		relatedCRDs[i] = deletedObj(&crdList[i])
	}

	if anyAlreadyDeleting {
		// reset the "early" conditions to avoid flapping.
		earlyConds = []metav1.Condition{}

		return earlyConds, updateStatus(policy, deletingCond("CustomResourceDefinition"), relatedCRDs...), nil
	}

	updateStatus(policy, deletedCond("CustomResourceDefinition"), relatedCRDs...)

	return earlyConds, true, nil
}

func (r *OperatorPolicyReconciler) handleCatalogSource(
	policy *policyv1beta1.OperatorPolicy,
	subscription *operatorv1alpha1.Subscription,
) (bool, error) {
	watcher := opPolIdentifier(policy.Namespace, policy.Name)

	if subscription == nil {
		// Note: existing related objects will not be removed by this status update
		if policy.Spec.ComplianceType.IsMustHave() {
			return updateStatus(policy, invalidCausingUnknownCond("CatalogSource")), nil
		}

		// CatalogSource may be available
		// related objects will remain the same to report last known state
		cond := notApplicableCond("CatalogSource")
		cond.Status = metav1.ConditionFalse

		return updateStatus(policy, cond), nil
	}

	catalogName := subscription.Spec.CatalogSource
	catalogNS := subscription.Spec.CatalogSourceNamespace

	// Check if CatalogSource exists
	foundCatalogSrc, err := r.DynamicWatcher.Get(watcher, catalogSrcGVK,
		catalogNS, catalogName)
	if err != nil {
		return false, fmt.Errorf("error getting CatalogSource: %w", err)
	}

	var catalogSrc *operatorv1alpha1.CatalogSource

	if foundCatalogSrc != nil {
		// CatalogSource is found, initiate health check
		catalogSrc = new(operatorv1alpha1.CatalogSource)

		err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(foundCatalogSrc.Object, catalogSrc)
		if err != nil {
			return false, fmt.Errorf("error converting the retrieved CatalogSource to the Go type: %w", err)
		}
	}

	if policy.Spec.ComplianceType.IsMustNotHave() {
		return r.mustnothaveCatalogSource(policy, catalogSrc, catalogName, catalogNS)
	}

	return r.musthaveCatalogSource(policy, catalogSrc, catalogName, catalogNS)
}

func (r *OperatorPolicyReconciler) mustnothaveCatalogSource(
	policy *policyv1beta1.OperatorPolicy,
	catalogSrc *operatorv1alpha1.CatalogSource,
	catalogName string,
	catalogNS string,
) (bool, error) {
	var relObj policyv1.RelatedObject

	cond := notApplicableCond("CatalogSource")
	cond.Status = metav1.ConditionFalse // CatalogSource condition has the opposite polarity

	if catalogSrc == nil {
		relObj = missingObj(catalogName, catalogNS, policyv1.MustNotHave, catalogSrcGVK)
	} else {
		relObj = foundNotApplicableObj(catalogSrc)
	}

	return updateStatus(policy, cond, relObj), nil
}

func (r *OperatorPolicyReconciler) musthaveCatalogSource(
	policy *policyv1beta1.OperatorPolicy,
	catalogSrc *operatorv1alpha1.CatalogSource,
	catalogName string,
	catalogNS string,
) (bool, error) {
	isMissing := catalogSrc == nil
	isUnhealthy := isMissing

	if catalogSrc != nil {
		// CatalogSource is found, initiate health check
		if catalogSrc.Status.GRPCConnectionState == nil {
			// Unknown State
			changed := updateStatus(
				policy,
				catalogSourceUnknownCond,
				catalogSrcUnknownObj(catalogSrc.Name, catalogSrc.Namespace),
			)

			return changed, nil
		}

		CatalogSrcState := catalogSrc.Status.GRPCConnectionState.LastObservedState
		isUnhealthy = (CatalogSrcState != CatalogSourceReady)
	}

	changed := updateStatus(policy, catalogSourceFindCond(isUnhealthy, isMissing, catalogName),
		catalogSourceObj(catalogName, catalogNS, isUnhealthy, isMissing))

	return changed, nil
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

// subLabelSelector returns a selector that matches a label that OLM adds to resources
// that are related to a Subscription. It can be used to find those resources even
// after the Subscription or CSV is deleted.
func subLabelSelector(sub *operatorv1alpha1.Subscription) labels.Selector {
	sel, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      fmt.Sprintf("operators.coreos.com/%v.%v", sub.Name, sub.Namespace),
			Operator: metav1.LabelSelectorOpExists,
		}},
	})
	if err != nil {
		panic(err)
	}

	return sel
}
