// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/go-logr/logr"
	gocmp "github.com/google/go-cmp/cmp"
	templates "github.com/stolostron/go-template-utils/v6/pkg/templates"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	apimachineryerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	kubeopenapivalidation "k8s.io/kube-openapi/pkg/util/proto/validation"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	yaml "sigs.k8s.io/yaml"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	common "open-cluster-management.io/config-policy-controller/pkg/common"
)

const (
	ControllerName             string = "configuration-policy-controller"
	CRDName                    string = "configurationpolicies.policy.open-cluster-management.io"
	pruneObjectFinalizer       string = "policy.open-cluster-management.io/delete-related-objects"
	disableTemplatesAnnotation string = "policy.open-cluster-management.io/disable-templates"
)

var log = ctrl.Log.WithName(ControllerName)

// PlcChan a channel used to pass policies ready for update
var PlcChan chan *policyv1.ConfigurationPolicy

var (
	eventNormal  = "Normal"
	eventWarning = "Warning"
	eventFmtStr  = "policy: %s/%s"
)

const (
	reasonWantFoundExists    = "Resource found as expected"
	reasonWantFoundCreated   = "K8s creation success"
	reasonUpdateSuccess      = "K8s update success"
	reasonDeleteSuccess      = "K8s deletion success"
	reasonWantFoundNoMatch   = "Resource found but does not match"
	reasonWantFoundDNE       = "Resource not found but should exist"
	reasonWantNotFoundExists = "Resource found but should not exist"
	reasonWantNotFoundDNE    = "Resource not found as expected"
	reasonCleanupError       = "Error cleaning up child objects"
	reasonFoundNotApplicable = "Resource found but will not be handled in mustnothave mode"
)

var ErrPolicyInvalid = errors.New("the Policy is invalid")

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigurationPolicyReconciler) SetupWithManager(
	mgr ctrl.Manager, evaluationConcurrency uint8, rawSources ...*source.Channel,
) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: int(evaluationConcurrency),
		}).
		For(&policyv1.ConfigurationPolicy{}, builder.WithPredicates(
			predicate.Funcs{
				// Skip most pure status/metadata updates
				UpdateFunc: func(e event.UpdateEvent) bool {
					if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
						return true
					}

					if !e.ObjectNew.GetDeletionTimestamp().IsZero() {
						return true
					}

					oldAnnotations := e.ObjectOld.GetAnnotations()
					newAnnotations := e.ObjectNew.GetAnnotations()

					// These are the options that change evaluation behavior that aren't in the spec.
					if oldAnnotations[disableTemplatesAnnotation] != newAnnotations[disableTemplatesAnnotation] ||
						oldAnnotations[IVAnnotation] != newAnnotations[IVAnnotation] {
						return true
					}

					oldTyped, ok := e.ObjectOld.(*policyv1.ConfigurationPolicy)
					if !ok {
						return false
					}

					newTyped, ok := e.ObjectNew.(*policyv1.ConfigurationPolicy)
					if !ok {
						return false
					}

					// Handle the case where compliance was explicitly reset by the governance-policy-framework
					return oldTyped.Status.ComplianceState != "" && newTyped.Status.ComplianceState == ""
				},
				DeleteFunc: func(event event.DeleteEvent) bool {
					// This is the only place to detect a deletion event and still get the policy UID, so it's a bit
					// of a hack but it works.
					r.lastEvaluatedCache.Delete(event.Object.GetUID())
					r.processedPolicyCache.Delete(event.Object.GetUID())

					return true
				},
			},
		))

	for _, rawSource := range rawSources {
		if rawSource != nil {
			builder = builder.WatchesRawSource(rawSource, &handler.EnqueueRequestForObject{})
		}
	}

	return builder.Complete(r)
}

// blank assignment to verify that ConfigurationPolicyReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &ConfigurationPolicyReconciler{}

// ConfigurationPolicyReconciler reconciles a ConfigurationPolicy object
type ConfigurationPolicyReconciler struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	DecryptionConcurrency uint8
	DynamicWatcher        depclient.DynamicWatcher
	Scheme                *runtime.Scheme
	Recorder              record.EventRecorder
	// processedPolicyCache has the ConfigurationPolicy UID as the key and the values are a *sync.Map with the keys
	// as object UIDs and the values as cachedEvaluationResult objects.
	processedPolicyCache sync.Map
	InstanceName         string
	// The Kubernetes client to use when evaluating/enforcing policies. Most times, this will be the same cluster
	// where the controller is running.
	TargetK8sClient        kubernetes.Interface
	TargetK8sDynamicClient dynamic.Interface
	TargetK8sConfig        *rest.Config
	SelectorReconciler     common.SelectorReconciler
	// Whether custom metrics collection is enabled
	EnableMetrics bool
	ServerVersion string
	// This is used to fetch and parse OpenAPI documents to perform client-side validation of object definitions.
	openAPIParser              *openapi.CachedOpenAPIParser
	openAPIParserLastRefreshed time.Time
	// A lock when performing actions that are not thread safe (i.e. reassigning object properties).
	lock sync.RWMutex
	// When true, the controller has detected it is being uninstalled and only basic cleanup should be performed before
	// exiting.
	UninstallMode bool
	// The number of seconds before a policy is eligible for reevaluation in watch mode (throttles frequently evaluated
	// policies)
	EvalBackoffSeconds uint
	// lastEvaluatedCache contains the value of ConfigurationPolicyStatus.LastEvaluated per ConfigurationPolicy UID.
	// This is a workaround to account for race conditions where the status is updated but the controller-runtime cache
	// has not updated yet.
	lastEvaluatedCache sync.Map
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile is responsible for evaluating and rescheduling ConfigurationPolicy evaluations.
func (r *ConfigurationPolicyReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	log := log.WithValues("name", request.Name, "namespace", request.Namespace)
	policy := &policyv1.ConfigurationPolicy{}

	err := r.Get(ctx, request.NamespacedName, policy)
	if k8serrors.IsNotFound(err) {
		log.V(1).Info("Handling a deleted policy")
		removeConfigPolicyMetrics(request)
		r.SelectorReconciler.Stop(request.Namespace, request.Name)

		objID := depclient.ObjectIdentifier{
			Group:     policyv1.GroupVersion.Group,
			Version:   policyv1.GroupVersion.Version,
			Kind:      "ConfigurationPolicy",
			Namespace: request.Namespace,
			Name:      request.Name,
		}

		err := r.DynamicWatcher.RemoveWatcher(objID)
		if err != nil {
			log.Error(err, "Failed to remove any watches from this deleted ConfigurationPolicy. Will ignore.")
		}

		return reconcile.Result{}, nil
	}

	if err != nil {
		return reconcile.Result{}, err
	}

	// Account for a change in evaluation interval either due to a spec change or compliance state change.
	defer func() {
		if policy.Spec == nil {
			// The watch is removed in the error handling of handleObjectTemplates in the Reconcile method
			return
		}

		compliantWithWatch := policy.Status.ComplianceState == policyv1.Compliant &&
			policy.Spec.EvaluationInterval.IsWatchForCompliant()
		nonCompliantWithWatch := policy.Status.ComplianceState != policyv1.Compliant &&
			policy.Spec.EvaluationInterval.IsWatchForNonCompliant()

		if !(compliantWithWatch || nonCompliantWithWatch) {
			err := r.DynamicWatcher.RemoveWatcher(policy.ObjectIdentifier())
			if err != nil {
				log.Error(err, "Failed to remove any watches related to this ConfigurationPolicy. Will ignore.")
			}
		}
	}()

	uninstalling, crdDeleting, err := r.cleanupImmediately()
	if !uninstalling && !crdDeleting && err != nil {
		log.Error(err, "Failed to determine if it's time to cleanup immediately")

		return reconcile.Result{}, err
	}

	// If the ConfigurationPolicy's spec field was updated, clear the cache of evaluated objects.
	if policy.Status.LastEvaluatedGeneration != policy.Generation {
		r.processedPolicyCache.Delete(policy.GetUID())
	}

	shouldEvaluate, durationLeft := r.shouldEvaluatePolicy(policy, (uninstalling || crdDeleting))
	if !shouldEvaluate {
		// Requeue based on the remaining time for the evaluation interval to be met.
		return reconcile.Result{RequeueAfter: durationLeft}, nil
	}

	before := time.Now().UTC()

	handleErr := r.handleObjectTemplates(policy)

	duration := time.Now().UTC().Sub(before)
	seconds := float64(duration) / float64(time.Second)

	policyEvalSecondsCounter.WithLabelValues(policy.Name).Add(seconds)
	policyEvalCounter.WithLabelValues(policy.Name).Inc()

	if handleErr != nil {
		// If the policy is invalid, don't bother requeueing since we need to wait for a spec change.
		if errors.Is(handleErr, ErrPolicyInvalid) {
			// Remove any watches on the policy in case the policy used to be valid and specified watches.
			err := r.DynamicWatcher.RemoveWatcher(policy.ObjectIdentifier())
			if err != nil {
				log.Error(err, "Failed to remove any watches related to this ConfigurationPolicy. Will ignore.")
			}

			return reconcile.Result{}, nil
		}

		// If a mapping error occurred, try again in 10 seconds to see if the CRD is available
		if errors.Is(handleErr, depclient.ErrNoVersionedResource) &&
			policy.Spec != nil &&
			policy.Spec.EvaluationInterval.IsWatchForNonCompliant() {
			log.Info("Requeuing the policy to be reevalauted in 10 seconds due to a mapping error")

			return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
		}

		return reconcile.Result{}, handleErr
	}

	var requeueAfter time.Duration
	var getIntervalErr error

	if policy.Status.ComplianceState == policyv1.Compliant {
		if policy.Spec.EvaluationInterval.IsWatchForCompliant() {
			log.V(2).Info("The policy is compliant and has the evaluation interval set to watch. Will not schedule.")

			return reconcile.Result{}, nil
		}

		requeueAfter, getIntervalErr = policy.Spec.EvaluationInterval.GetCompliantInterval()
	} else {
		// If the policy is not compliant (i.e. noncompliant or unknown), fall back to the noncompliant evaluation
		// interval. This is a court of guilty until proven innocent.
		if policy.Spec.EvaluationInterval.IsWatchForNonCompliant() {
			log.V(2).Info(
				"The policy is not compliant and has the evaluation interval set to watch. Will not schedule.",
			)

			return reconcile.Result{}, nil
		}

		requeueAfter, getIntervalErr = policy.Spec.EvaluationInterval.GetNonCompliantInterval()
	}

	// At this point, we know the evaluation interval isn't set to watch so remove any potential watches for this
	// policy.
	removeWatcherErr := r.DynamicWatcher.RemoveWatcher(policy.ObjectIdentifier())
	if removeWatcherErr != nil {
		log.Error(err, "Failed to remove any watches related to this ConfigurationPolicy. Will ignore.")
	}

	if getIntervalErr != nil {
		if errors.Is(getIntervalErr, policyv1.ErrIsNever) {
			log.V(2).Info(
				"The policy will not be scheduled for evaluation since it has an evaluation interval of never",
			)

			return reconcile.Result{}, nil
		}

		log.Error(
			getIntervalErr,
			"The policy has an invalid evaluation interval; defaulting to 10s",
		)

		requeueAfter = 10 * time.Second
	}

	var requeueNow bool

	// Account for an evaluation interval of 0s.
	if requeueAfter <= 0 {
		requeueAfter = 0
		requeueNow = true
	}

	log.V(2).Info("The policy has a scheduled next evaluation", "untilNextEvaluation", requeueAfter.String())

	return reconcile.Result{RequeueAfter: requeueAfter, Requeue: requeueNow}, nil
}

// shouldEvaluatePolicy will determine if the policy is ready for evaluation by examining the
// status.lastEvaluated and status.lastEvaluatedGeneration fields. If a policy has been updated, it
// will always be triggered for evaluation. If the spec.evaluationInterval configuration has been
// met, then that will also trigger an evaluation. If cleanupImmediately is true, then only policies
// with finalizers will be ready for evaluation regardless of the last evaluation.
// cleanupImmediately should be set true when the controller is getting uninstalled.
// If the policy is not ready to be evaluated, the returned duration is how long until the next evaluation.
func (r *ConfigurationPolicyReconciler) shouldEvaluatePolicy(
	policy *policyv1.ConfigurationPolicy, cleanupImmediately bool,
) (bool, time.Duration) {
	log := log.WithValues("policy", policy.GetName())

	// If it's time to clean up such as when the config-policy-controller is being uninstalled, only evaluate policies
	// with a finalizer to remove the finalizer.
	if cleanupImmediately {
		return len(policy.Finalizers) != 0, 0
	}

	if cachedLastEval, ok := r.lastEvaluatedCache.Load(policy.UID); ok {
		if cachedLastEval.(string) != policy.Status.LastEvaluated {
			log.V(1).Info(
				"The policy's status.lastEvaluated field is not synced in the controller-runtime cache. Will requeue.",
			)

			return false, time.Second
		}
	}

	if policy.ObjectMeta.DeletionTimestamp != nil {
		log.V(1).Info("The policy has been deleted and is waiting for object cleanup. Will evaluate it now.")

		return true, 0
	}

	if policy.Status.LastEvaluated == "" {
		log.V(1).Info("The policy's status.lastEvaluated field is not set. Will evaluate it now.")

		return true, 0
	}

	if policy.Status.LastEvaluatedGeneration != policy.Generation {
		log.V(1).Info(
			"The policy has been updated. Will evaluate it now.",
			"generation", policy.Generation,
			"lastEvaluatedGeneration", policy.Status.LastEvaluatedGeneration,
		)

		return true, 0
	}

	// If there was a timeout during a recreate, immediately evaluate the policy regardless of the evaluation interval.
	if policy.Status.ComplianceState == policyv1.NonCompliant {
		for _, details := range policy.Status.CompliancyDetails {
			for _, condition := range details.Conditions {
				if condition.Reason == "K8s update template error" && strings.Contains(
					condition.Message, "timed out waiting for the object to delete during recreate",
				) {
					return true, 0
				}
			}
		}
	}

	lastEvaluated, err := time.Parse(time.RFC3339, policy.Status.LastEvaluated)
	if err != nil {
		log.Error(err, "The policy has an invalid status.lastEvaluated value. Will evaluate it now.")

		return true, 0
	}

	usesSelector := policy.Spec.NamespaceSelector.MatchLabels != nil ||
		policy.Spec.NamespaceSelector.MatchExpressions != nil ||
		len(policy.Spec.NamespaceSelector.Include) != 0

	if usesSelector && r.SelectorReconciler.HasUpdate(policy.Namespace, policy.Name) {
		log.V(1).Info("There was an update for this policy's namespaces. Will evaluate it now.")

		return true, 0
	}

	var interval time.Duration
	var getIntervalErr error

	if policy.Status.ComplianceState == policyv1.Compliant && policy.Spec != nil {
		interval, getIntervalErr = policy.Spec.EvaluationInterval.GetCompliantInterval()
	} else if policy.Status.ComplianceState == policyv1.NonCompliant && policy.Spec != nil {
		interval, getIntervalErr = policy.Spec.EvaluationInterval.GetNonCompliantInterval()
	} else {
		log.V(1).Info("The policy has an unknown compliance. Will evaluate it now.")

		return true, 0
	}

	now := time.Now().UTC()

	if errors.Is(getIntervalErr, policyv1.ErrIsNever) {
		log.V(1).Info("Skipping the policy evaluation due to the spec.evaluationInterval value being set to never")

		return false, 0
	} else if errors.Is(getIntervalErr, policyv1.ErrIsWatch) {
		minNextEval := lastEvaluated.Add(time.Second * time.Duration(r.EvalBackoffSeconds))
		durationLeft := minNextEval.Sub(now)

		if durationLeft > 0 {
			log.V(1).Info(
				"The policy evaluation is configured for a watch event but rescheduling the evaluation due to the "+
					"configured evaluation backoff",
				"evaluationBackoffSeconds", r.EvalBackoffSeconds,
				"remainingSeconds", math.Round(durationLeft.Seconds()),
			)

			return false, durationLeft
		}

		log.V(1).Info("The policy evaluation is configured for a watch event. Will evaluate now.")

		return true, 0
	} else if getIntervalErr != nil {
		log.Error(
			getIntervalErr,
			"The policy has an invalid spec.evaluationInterval value; defaulting to watch",
			"spec.evaluationInterval.compliant", policy.Spec.EvaluationInterval.Compliant,
			"spec.evaluationInterval.noncompliant", policy.Spec.EvaluationInterval.NonCompliant,
		)

		return true, 0
	}

	nextEvaluation := lastEvaluated.Add(interval)
	durationLeft := nextEvaluation.Sub(now)

	if durationLeft > 0 {
		log.V(1).Info("Skipping the policy evaluation due to the policy not reaching the evaluation interval")

		return false, durationLeft
	}

	return true, 0
}

// cleanUpChildObjects conditionally removed child objects that are no longer referenced in the
// `newRelated` list, compared to what is currently in the policy. It does not delete anything in
// inform mode, and it obeys the pruneObjectBehavior setting.
func (r *ConfigurationPolicyReconciler) cleanUpChildObjects(
	plc *policyv1.ConfigurationPolicy, newRelated []policyv1.RelatedObject, usingWatch bool,
) []string {
	deletionFailures := []string{}

	if !plc.Spec.RemediationAction.IsEnforce() {
		return deletionFailures
	}

	// PruneObjectBehavior = none case fall in here
	if !(string(plc.Spec.PruneObjectBehavior) == "DeleteAll" ||
		string(plc.Spec.PruneObjectBehavior) == "DeleteIfCreated") {
		return deletionFailures
	}

	objsToDelete := plc.Status.RelatedObjects

	// When spec is updated and new related objects are created
	if len(newRelated) != 0 {
		var objShouldRemoved []policyv1.RelatedObject

		for _, oldR := range objsToDelete {
			if !containRelated(newRelated, oldR) {
				objShouldRemoved = append(objShouldRemoved, oldR)
			}
		}

		objsToDelete = objShouldRemoved
	}

	for _, object := range objsToDelete {
		// set up client for object deletion
		gvk := schema.FromAPIVersionAndKind(object.Object.APIVersion, object.Object.Kind)

		log := log.WithValues("policy", plc.GetName(), "groupVersionKind", gvk.String())

		scopedGVR, err := r.DynamicWatcher.GVKToGVR(gvk)
		if err != nil {
			log.Error(err, "Could not get resource mapping for child object")

			deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
				object.Object.Metadata.Name, object.Object.Metadata.Namespace))

			continue
		}

		// determine whether object should be deleted
		needsDelete := false
		var existing *unstructured.Unstructured

		if usingWatch {
			existing, err = r.DynamicWatcher.Get(
				plc.ObjectIdentifier(),
				gvk,
				object.Object.Metadata.Namespace,
				object.Object.Metadata.Name,
			)
		} else {
			existing, err = getObject(
				object.Object.Metadata.Namespace,
				object.Object.Metadata.Name,
				scopedGVR,
				r.TargetK8sDynamicClient,
			)
		}

		if err != nil {
			log.Error(err, "Failed to get child object")

			deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
				object.Object.Metadata.Name, object.Object.Metadata.Namespace))

			continue
		}

		// object does not exist, no deletion logic needed
		if existing == nil {
			continue
		}

		if string(plc.Spec.PruneObjectBehavior) == "DeleteAll" {
			needsDelete = true
		} else if string(plc.Spec.PruneObjectBehavior) == "DeleteIfCreated" {
			// if prune behavior is DeleteIfCreated, we need to check whether createdByPolicy
			// is true and the UID is not stale
			if object.Properties != nil &&
				object.Properties.CreatedByPolicy != nil &&
				*object.Properties.CreatedByPolicy &&
				object.Properties.UID == string(existing.GetUID()) {
				needsDelete = true
			}
		}

		// delete object if needed
		if needsDelete {
			// if object has already been deleted and is stuck, no need to redo delete request
			_, deletionTimeFound, _ := unstructured.NestedString(existing.Object, "metadata", "deletionTimestamp")
			if deletionTimeFound {
				log.Error(fmt.Errorf("tried to delete object, but delete is hanging"), "Error")

				deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
					object.Object.Metadata.Name, object.Object.Metadata.Namespace))

				continue
			}

			var res dynamic.ResourceInterface
			if scopedGVR.Namespaced {
				res = r.TargetK8sDynamicClient.Resource(scopedGVR.GroupVersionResource).Namespace(
					object.Object.Metadata.Namespace,
				)
			} else {
				res = r.TargetK8sDynamicClient.Resource(scopedGVR.GroupVersionResource)
			}

			if completed, err := deleteObject(res, object.Object.Metadata.Name,
				object.Object.Metadata.Namespace); !completed {
				deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
					object.Object.Metadata.Name, object.Object.Metadata.Namespace))

				log.Error(err, "Error: Failed to delete object during child object pruning")
			} else {
				// Don't use the cache here to avoid race conditions since this is to verify that the deletion was
				// successful. The cache is dependent on the watch updating.
				obj, _ := getObject(
					object.Object.Metadata.Namespace,
					object.Object.Metadata.Name,
					scopedGVR,
					r.TargetK8sDynamicClient,
				)

				if obj != nil {
					log.Error(err, "Error: tried to delete object, but delete is hanging")

					deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
						object.Object.Metadata.Name, object.Object.Metadata.Namespace))

					continue
				}

				log.Info("Object successfully deleted as part of child object pruning or detached objects")
			}
		}
	}

	return deletionFailures
}

// cleanupImmediately returns true (i.e. beingUninstalled or crdDeleting) when the cluster is in a state where
// configurationpolicies should be removed as soon as possible, ignoring the pruneObjectBehavior of the policies. This
// is the case when the controller is being uninstalled or the CRD is being deleted.
func (r *ConfigurationPolicyReconciler) cleanupImmediately() (beingUninstalled bool, crdDeleting bool, err error) {
	var beingUninstalledErr error

	beingUninstalled, beingUninstalledErr = IsBeingUninstalled(r.Client)

	var defErr error

	crdDeleting, defErr = r.definitionIsDeleting()

	if beingUninstalledErr != nil && defErr != nil {
		err = fmt.Errorf("%w; %w", beingUninstalledErr, defErr)
	} else if beingUninstalledErr != nil {
		err = beingUninstalledErr
	} else if defErr != nil {
		err = defErr
	}

	return
}

func (r *ConfigurationPolicyReconciler) definitionIsDeleting() (bool, error) {
	key := types.NamespacedName{Name: CRDName}
	v1def := extensionsv1.CustomResourceDefinition{}

	v1err := r.Get(context.TODO(), key, &v1def)
	if v1err == nil {
		return (v1def.ObjectMeta.DeletionTimestamp != nil), nil
	}

	v1beta1def := extensionsv1beta1.CustomResourceDefinition{}

	v1beta1err := r.Get(context.TODO(), key, &v1beta1def)
	if v1beta1err == nil {
		return (v1beta1def.DeletionTimestamp != nil), nil
	}

	// It might not be possible to get a not-found on the CRD while reconciling the CR...
	// But in that case, it seems reasonable to still consider it "deleting"
	if k8serrors.IsNotFound(v1err) || k8serrors.IsNotFound(v1beta1err) {
		return true, nil
	}

	// Both had unexpected errors, return them and retry later
	return false, fmt.Errorf("v1: %v, v1beta1: %v", v1err, v1beta1err) //nolint:errorlint
}

// currentlyUsingWatch determines if the dynamic watcher should be used based on
// the current compliance and the evaluation interval settings.
func currentlyUsingWatch(plc *policyv1.ConfigurationPolicy) bool {
	if plc.Status.ComplianceState == policyv1.Compliant {
		return plc.Spec.EvaluationInterval.IsWatchForCompliant()
	}

	// If the policy is not compliant (i.e. noncompliant or unknown), fall back to the noncompliant
	// evaluation interval. This is a court of guilty until proven innocent.
	return plc.Spec.EvaluationInterval.IsWatchForNonCompliant()
}

// handleObjectTemplates iterates through all policy templates in a given policy and processes them. If fields are
// missing on the policy (excluding objectDefinition), an error of type ErrPolicyInvalid is returned.
func (r *ConfigurationPolicyReconciler) handleObjectTemplates(plc *policyv1.ConfigurationPolicy) error {
	log := log.WithValues("policy", plc.GetName())
	log.V(1).Info("Processing object templates")

	if err := r.validateConfigPolicy(plc); err != nil {
		return err
	}

	if returnNow, err := r.manageDeletionFinalizer(plc); err != nil || returnNow {
		return err
	}

	usingWatch := currentlyUsingWatch(plc)

	if usingWatch && r.DynamicWatcher != nil {
		watcherObj := plc.ObjectIdentifier()

		err := r.DynamicWatcher.StartQueryBatch(watcherObj)
		if err != nil {
			log.Error(
				err,
				"Failed to start a query batch using the dynamic watcher. Will try again on the next evaluation.",
				"watcher", watcherObj,
			)

			return err
		}

		defer func() {
			err := r.DynamicWatcher.EndQueryBatch(watcherObj)
			if err != nil {
				log.Error(err, "Failed to stop the query batch using the dynamic watcher", "watcher", watcherObj)
			}
		}()
	}

	if plc.ObjectMeta.DeletionTimestamp != nil {
		return r.handleDeletion(plc, usingWatch)
	}

	disableTemplates := false

	if disableAnnotation, ok := plc.Annotations[disableTemplatesAnnotation]; ok {
		log.V(2).Info("Found disable-templates annotation", "value", disableAnnotation)

		parsedDisable, err := strconv.ParseBool(disableAnnotation)
		if err != nil {
			log.Error(err, "Could not parse value for disable-templates annotation", "value", disableAnnotation)
		} else {
			disableTemplates = parsedDisable
		}
	}

	parentStatusUpdateNeeded := false

	if !disableTemplates {
		updateNeeded, tmplErr := r.handleTemplatization(plc, usingWatch)
		if tmplErr != nil {
			return tmplErr
		}

		parentStatusUpdateNeeded = updateNeeded
	}

	relatedObjects := []policyv1.RelatedObject{}

	if len(plc.Spec.ObjectTemplates) == 0 {
		reason := "No object templates"
		msg := fmt.Sprintf("%v contains no object templates to check, and thus has no violations",
			plc.GetName())

		statusUpdateNeeded := addConditionToStatus(plc, -1, true, reason, msg)

		if statusUpdateNeeded {
			r.recordInfoEvent(plc, false)
		}

		updatedRelated := r.updatedRelatedObjects(plc, relatedObjects)
		if !gocmp.Equal(updatedRelated, plc.Status.RelatedObjects) {
			r.cleanUpChildObjects(plc, updatedRelated, usingWatch)

			plc.Status.RelatedObjects = updatedRelated
		}

		r.addForUpdate(plc, statusUpdateNeeded)

		return nil
	}

	var mappingErr error

	for index, objectT := range plc.Spec.ObjectTemplates {
		nsToResults := map[string]objectTmplEvalResult{}
		desiredObj, scopedGVR, relevantNamespaces, errEvent, mapErr := r.determineDesiredObject(plc, index, objectT)

		// Return all mapping errors encountered and let the caller decide if the errors should be retried
		mappingErr = errors.Join(mappingErr, mapErr)

		for _, ns := range relevantNamespaces {
			log.V(1).Info("Handling the object template for the relevant namespace",
				"namespace", ns, "desiredName", desiredObj.GetName(), "index", index)

			if errEvent != nil {
				nsToResults[ns] = objectTmplEvalResult{
					events: []objectTmplEvalEvent{*errEvent},
				}

				continue
			}

			related, result := r.handleObjects(objectT, ns, desiredObj, index, plc, scopedGVR, usingWatch)

			nsToResults[ns] = result

			for _, object := range related {
				relatedObjects = addOrUpdateRelatedObject(relatedObjects, object)
			}
		}

		eventBatches := batchedEvents(nsToResults)

		resourceName := scopedGVR.Resource
		if resourceName == "" {
			// Fallback to the kind in the object, if the scopedGVR wasn't populated.
			resourceName = desiredObj.GetKind()
		}

		// If there are multiple batches, check if the last batch is noncompliant and is the current state. If so,
		// skip status updating and event generation. This is required to avoid an infinite loop of status updating
		// when there is an error. In the case it's compliant, it's likely that some other process that is also
		// updating the object and the ConfigurationPolicy has to constantly update it. We want to generate a
		// status in this case.
		if len(eventBatches) > 1 {
			lastBatch := eventBatches[len(eventBatches)-1]

			compliant, reason, msg := createStatus(resourceName, lastBatch)

			if !compliant {
				statusUpdateNeeded := addConditionToStatus(plc.DeepCopy(), index, compliant, reason, msg)

				if !statusUpdateNeeded {
					log.V(2).Info("Skipping status update because the last batch already matches")

					continue
				}
			}
		}

		for i, batch := range eventBatches {
			compliant, reason, msg := createStatus(resourceName, batch)

			statusUpdateNeeded := addConditionToStatus(plc, index, compliant, reason, msg)

			if statusUpdateNeeded {
				parentStatusUpdateNeeded = true

				// The event is always sent at the end, so skip sending it in the final batch
				if i == len(eventBatches)-1 {
					break
				}

				log.Info("Sending an update policy status event for the object template",
					"policy", plc.Name, "index", index)
				r.addForUpdate(plc, true)
			}
		}
	}

	updatedRelated := r.updatedRelatedObjects(plc, relatedObjects)
	if !gocmp.Equal(updatedRelated, plc.Status.RelatedObjects) {
		r.cleanUpChildObjects(plc, updatedRelated, usingWatch)

		plc.Status.RelatedObjects = updatedRelated
	}

	r.addForUpdate(plc, parentStatusUpdateNeeded)

	return mappingErr
}

// validateConfigPolicy returns an error and increments the "invalid-template" error counter metric
// if the configuration is invalid.
func (r *ConfigurationPolicyReconciler) validateConfigPolicy(plc *policyv1.ConfigurationPolicy) error {
	log := log.WithValues("policy", plc.GetName())

	var invalidMessage string

	if plc.Spec == nil {
		invalidMessage = "Policy does not have a Spec specified"
	} else if plc.Spec.RemediationAction == "" {
		invalidMessage = "Policy does not have a RemediationAction specified"
	} else {
		return nil
	}

	log.Info(invalidMessage)
	statusChanged := addConditionToStatus(plc, -1, false, "Invalid spec", invalidMessage)

	if statusChanged {
		r.recordInfoEvent(plc, true)
	}

	// Note: don't change related objects while the policy is invalid

	r.addForUpdate(plc, statusChanged)

	parent := ""
	if len(plc.OwnerReferences) > 0 {
		parent = plc.OwnerReferences[0].Name
	}

	policyUserErrorsCounter.WithLabelValues(parent, plc.GetName(), "invalid-template").Add(1)

	return fmt.Errorf("%w: %s", ErrPolicyInvalid, invalidMessage)
}

// manageDeletionFinalizer sets or removes the finalizer on the ConfigurationPolicy based on the
// pruneObjectBehavior setting, and whether the controller is being uninstalled. If the controller
// is being uninstalled, the function will return true, indicating the rest of the policy handling
// logic should be skipped.
func (r *ConfigurationPolicyReconciler) manageDeletionFinalizer(plc *policyv1.ConfigurationPolicy,
) (returnNow bool, err error) {
	if plc.Spec.PruneObjectBehavior == "DeleteIfCreated" || plc.Spec.PruneObjectBehavior == "DeleteAll" {
		uninstalling, crdDeleting, err := r.cleanupImmediately()
		if !uninstalling && !crdDeleting && err != nil {
			log.Error(err, "Error determining whether to cleanup immediately, requeueing policy")

			return true, err
		}

		if uninstalling || crdDeleting {
			if objHasFinalizer(plc, pruneObjectFinalizer) {
				patch := removeObjFinalizerPatch(plc, pruneObjectFinalizer)

				err = r.Patch(context.TODO(), plc, client.RawPatch(types.JSONPatchType, patch))
				if err != nil {
					log.Error(err, "Error removing finalizer for configuration policy")

					return true, err
				}
			}

			return true, nil
		}

		// set finalizer if it hasn't been set
		if !objHasFinalizer(plc, pruneObjectFinalizer) {
			var patch []byte
			if plc.Finalizers == nil {
				patch = []byte(
					`[{"op":"add","path":"/metadata/finalizers","value":["` + pruneObjectFinalizer + `"]}]`,
				)
			} else {
				patch = []byte(
					`[{"op":"add","path":"/metadata/finalizers/-","value":"` + pruneObjectFinalizer + `"}]`,
				)
			}

			err := r.Patch(context.TODO(), plc, client.RawPatch(types.JSONPatchType, patch))
			if err != nil {
				log.Error(err, "Error setting finalizer for configuration policy")

				return true, err
			}
		}
	} else if objHasFinalizer(plc, pruneObjectFinalizer) {
		// if pruneObjectBehavior is none, no finalizer is needed
		patch := removeObjFinalizerPatch(plc, pruneObjectFinalizer)

		err := r.Patch(context.TODO(), plc, client.RawPatch(types.JSONPatchType, patch))
		if err != nil {
			log.Error(err, "Error removing finalizer for configuration policy")

			return true, err
		}
	}

	return false, nil
}

// handleDeletion cleans up the child objects, based on the pruneObjectBehavior setting. If all of
// the required child objects are fully removed, it will remove the finalizer.
func (r *ConfigurationPolicyReconciler) handleDeletion(plc *policyv1.ConfigurationPolicy, usingWatch bool) error {
	if !(plc.Spec.PruneObjectBehavior == "DeleteIfCreated" || plc.Spec.PruneObjectBehavior == "DeleteAll") {
		return nil
	}

	log := log.WithValues("policy", plc.GetName())

	parentStatusUpdateNeeded := false

	log.Info("Config policy has been deleted, handling child objects")

	failures := r.cleanUpChildObjects(plc, nil, usingWatch)

	if len(failures) == 0 {
		log.Info("Objects have been successfully cleaned up, removing finalizer")

		patch := removeObjFinalizerPatch(plc, pruneObjectFinalizer)

		err := r.Patch(context.TODO(), plc, client.RawPatch(types.JSONPatchType, patch))
		if err != nil {
			log.Error(err, "Error removing finalizer for configuration policy")

			return err
		}
	} else {
		log.Info("Object cleanup failed, some objects have not been deleted from the cluster")

		failuresStr := strings.Join(failures, ", ")

		statusChanged := addConditionToStatus(plc, -1, false, reasonCleanupError,
			"Failed to delete objects: "+failuresStr)
		if statusChanged {
			parentStatusUpdateNeeded = true

			r.recordInfoEvent(plc, true)
		}

		// Note: don't change related objects while deletion is in progress

		r.addForUpdate(plc, parentStatusUpdateNeeded)

		return fmt.Errorf("failed to delete objects %s", failuresStr)
	}

	return nil
}

// handleTemplatization sets the `plc.Spec.ObjectTemplates` after resolving any templatized values.
func (r *ConfigurationPolicyReconciler) handleTemplatization(
	plc *policyv1.ConfigurationPolicy, usingWatch bool,
) (parentStatusUpdateNeeded bool, err error) {
	log := log.WithValues("policy", plc.GetName())

	if r.EnableMetrics {
		startTime := time.Now().UTC()

		defer func() {
			durationSeconds := time.Since(startTime).Seconds()
			plcTempsProcessSecondsCounter.WithLabelValues(plc.GetName()).Add(durationSeconds)
			plcTempsProcessCounter.WithLabelValues(plc.GetName()).Inc()
		}()
	}

	relatedObjects := []policyv1.RelatedObject{}

	addTemplateErrorViolation := func(reason, msg string) {
		log.Info("Setting the policy to noncompliant due to a templating error", "error", msg)

		if reason == "" {
			reason = "Error processing template"
		}

		statusChanged := addConditionToStatus(plc, -1, false, reason, msg)
		if statusChanged {
			parentStatusUpdateNeeded = true

			r.recordInfoEvent(plc, true)
		}

		r.updatedRelatedObjects(plc, relatedObjects)

		// Note: don't clean up child objects when there is a template violation

		r.addForUpdate(plc, parentStatusUpdateNeeded)
	}

	resolveOptions := templates.ResolveOptions{}

	if usesEncryption(plc) {
		var encryptionConfig templates.EncryptionConfig
		var err error

		encryptionConfig, err = r.getEncryptionConfig(context.TODO(), plc)
		if err != nil {
			addTemplateErrorViolation("", err.Error())

			return parentStatusUpdateNeeded, err
		}

		resolveOptions.EncryptionConfig = encryptionConfig
	}

	// set up raw data for template processing
	var rawDataList [][]byte

	isRawObjTemplate := false

	if plc.Spec.ObjectTemplatesRaw != "" {
		rawDataList = [][]byte{[]byte(plc.Spec.ObjectTemplatesRaw)}
		isRawObjTemplate = true
	} else {
		for _, objectT := range plc.Spec.ObjectTemplates {
			rawDataList = append(rawDataList, objectT.ObjectDefinition.Raw)
		}
	}

	resolveOptions.InputIsYAML = isRawObjTemplate

	log.V(2).Info("Processing the object templates", "count", len(plc.Spec.ObjectTemplates))

	var tmplResolver *templates.TemplateResolver

	if usingWatch {
		tmplResolver, err = templates.NewResolverWithDynamicWatcher(
			r.DynamicWatcher, templates.Config{SkipBatchManagement: true},
		)
		objID := plc.ObjectIdentifier()

		resolveOptions.Watcher = &objID
	} else {
		tmplResolver, err = templates.NewResolver(r.TargetK8sConfig, templates.Config{})
	}

	if err != nil {
		return parentStatusUpdateNeeded, err
	}

	var objTemps []*policyv1.ObjectTemplate

	// process object templates for go template usage
	for i, rawData := range rawDataList {
		hasTemplate := templates.HasTemplate(rawData, "", true)

		if !hasTemplate && !isRawObjTemplate {
			continue
		}

		if !hasTemplate {
			// Unmarshal raw template YAML into object as it doesn't need template resolution
			err := yaml.Unmarshal(rawData, &objTemps)
			if err != nil {
				addTemplateErrorViolation("Error parsing the YAML in the object-templates-raw field", err.Error())

				return parentStatusUpdateNeeded, err
			}

			plc.Spec.ObjectTemplates = objTemps

			break
		}

		log.V(1).Info("Processing policy templates")

		// If there's a template, we can't rely on the cache results.
		r.processedPolicyCache.Delete(plc.GetUID())

		resolvedTemplate, tplErr := tmplResolver.ResolveTemplate(rawData, nil, &resolveOptions)
		if tplErr != nil {
			if errors.Is(tplErr, templates.ErrInvalidAESKey) || errors.Is(tplErr, templates.ErrAESKeyNotSet) {
				addTemplateErrorViolation("", `The "policy-encryption-key" Secret contains an invalid AES key`)

				return parentStatusUpdateNeeded, tplErr
			} else if errors.Is(tplErr, templates.ErrInvalidIV) {
				addTemplateErrorViolation("", fmt.Sprintf(
					`The "%s" annotation value is not a valid initialization vector`, IVAnnotation,
				))

				return parentStatusUpdateNeeded, fmt.Errorf("%w: %w", ErrPolicyInvalid, tplErr)
			} else {
				addTemplateErrorViolation("", tplErr.Error())

				return parentStatusUpdateNeeded, tplErr
			}
		}

		// If raw data, only one passthrough is needed, since all the object templates are in it
		if isRawObjTemplate {
			err := json.Unmarshal(resolvedTemplate.ResolvedJSON, &objTemps)
			if err != nil {
				addTemplateErrorViolation("Error unmarshalling raw template", err.Error())

				return parentStatusUpdateNeeded, err
			}

			if resolvedTemplate.HasSensitiveData {
				for i := range objTemps {
					if objTemps[i].RecordDiff == "" {
						log.V(1).Info(
							"Not automatically turning on recordDiff due to templates interacting with sensitive data",
							"objectTemplateIndex", i,
						)

						objTemps[i].RecordDiff = policyv1.RecordDiffCensored
					}
				}
			}

			plc.Spec.ObjectTemplates = objTemps

			break
		}

		if plc.Spec.ObjectTemplates[i].RecordDiff == "" && resolvedTemplate.HasSensitiveData {
			log.V(1).Info(
				"Not automatically turning on recordDiff due to templates interacting with sensitive data",
				"objectTemplateIndex", i,
			)

			plc.Spec.ObjectTemplates[i].RecordDiff = policyv1.RecordDiffCensored
		}

		// Otherwise, set the resolved data for use in further processing
		plc.Spec.ObjectTemplates[i].ObjectDefinition.Raw = resolvedTemplate.ResolvedJSON
	}

	return parentStatusUpdateNeeded, nil
}

// determineDesiredObject decodes the object definition, gets its mapping, and determines which namespaces
// are relevant (using the policy's selector if a namespace is not set in the object definition). If an
// error occurs during this process, it returns an evaluation event with more details about the error.
func (r *ConfigurationPolicyReconciler) determineDesiredObject(
	plc *policyv1.ConfigurationPolicy, index int, objectT *policyv1.ObjectTemplate,
) (
	desiredObj unstructured.Unstructured,
	scopedGVR depclient.ScopedGVR,
	relevantNamespaces []string,
	errEvent *objectTmplEvalEvent,
	mappingErr error,
) {
	log := log.WithValues("policy", plc.GetName())

	_, _, err := unstructured.UnstructuredJSONScheme.Decode(objectT.ObjectDefinition.Raw, nil, &desiredObj)
	if err != nil {
		log.Error(err, "Could not decode the objectDefinition", "index", index)

		errEvent = &objectTmplEvalEvent{
			compliant: false,
			reason:    "K8s decode object definition error",
			message: fmt.Sprintf("Decoding error, please check your policy file!"+
				" Aborting handling the object template at index [%v] in policy `%v` with error = `%v`",
				index, plc.Name, err),
		}
	}

	// strings.TrimSpace() is needed here because a multi-line value will have '\n' in it. This is kept for
	// backwards compatibility.
	desiredObj.SetName(strings.TrimSpace(desiredObj.GetName()))
	desiredObj.SetNamespace(strings.TrimSpace(desiredObj.GetNamespace()))
	desiredObj.SetKind(strings.TrimSpace(desiredObj.GetKind()))

	// map raw object to a resource, generate a violation if resource cannot be found
	if errEvent == nil {
		scopedGVR, err = r.getMapping(desiredObj.GroupVersionKind(), plc, index)
		if err != nil {
			mappingErr = err

			errEvent = &objectTmplEvalEvent{
				compliant: false,
				reason:    "K8s error",
				message:   err.Error(),
			}
		}
	}

	if scopedGVR.Namespaced && desiredObj.GetNamespace() == "" {
		selectedNamespaces, err := r.SelectorReconciler.Get(plc.Namespace, plc.Name, plc.Spec.NamespaceSelector)
		if err != nil {
			log.Error(err, "Failed to select the namespaces",
				"namespaceSelector", fmt.Sprintf("%+v", plc.Spec.NamespaceSelector))

			msg := fmt.Sprintf("Error filtering namespaces with provided namespaceSelector: %v", err)

			// only report this error if there wasn't another yet
			if errEvent == nil {
				errEvent = &objectTmplEvalEvent{
					compliant: false,
					reason:    "namespaceSelector error",
					message:   msg,
				}
			}
		}

		if len(selectedNamespaces) == 0 {
			relevantNamespaces = []string{desiredObj.GetNamespace()}
		} else {
			relevantNamespaces = selectedNamespaces
		}
	} else {
		relevantNamespaces = []string{desiredObj.GetNamespace()}
	}

	return desiredObj, scopedGVR, relevantNamespaces, errEvent, mappingErr
}

// batchedEvents combines compliance events into batches that should be emitted in order. For example,
// if an object didn't match and was enforced, there would be an event that it didn't match in the first
// batch, and then the second batch would be that it was updated successfully.
func batchedEvents(nsToResults map[string]objectTmplEvalResult) (
	eventBatches []map[string]*objectTmplEvalResultWithEvent,
) {
	for ns, result := range nsToResults {
		// Ensure eventBatches has enough batch entries for the number of compliance events for this namespace.
		if len(eventBatches) < len(result.events) {
			eventBatches = append(
				make([]map[string]*objectTmplEvalResultWithEvent, len(result.events)-len(eventBatches)),
				eventBatches...,
			)
		}

		for i, event := range result.events {
			// Determine the applicable batch. For example, if the policy enforces a "Role" in namespaces "ns1" and
			// "ns2", and the "Role" was created in "ns1" and already compliant in "ns2", then "eventBatches" would
			// have a length of two. The zeroth index would contain a noncompliant event because the "Role" did not
			// exist in "ns1". The first index would contain two compliant events because the "Role" was created in
			// "ns1" and was already compliant in "ns2".
			batchIndex := len(eventBatches) - len(result.events) + i

			if eventBatches[batchIndex] == nil {
				eventBatches[batchIndex] = map[string]*objectTmplEvalResultWithEvent{}
			}

			eventBatches[batchIndex][ns] = &objectTmplEvalResultWithEvent{result: result, event: event}
		}
	}

	return eventBatches
}

// updatedRelatedObjects calculates what the related objects list should be, sorting the given list
// and preserving properties that are already present in the current related objects list.
func (r *ConfigurationPolicyReconciler) updatedRelatedObjects(
	plc *policyv1.ConfigurationPolicy, related []policyv1.RelatedObject,
) (updatedRelated []policyv1.RelatedObject) {
	oldRelated := plc.Status.RelatedObjects

	sort.SliceStable(related, func(i, j int) bool {
		if related[i].Object.Kind != related[j].Object.Kind {
			return related[i].Object.Kind < related[j].Object.Kind
		}
		if related[i].Object.Metadata.Namespace != related[j].Object.Metadata.Namespace {
			return related[i].Object.Metadata.Namespace < related[j].Object.Metadata.Namespace
		}

		return related[i].Object.Metadata.Name < related[j].Object.Metadata.Name
	})

	for i, newEntry := range related {
		for _, oldEntry := range oldRelated {
			// Get matching objects
			if gocmp.Equal(newEntry.Object, oldEntry.Object) {
				if oldEntry.Properties != nil &&
					newEntry.Properties != nil &&
					newEntry.Properties.CreatedByPolicy != nil &&
					!(*newEntry.Properties.CreatedByPolicy) {
					// Use the old properties if they existed and this is not a newly created resource
					related[i].Properties.CreatedByPolicy = oldEntry.Properties.CreatedByPolicy
					related[i].Properties.UID = oldEntry.Properties.UID

					break
				}
			}
		}
	}

	return related
}

// helper function that appends a condition (violation or compliant) to the status of a configurationpolicy
// Set the index to -1 to signal that the status should be cleared.
func addConditionToStatus(
	plc *policyv1.ConfigurationPolicy, index int, compliant bool, reason string, message string,
) (updateNeeded bool) {
	newCond := &policyv1.Condition{
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	var complianceState policyv1.ComplianceState

	if reason == reasonCleanupError {
		complianceState = policyv1.Terminating
		newCond.Type = "violation"
	} else if compliant {
		complianceState = policyv1.Compliant
		newCond.Type = "notification"
	} else {
		complianceState = policyv1.NonCompliant
		newCond.Type = "violation"
	}

	log := log.WithValues("policy", plc.GetName(), "complianceState", complianceState)

	if compliant && plc.Spec != nil && plc.Spec.EvaluationInterval.Compliant == "never" {
		msg := `This policy will not be evaluated again due to spec.evaluationInterval.compliant being set to "never"`
		log.Info(msg)
		newCond.Message += fmt.Sprintf(". %s.", msg)
	} else if !compliant && plc.Spec != nil && plc.Spec.EvaluationInterval.NonCompliant == "never" {
		msg := "This policy will not be evaluated again due to spec.evaluationInterval.noncompliant " +
			`being set to "never"`
		log.Info(msg)
		newCond.Message += fmt.Sprintf(". %s.", msg)
	}

	// Set a boolean to clear the details array if the index is -1, but set
	// the index to zero to determine whether a status update is required
	clearStatus := false

	if index == -1 {
		clearStatus = true
		index = 0
	}

	// Handle the case where this object template index wasn't processed before. This will add unknown compliancy
	// details for previous object templates that didn't succeed but also didn't cause a violation. One example is if
	// getting the mapping failed on a previous object template but the mapping error was unknown so processing of the
	// object template was skipped.
	for len(plc.Status.CompliancyDetails)-1 < index {
		emptyCompliance := policyv1.UnknownCompliancy

		// On the entry for the currently processing object template, use the object templates compliance state.
		if index == len(plc.Status.CompliancyDetails)-2 {
			emptyCompliance = complianceState
		}

		plc.Status.CompliancyDetails = append(plc.Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: emptyCompliance,
			Conditions:      []policyv1.Condition{},
		})
	}

	if plc.Status.CompliancyDetails[index].ComplianceState != complianceState {
		updateNeeded = true
	}

	plc.Status.CompliancyDetails[index].ComplianceState = complianceState

	// Ensure the new condition is in the status
	currentConds := plc.Status.CompliancyDetails[index].Conditions

	if len(currentConds) == 0 {
		plc.Status.CompliancyDetails[index].Conditions = []policyv1.Condition{*newCond}
		updateNeeded = true
	} else {
		oldCond := currentConds[len(currentConds)-1]
		newConditionIsSame := oldCond.Status == newCond.Status &&
			oldCond.Reason == newCond.Reason &&
			oldCond.Message == newCond.Message &&
			oldCond.Type == newCond.Type

		if !newConditionIsSame {
			plc.Status.CompliancyDetails[index].Conditions[len(currentConds)-1] = *newCond
			updateNeeded = true
		}
	}

	// Clear the details array if the index provided was -1
	if clearStatus {
		plc.Status.CompliancyDetails = plc.Status.CompliancyDetails[0:1]
	}

	return updateNeeded
}

// handleObjects controls the processing of each individual object template within a configurationpolicy
func (r *ConfigurationPolicyReconciler) handleObjects(
	objectT *policyv1.ObjectTemplate,
	namespace string,
	desiredObj unstructured.Unstructured,
	index int,
	policy *policyv1.ConfigurationPolicy,
	scopedGVR depclient.ScopedGVR,
	useCache bool,
) (
	relatedObjects []policyv1.RelatedObject,
	result objectTmplEvalResult,
) {
	log := log.WithValues("policy", policy.GetName(), "index", index, "objectNamespace", namespace)

	if namespace != "" {
		log.V(2).Info("Handling object template")
	} else {
		log.V(2).Info("Handling object template, no namespace specified")
	}

	exists := true
	objNames := []string{}
	remediation := policy.Spec.RemediationAction

	desiredObjName := desiredObj.GetName()
	desiredObjNamespace := desiredObj.GetNamespace()
	desiredObjKind := desiredObj.GetKind()

	// If the parsed namespace doesn't match the object namespace, something in the calling function went wrong
	if desiredObjNamespace != "" && desiredObjNamespace != namespace {
		panic(fmt.Sprintf("Error: provided namespace '%s' does not match object namespace '%s'",
			namespace, desiredObjNamespace))
	}

	if scopedGVR.Namespaced && namespace == "" {
		log.Info(
			"The object template is namespaced but no namespace is specified. Cannot process.",
			"name", desiredObjName,
			"kind", desiredObjKind,
		)

		var space string
		if desiredObjName != "" {
			space = " "
		}

		// namespaced but none specified, generate violation
		msg := fmt.Sprintf("namespaced object%s%s of kind %s has no namespace specified "+
			"from the policy namespaceSelector nor the object metadata",
			space, desiredObjName, desiredObjKind,
		)

		result = objectTmplEvalResult{
			[]string{desiredObjName},
			namespace,
			[]objectTmplEvalEvent{{false, "K8s missing namespace", msg}},
		}

		return nil, result
	}

	var existingObj *unstructured.Unstructured
	var allResourceNames []string

	if desiredObjName != "" { // named object, so checking just for the existence of the specific object
		// If the object couldn't be retrieved, this will be handled later on.
		if useCache {
			objGVK := schema.GroupVersionKind{
				Group:   scopedGVR.Group,
				Version: scopedGVR.Version,
				Kind:    desiredObjKind,
			}

			existingObj, _ = r.getObjectFromCache(policy, namespace, desiredObjName, objGVK)
		} else {
			existingObj, _ = getObject(namespace, desiredObjName, scopedGVR, r.TargetK8sDynamicClient)
		}

		exists = existingObj != nil

		objNames = append(objNames, desiredObjName)
	} else if desiredObjKind != "" {
		// No name, so we are checking for the existence of any object of this kind
		log.V(1).Info(
			"The object template does not specify a name. Will search for matching objects in the namespace.",
		)
		objNames, allResourceNames = r.getNamesOfKind(
			policy,
			desiredObj,
			scopedGVR,
			namespace,
			r.TargetK8sDynamicClient,
			objectT.ComplianceType,
			useCache,
		)

		// we do not support enforce on unnamed templates
		if !remediation.IsInform() {
			log.Info(
				"The object template does not specify a name. Setting the remediation action to inform.",
				"oldRemediationAction", remediation,
			)
		}
		remediation = "inform"

		if len(objNames) == 0 {
			exists = false
		} else if len(objNames) == 1 {
			// If the object couldn't be retrieved, this will be handled later on.
			existingObj, _ = getObject(namespace, objNames[0], scopedGVR, r.TargetK8sDynamicClient)

			exists = existingObj != nil
		}
	}

	objShouldExist := !objectT.ComplianceType.IsMustNotHave()

	shouldAddCondensedRelatedObj := false

	if len(objNames) == 1 {
		name := objNames[0]
		singObj := singleObject{
			policy:      policy,
			scopedGVR:   scopedGVR,
			existingObj: existingObj,
			name:        name,
			namespace:   namespace,
			shouldExist: objShouldExist,
			index:       index,
			desiredObj:  desiredObj,
		}

		log.V(2).Info("Handling a single object template")

		var objectProperties *policyv1.ObjectProperties

		result, objectProperties = r.handleSingleObj(singObj, remediation, exists, objectT)

		if len(result.events) != 0 {
			event := result.events[len(result.events)-1]
			relatedObjects = addRelatedObjects(
				event.compliant,
				scopedGVR,
				desiredObjKind,
				namespace,
				result.objectNames,
				event.reason,
				objectProperties,
			)
		}
	} else { // This case only occurs when the desired object is not named
		resultEvent := objectTmplEvalEvent{}
		if objShouldExist {
			if exists {
				resultEvent.compliant = true
				resultEvent.reason = reasonWantFoundExists
			} else {
				resultEvent.compliant = false
				resultEvent.reason = reasonWantFoundDNE
				// Length of objNames = 0, complianceType == musthave or mustonlyhave
				// Find Noncompliant resources to add to the status.relatedObjects for debugging purpose
				shouldAddCondensedRelatedObj = true
				if desiredObjKind != "" && desiredObjName == "" {
					// Change reason to Resource found but does not match
					if len(allResourceNames) > 0 {
						resultEvent.reason = reasonWantFoundNoMatch
					}
				}
			}
		} else {
			if exists {
				resultEvent.compliant = false
				resultEvent.reason = reasonWantNotFoundExists
			} else {
				resultEvent.compliant = true
				resultEvent.reason = reasonWantNotFoundDNE
				// Compliant, complianceType == mustnothave
				// Find resources in the same namespace to add to the status.relatedObjects for debugging purpose
				shouldAddCondensedRelatedObj = true
			}
		}

		result = objectTmplEvalResult{objectNames: objNames, events: []objectTmplEvalEvent{resultEvent}}

		if shouldAddCondensedRelatedObj {
			// relatedObjs name is -
			relatedObjects = addCondensedRelatedObjs(
				scopedGVR,
				resultEvent.compliant,
				desiredObjKind,
				namespace,
				resultEvent.reason,
			)
		} else {
			relatedObjects = addRelatedObjects(
				resultEvent.compliant,
				scopedGVR,
				desiredObjKind,
				namespace,
				objNames,
				resultEvent.reason,
				nil,
			)
		}
	}

	return relatedObjects, result
}

type singleObject struct {
	policy      *policyv1.ConfigurationPolicy
	scopedGVR   depclient.ScopedGVR
	existingObj *unstructured.Unstructured
	name        string
	namespace   string
	shouldExist bool
	index       int
	desiredObj  unstructured.Unstructured
}

type objectTmplEvalResult struct {
	objectNames []string
	namespace   string
	events      []objectTmplEvalEvent
}

type objectTmplEvalEvent struct {
	compliant bool
	reason    string
	message   string
}

type objectTmplEvalResultWithEvent struct {
	result objectTmplEvalResult
	event  objectTmplEvalEvent
}

// handleSingleObj takes in an object template (for a named object) and its data and determines whether
// the object on the cluster is compliant or not
func (r *ConfigurationPolicyReconciler) handleSingleObj(
	obj singleObject,
	remediation policyv1.RemediationAction,
	exists bool,
	objectT *policyv1.ObjectTemplate,
) (
	result objectTmplEvalResult,
	objectProperties *policyv1.ObjectProperties,
) {
	objLog := log.WithValues("object", obj.name, "policy", obj.policy.Name, "index", obj.index)

	result = objectTmplEvalResult{
		objectNames: []string{obj.name},
		namespace:   obj.namespace,
		events:      []objectTmplEvalEvent{},
	}

	if !exists && obj.shouldExist {
		// object is missing and will be created, so send noncompliant "does not exist" event regardless of the
		// remediation action
		result.events = append(result.events, objectTmplEvalEvent{false, reasonWantFoundDNE, ""})

		// it is a musthave and it does not exist, so it must be created
		if remediation.IsEnforce() {
			var uid string
			completed, reason, msg, uid, err := r.enforceByCreatingOrDeleting(obj)

			hasStatus := false
			if tmplObj, err := unmarshalFromJSON(objectT.ObjectDefinition.Raw); err == nil {
				_, hasStatus = tmplObj.Object["status"]
			}

			if completed && hasStatus {
				msg += ", the status of the object will be verified in the next evaluation"
				reason += ", status unchecked"
				result.events = append(result.events, objectTmplEvalEvent{false, reason, msg})
			} else {
				result.events = append(result.events, objectTmplEvalEvent{completed, reason, msg})
			}

			if err != nil {
				// violation created for handling error
				objLog.Error(err, "Could not handle missing musthave object")
			} else {
				created := true
				objectProperties = &policyv1.ObjectProperties{
					CreatedByPolicy: &created,
					UID:             uid,
				}
			}
		}

		return
	}

	if exists && !obj.shouldExist {
		// it is a mustnothave but it exist, so it must be deleted
		if remediation.IsEnforce() {
			completed, reason, msg, _, err := r.enforceByCreatingOrDeleting(obj)
			if err != nil {
				objLog.Error(err, "Could not handle existing mustnothave object")
			}

			result.events = append(result.events, objectTmplEvalEvent{completed, reason, msg})
		} else { // inform
			result.events = append(result.events, objectTmplEvalEvent{false, reasonWantNotFoundExists, ""})
		}

		return
	}

	if !exists && !obj.shouldExist {
		log.V(1).Info("The object does not exist and is compliant with the mustnothave compliance type")
		// it is a must not have and it does not exist, so it is compliant
		result.events = append(result.events, objectTmplEvalEvent{true, reasonWantNotFoundDNE, ""})

		return
	}

	// object exists and the template requires it, so we need to check specific fields to see if we have a match
	if exists && obj.shouldExist {
		log.V(2).Info("The object already exists. Verifying the object fields match what is desired.")

		var throwSpecViolation, triedUpdate bool
		var msg, diff string
		var updatedObj *unstructured.Unstructured

		created := false
		uid := string(obj.existingObj.GetUID())

		if evaluated, compliant, cachedMsg := r.alreadyEvaluated(obj.policy, obj.existingObj); evaluated {
			log.V(1).Info("Skipping object comparison since the resourceVersion hasn't changed")

			for _, relatedObj := range obj.policy.Status.RelatedObjects {
				if relatedObj.Properties != nil && relatedObj.Properties.UID == uid {
					// Retain the diff from the previous evaluation
					diff = relatedObj.Properties.Diff

					break
				}
			}

			throwSpecViolation = !compliant
			msg = cachedMsg
		} else {
			throwSpecViolation, msg, diff, triedUpdate, updatedObj = r.checkAndUpdateResource(
				obj, objectT, remediation,
			)

			if updatedObj != nil && string(updatedObj.GetUID()) != uid {
				uid = string(updatedObj.GetUID())
				created = true
			}
		}

		if triedUpdate && !strings.Contains(msg, "Error validating the object") {
			// The object was mismatched and was potentially fixed depending on the remediation action
			result.events = append(result.events, objectTmplEvalEvent{false, reasonWantFoundNoMatch, ""})
		}

		if throwSpecViolation {
			var resultReason, resultMsg string

			if msg != "" {
				resultReason = "K8s update template error"
				resultMsg = msg
			} else {
				resultReason = reasonWantFoundNoMatch
			}

			objectProperties = &policyv1.ObjectProperties{
				CreatedByPolicy: &created,
				UID:             uid,
				Diff:            diff,
			}

			result.events = append(result.events, objectTmplEvalEvent{false, resultReason, resultMsg})
		} else {
			// it is a must have and it does exist, so it is compliant
			if remediation.IsEnforce() {
				if updatedObj != nil {
					result.events = append(result.events, objectTmplEvalEvent{true, reasonUpdateSuccess, ""})
				} else {
					result.events = append(result.events, objectTmplEvalEvent{true, reasonWantFoundExists, ""})
				}

				objectProperties = &policyv1.ObjectProperties{
					CreatedByPolicy: &created,
					UID:             uid,
					Diff:            diff,
				}
			} else {
				result.events = append(result.events, objectTmplEvalEvent{true, reasonWantFoundExists, ""})
			}
		}
	}

	return
}

// getMapping takes in a raw object, decodes it, and maps it to an existing group/kind
func (r *ConfigurationPolicyReconciler) getMapping(
	gvk schema.GroupVersionKind, policy *policyv1.ConfigurationPolicy, index int,
) (depclient.ScopedGVR, error) {
	log := log.WithValues("policy", policy.GetName(), "index", index)

	if gvk.Group == "" && gvk.Version == "" {
		err := fmt.Errorf("object template at index [%v] in policy `%v` missing apiVersion", index, policy.Name)

		log.Error(err, "Can not get mapping for object")

		return depclient.ScopedGVR{}, err
	}

	scopedGVR, err := r.DynamicWatcher.GVKToGVR(gvk)
	if err != nil {
		if !errors.Is(err, depclient.ErrNoVersionedResource) {
			log.Error(err, "Could not identify mapping error from raw object", "gvk", gvk)

			return depclient.ScopedGVR{}, err
		}

		mappingErr := errors.New("couldn't find mapping resource with kind " + gvk.Kind +
			", please check if you have CRD deployed")

		log.Error(err, "Could not map resource, do you have the CRD deployed?", "kind", gvk.Kind)

		parent := ""
		if len(policy.OwnerReferences) > 0 {
			parent = policy.OwnerReferences[0].Name
		}

		policyUserErrorsCounter.WithLabelValues(parent, policy.GetName(), "no-object-CRD").Add(1)

		return depclient.ScopedGVR{}, mappingErr
	}

	log.V(2).Info("Found the API mapping for the object template",
		"group", gvk.Group, "version", gvk.Version, "kind", gvk.Kind)

	return scopedGVR, nil
}

// buildNameList is a helper function to pull names of resources that match an objectTemplate from a list of resources
func buildNameList(
	desiredObj unstructured.Unstructured,
	complianceType policyv1.ComplianceType,
	resList *unstructured.UnstructuredList,
) (kindNameList []string) {
	for i := range resList.Items {
		uObj := resList.Items[i]
		match := true

		for key := range desiredObj.Object {
			// Dry run API requests aren't run on unnamed object templates for performance reasons, so be less
			// conservative in the comparison algorithm.
			zeroValueEqualsNil := true

			// if any key in the object generates a mismatch, the object does not match the template and we
			// do not add its name to the list
			errorMsg, updateNeeded, _, skipped := handleSingleKey(
				key, desiredObj, &uObj, complianceType, zeroValueEqualsNil,
			)
			if !skipped {
				if errorMsg != "" || updateNeeded {
					match = false
				}
			}
		}

		if match {
			kindNameList = append(kindNameList, uObj.GetName())
		}
	}

	return kindNameList
}

// getNamesOfKind returns an array with names of all of the resources found
// matching the GVK specified.
// allResourceList includes names that are under the same namespace and kind.
func (r *ConfigurationPolicyReconciler) getNamesOfKind(
	plc *policyv1.ConfigurationPolicy,
	desiredObj unstructured.Unstructured,
	scopedGVR depclient.ScopedGVR,
	ns string,
	dclient dynamic.Interface,
	complianceType policyv1.ComplianceType,
	useCache bool,
) (kindNameList []string, allResourceList []string) {
	var resList *unstructured.UnstructuredList
	var err error

	if useCache {
		var returnedItems []unstructured.Unstructured

		returnedItems, err = r.DynamicWatcher.List(plc.ObjectIdentifier(), desiredObj.GroupVersionKind(), ns, nil)

		resList = &unstructured.UnstructuredList{Items: returnedItems}
	} else if scopedGVR.Namespaced {
		res := dclient.Resource(scopedGVR.GroupVersionResource).Namespace(ns)

		resList, err = res.List(context.TODO(), metav1.ListOptions{})
	} else {
		res := dclient.Resource(scopedGVR.GroupVersionResource)

		resList, err = res.List(context.TODO(), metav1.ListOptions{})
	}

	if err != nil {
		log.Error(
			err, "Could not list resources", "rsrc", scopedGVR.Resource, "namespaced", scopedGVR.Namespaced,
		)

		return kindNameList, allResourceList
	}

	for _, res := range resList.Items {
		allResourceList = append(allResourceList, res.GetName())
	}

	return buildNameList(desiredObj, complianceType, resList), allResourceList
}

// enforceByCreatingOrDeleting can handle the situation where a musthave or mustonlyhave object is
// completely missing (as opposed to existing, but not matching the desired state), or where a
// mustnothave object does exist. Eg, it does not handle the case where a targeted update would need
// to be made to an object.
func (r *ConfigurationPolicyReconciler) enforceByCreatingOrDeleting(obj singleObject) (
	result bool, reason string, msg string, uid string, erro error,
) {
	log := log.WithValues(
		"object", obj.name,
		"policy", obj.policy.Name,
		"objectNamespace", obj.namespace,
		"objectTemplateIndex", obj.index,
	)
	idStr := identifierStr([]string{obj.name}, obj.namespace)

	var res dynamic.ResourceInterface
	if obj.scopedGVR.Namespaced {
		res = r.TargetK8sDynamicClient.Resource(obj.scopedGVR.GroupVersionResource).Namespace(obj.namespace)
	} else {
		res = r.TargetK8sDynamicClient.Resource(obj.scopedGVR.GroupVersionResource)
	}

	var completed bool
	var err error

	if obj.shouldExist {
		log.Info("Enforcing the policy by creating the object")

		var createdObj *unstructured.Unstructured

		if createdObj, err = r.createObject(res, obj.desiredObj); createdObj == nil {
			reason = "K8s creation error"
			msg = fmt.Sprintf(
				"%v %v is missing, and cannot be created, reason: `%v`", obj.scopedGVR.Resource, idStr, err,
			)
		} else {
			log.V(2).Info(
				"Created missing must have object", "resource", obj.scopedGVR.Resource, "name", obj.name,
			)
			reason = reasonWantFoundCreated
			msg = fmt.Sprintf("%v %v was created successfully", obj.scopedGVR.Resource, idStr)

			uid = string(createdObj.GetUID())
			completed = true
		}
	} else {
		log.Info("Enforcing the policy by deleting the object")

		if completed, err = deleteObject(res, obj.name, obj.namespace); !completed {
			reason = "K8s deletion error"
			msg = fmt.Sprintf(
				"%v %v exists, and cannot be deleted, reason: `%v`", obj.scopedGVR.Resource, idStr, err,
			)
		} else {
			reason = reasonDeleteSuccess
			msg = fmt.Sprintf("%v %v was deleted successfully", obj.scopedGVR.Resource, idStr)
		}
	}

	return completed, reason, msg, uid, err
}

// getObject gets the object with the dynamic client and returns the object if found.
func getObject(
	namespace string,
	name string,
	scopedGVR depclient.ScopedGVR,
	dclient dynamic.Interface,
) (object *unstructured.Unstructured, err error) {
	objLog := log.WithValues("name", name, "namespaced", scopedGVR.Namespaced, "namespace", namespace)
	objLog.V(2).Info("Checking if the object exists")

	var res dynamic.ResourceInterface
	if scopedGVR.Namespaced {
		res = dclient.Resource(scopedGVR.GroupVersionResource).Namespace(namespace)
	} else {
		res = dclient.Resource(scopedGVR.GroupVersionResource)
	}

	object, err = res.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			objLog.V(2).Info("Got 'Not Found' response for object from the API server")

			return nil, nil
		}

		objLog.V(2).Error(err, "Could not retrieve object from the API server")

		return nil, err
	}

	objLog.V(2).Info("Retrieved object from the API server")

	return object, nil
}

// getObjectFromCache gets the object with the caching dependency watcher client and returns the object if found.
func (r *ConfigurationPolicyReconciler) getObjectFromCache(
	plc *policyv1.ConfigurationPolicy,
	objNamespace string,
	objName string,
	objGVK schema.GroupVersionKind,
) (*unstructured.Unstructured, error) {
	objLog := log.WithValues("name", objName, "namespace", objNamespace)
	objLog.V(2).Info("Checking if the object exists")

	watcher := plc.ObjectIdentifier()

	rv, err := r.DynamicWatcher.Get(watcher, objGVK, objNamespace, objName)
	if err != nil {
		objLog.V(2).Error(err, "Could not retrieve object from the API server")

		return nil, err
	}

	if rv == nil {
		objLog.V(2).Info("Got 'Not Found' response for object from the API server")

		return nil, nil
	}

	objLog.V(2).Info("Retrieved object from the watch cache")

	return rv, nil
}

func (r *ConfigurationPolicyReconciler) createObject(
	res dynamic.ResourceInterface, unstruct unstructured.Unstructured,
) (object *unstructured.Unstructured, err error) {
	objLog := log.WithValues("name", unstruct.GetName(), "namespace", unstruct.GetNamespace())
	objLog.V(2).Info("Entered createObject", "unstruct", unstruct)

	// FieldValidation is supported in k8s 1.25 as beta release
	// so if the version is below 1.25, we need to use client side validation to validate the object
	if semver.Compare(r.ServerVersion, "v1.25.0") < 0 {
		if err := r.validateObject(&unstruct); err != nil {
			return nil, err
		}
	}

	object, err = res.Create(context.TODO(), &unstruct, metav1.CreateOptions{
		FieldValidation: metav1.FieldValidationStrict,
	})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			objLog.V(2).Info("Got 'Already Exists' response for object")

			return object, err
		}

		objLog.Error(err, "Could not create object", "reason", k8serrors.ReasonForError(err))

		return nil, err
	}

	objLog.V(2).Info("Resource created")

	return object, nil
}

func deleteObject(res dynamic.ResourceInterface, name, namespace string) (deleted bool, err error) {
	objLog := log.WithValues("name", name, "namespace", namespace)
	objLog.V(2).Info("Entered deleteObject")

	err = res.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			objLog.V(2).Info("Got 'Not Found' response while deleting object")

			return true, err
		}

		objLog.Error(err, "Could not delete object")

		return false, err
	}

	objLog.V(2).Info("Deleted object")

	return true, nil
}

// mergeSpecs is a wrapper for the recursive function to merge 2 maps.
func mergeSpecs(
	templateVal, existingVal interface{}, ctype policyv1.ComplianceType, zeroValueEqualsNil bool,
) (interface{}, error) {
	// Copy templateVal since it will be modified in mergeSpecsHelper
	data1, err := json.Marshal(templateVal)
	if err != nil {
		return nil, err
	}

	var j1 interface{}

	err = json.Unmarshal(data1, &j1)
	if err != nil {
		return nil, err
	}

	return mergeSpecsHelper(j1, existingVal, ctype, zeroValueEqualsNil), nil
}

// mergeSpecsHelper is a helper function that takes an object from the existing object and merges in
// all the data that is different in the template. This way, comparing the merged object to the one
// that exists on the cluster will tell you whether the existing object is compliant with the template.
// This function uses recursion to check mismatches in nested objects and is the basis for most
// comparisons the controller makes.
func mergeSpecsHelper(
	templateVal, existingVal interface{}, ctype policyv1.ComplianceType, zeroValueEqualsNil bool,
) interface{} {
	switch templateVal := templateVal.(type) {
	case map[string]interface{}:
		existingVal, ok := existingVal.(map[string]interface{})
		if !ok {
			// if one field is a map and the other isn't, don't bother merging -
			// just returning the template value will still generate noncompliant
			return templateVal
		}
		// otherwise, iterate through all fields in the template object and
		// merge in missing values from the existing object
		for k, v2 := range existingVal {
			if v1, ok := templateVal[k]; ok {
				templateVal[k] = mergeSpecsHelper(v1, v2, ctype, zeroValueEqualsNil)
			} else {
				templateVal[k] = v2
			}
		}
	case []interface{}: // list nested in map
		existingVal, ok := existingVal.([]interface{})
		if !ok {
			// if one field is a list and the other isn't, don't bother merging
			return templateVal
		}

		if len(existingVal) > 0 {
			// if both values are non-empty lists, we need to merge in the extra data in the existing
			// object to do a proper compare
			return mergeArrays(templateVal, existingVal, ctype, zeroValueEqualsNil)
		}
	case nil:
		// if template value is nil, pull data from existing, since the template does not care about it
		existingVal, ok := existingVal.(map[string]interface{})
		if ok {
			return existingVal
		}
	}

	_, ok := templateVal.(string)
	if !ok {
		return templateVal
	}

	return templateVal.(string)
}

type countedVal struct {
	value interface{}
	count int
}

// mergeArrays is a helper function that takes a list from the existing object and merges in all the data that is
// different in the template.
func mergeArrays(
	desiredArr []interface{}, existingArr []interface{}, ctype policyv1.ComplianceType, zeroValueEqualsNil bool,
) (result []interface{}) {
	if ctype.IsMustOnlyHave() {
		return desiredArr
	}

	desiredArrCopy := append([]interface{}{}, desiredArr...)
	idxWritten := map[int]bool{}

	for i := range desiredArrCopy {
		idxWritten[i] = false
	}

	// create a set with a key for each unique item in the list
	oldItemSet := make(map[string]*countedVal)

	for _, val2 := range existingArr {
		key := fmt.Sprint(val2)

		if entry, ok := oldItemSet[key]; ok {
			entry.count++
		} else {
			oldItemSet[key] = &countedVal{value: val2, count: 1}
		}
	}

	seen := map[string]bool{}

	// Iterate both arrays in order to favor the case when the object is already compliant.
	for _, val2 := range existingArr {
		key := fmt.Sprint(val2)
		if seen[key] {
			continue
		}

		seen[key] = true

		count := 0
		val2 := oldItemSet[key].value
		// for each list item in the existing array, iterate through the template array and try to find a match
		for desiredArrIdx, val1 := range desiredArrCopy {
			if idxWritten[desiredArrIdx] {
				continue
			}

			var mergedObj interface{}
			// Stores if val1 and val2 are maps with the same "name" key value. In the case of the containers array
			// in a Deployment object, the value should be merged and not appended if the name is the same in both.
			var sameNamedObjects bool

			switch val2 := val2.(type) {
			case map[string]interface{}:
				// If the policy value and the current value are different types, use the same logic
				// as the default case.
				val1, ok := val1.(map[string]interface{})
				if !ok {
					mergedObj = val1

					break
				}

				if name2, ok := val2["name"].(string); ok && name2 != "" {
					if name1, ok := val1["name"].(string); ok && name1 == name2 {
						sameNamedObjects = true
					}
				}

				// use map compare helper function to check equality on lists of maps
				mergedObj, _ = compareSpecs(val1, val2, ctype, zeroValueEqualsNil)
			default:
				mergedObj = val1
			}
			// if a match is found, this field is already in the template, so we can skip it in future checks
			if sameNamedObjects || equalObjWithSort(mergedObj, val2, zeroValueEqualsNil) {
				count++

				desiredArr[desiredArrIdx] = mergedObj
				idxWritten[desiredArrIdx] = true
			}

			// If the result of merging val1 (template) into val2 (existing value) matched val2 for the required count,
			// move on to the next existing value.
			if count == oldItemSet[key].count {
				break
			}
		}
		// if an item in the existing object cannot be found in the template, we add it to the template array
		// to produce the merged array
		if count < oldItemSet[key].count {
			for i := 0; i < (oldItemSet[key].count - count); i++ {
				desiredArr = append(desiredArr, val2)
			}
		}
	}

	return desiredArr
}

// compareSpecs is a wrapper function that creates a merged map for mustHave
// and returns the template map for mustonlyhave
func compareSpecs(
	newSpec, oldSpec map[string]interface{}, ctype policyv1.ComplianceType, zeroValueEqualsNil bool,
) (updatedSpec map[string]interface{}, err error) {
	if ctype.IsMustOnlyHave() {
		return newSpec, nil
	}
	// if compliance type is musthave, create merged object to compare on
	merged, err := mergeSpecs(newSpec, oldSpec, ctype, zeroValueEqualsNil)
	if err != nil {
		return merged.(map[string]interface{}), err
	}

	return merged.(map[string]interface{}), nil
}

// handleSingleKey checks whether a key/value pair in an object template matches with that in the existing
// resource on the cluster
func handleSingleKey(
	key string,
	desiredObj unstructured.Unstructured,
	existingObj *unstructured.Unstructured,
	complianceType policyv1.ComplianceType,
	zeroValueEqualsNil bool,
) (errormsg string, update bool, merged interface{}, skip bool) {
	log := log.WithValues("name", existingObj.GetName(), "namespace", existingObj.GetNamespace())
	var err error

	updateNeeded := false

	if key == "apiVersion" || key == "kind" {
		log.V(2).Info("Ignoring the key since it is deny listed", "key", key)

		return "", false, nil, true
	}

	desiredValue := formatTemplate(desiredObj, key)
	existingValue := existingObj.UnstructuredContent()[key]
	typeErr := ""

	// We will compare the existing field to a "merged" field which has the fields in the template
	// merged into the existing object to avoid erroring on fields that are not in the template
	// but have been automatically added to the object.
	// For the mustOnlyHave complianceType, this object is identical to the field in the template.
	var mergedValue interface{}

	switch desiredValue := desiredValue.(type) {
	case []interface{}:
		switch existingValue := existingValue.(type) {
		case []interface{}:
			mergedValue = mergeArrays(desiredValue, existingValue, complianceType, zeroValueEqualsNil)
		case nil:
			mergedValue = desiredValue
		default:
			typeErr = fmt.Sprintf(
				"Error merging changes into key \"%s\": object type of template and existing do not match",
				key)
		}
	case map[string]interface{}:
		switch existingValue := existingValue.(type) {
		case map[string]interface{}:
			mergedValue, err = compareSpecs(desiredValue, existingValue, complianceType, zeroValueEqualsNil)
		case nil:
			mergedValue = desiredValue
		default:
			typeErr = fmt.Sprintf(
				"Error merging changes into key \"%s\": object type of template and existing do not match",
				key)
		}
	default: // If the field is not an object or slice, just do a basic compare
		mergedValue = desiredValue
	}

	if typeErr != "" {
		return typeErr, false, mergedValue, false
	}

	if err != nil {
		message := fmt.Sprintf("Error merging changes into %s: %s", key, err)

		return message, false, mergedValue, false
	}

	if key == "metadata" {
		// filter out autogenerated annotations that have caused compare issues in the past
		mergedValue, existingValue = fmtMetadataForCompare(
			mergedValue.(map[string]interface{}), existingValue.(map[string]interface{}))
	}

	if key == "stringData" && existingObj.GetKind() == "Secret" {
		// override automatic conversion from stringData to data before evaluation
		encodedValue, _, err := unstructured.NestedStringMap(existingObj.Object, "data")
		if err != nil {
			message := "Error accessing encoded data"

			return message, false, mergedValue, false
		}

		decodedValue := make(map[string]interface{}, len(encodedValue))

		for k, encoded := range encodedValue {
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				secretName := existingObj.GetName()
				message := fmt.Sprintf("Error decoding secret: %s", secretName)

				return message, false, mergedValue, false
			}

			decodedValue[k] = string(decoded)
		}

		existingValue = decodedValue
	}

	// sort objects before checking equality to ensure they're in the same order
	if !equalObjWithSort(mergedValue, existingValue, zeroValueEqualsNil) {
		updateNeeded = true
	}

	return "", updateNeeded, mergedValue, false
}

// validateObject performs client-side validation of the input object using the server's OpenAPI definitions that are
// cached. An error is returned if the input object is invalid or the OpenAPI data could not be fetched.
func (r *ConfigurationPolicyReconciler) validateObject(object *unstructured.Unstructured) error {
	// Parse() handles caching of the OpenAPI data.
	r.lock.RLock()

	// Reset the OpenAPI cache every 10 minutes in case the CRDs were updated since the last fetch.
	if r.openAPIParser == nil || time.Now().UTC().Sub(r.openAPIParserLastRefreshed) >= 10*time.Minute {
		r.lock.RUnlock()
		r.lock.Lock()
		// Repeat the check after the write lock is obtained in case another goroutine updated it
		if r.openAPIParser == nil || time.Now().UTC().Sub(r.openAPIParserLastRefreshed) >= 10*time.Minute {
			r.openAPIParser = openapi.NewOpenAPIParser(r.TargetK8sClient.Discovery())
			r.openAPIParserLastRefreshed = time.Now().UTC()
		}

		r.lock.Unlock()
		r.lock.RLock()
	}

	openAPIResources, err := r.openAPIParser.Parse()
	r.lock.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to retrieve the OpenAPI data from the Kubernetes API: %w", err)
	}

	schema := validation.ConjunctiveSchema{
		validation.NewSchemaValidation(openAPIResources),
		validation.NoDoubleKeySchema{},
	}

	objectJSON, err := object.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal the object to JSON: %w", err)
	}

	schemaErr := schema.ValidateBytes(objectJSON)

	// Filter out errors due to missing fields in the status since those are ignored when enforcing the policy. This
	// allows a user to switch a policy between inform and enforce without having to remove the status check.
	return apimachineryerrors.FilterOut(
		schemaErr,
		func(err error) bool {
			var validationErr kubeopenapivalidation.ValidationError
			if !errors.As(err, &validationErr) {
				return false
			}

			// Path is in the format of Pod.status.conditions[0].
			pathParts := strings.SplitN(validationErr.Path, ".", 3)
			if len(pathParts) < 2 || pathParts[1] != "status" {
				return false
			}

			var missingFieldErr kubeopenapivalidation.MissingRequiredFieldError

			return errors.As(validationErr.Err, &missingFieldErr)
		},
	)
}

type cachedEvaluationResult struct {
	resourceVersion string
	compliant       bool
	msg             string
}

// checkAndUpdateResource checks each individual key of a resource and passes it to handleKeys to see if it
// matches the template and update it if the remediationAction is enforce. UpdateNeeded indicates whether the
// function tried to update the child object and updateSucceeded indicates whether the update was applied
// successfully.
func (r *ConfigurationPolicyReconciler) checkAndUpdateResource(
	obj singleObject, objectT *policyv1.ObjectTemplate, remediation policyv1.RemediationAction,
) (
	throwSpecViolation bool, message string, diff string, updateNeeded bool, updatedObj *unstructured.Unstructured,
) {
	log := log.WithValues(
		"policy", obj.policy.Name, "name", obj.name, "namespace", obj.namespace, "resource", obj.scopedGVR.Resource,
	)

	// Time the function, and record it in a metric
	before := time.Now().UTC()
	defer func() {
		duration := time.Now().UTC().Sub(before)
		seconds := float64(duration) / float64(time.Second)
		compareObjSecondsCounter.WithLabelValues(
			obj.policy.Name,
			obj.namespace,
			fmt.Sprintf("%s.%s", obj.scopedGVR.Resource, obj.name),
		).Add(seconds)
		compareObjEvalCounter.WithLabelValues(
			obj.policy.Name,
			obj.namespace,
			fmt.Sprintf("%s.%s", obj.scopedGVR.Resource, obj.name),
		).Inc()
	}()

	if obj.existingObj == nil {
		log.Info("Skipping update: Previous object retrieval from the API server failed")

		return false, "", "", false, nil
	}

	var res dynamic.ResourceInterface
	if obj.scopedGVR.Namespaced {
		res = r.TargetK8sDynamicClient.Resource(obj.scopedGVR.GroupVersionResource).Namespace(obj.namespace)
	} else {
		res = r.TargetK8sDynamicClient.Resource(obj.scopedGVR.GroupVersionResource)
	}

	// Use a copy since some values can be directly assigned to mergedObj in handleSingleKey.
	existingObjectCopy := obj.existingObj.DeepCopy()
	removeFieldsForComparison(existingObjectCopy)

	throwSpecViolation, message, updateNeeded, statusMismatch := handleKeys(
		obj.desiredObj,
		obj.existingObj,
		existingObjectCopy,
		objectT.ComplianceType,
		objectT.MetadataComplianceType,
	)
	if message != "" {
		return true, message, "", true, nil
	}

	recordDiff := objectT.RecordDiffWithDefault()
	var needsRecreate bool

	if updateNeeded {
		mismatchLog := "Detected value mismatch"

		log.Info(mismatchLog)

		// FieldValidation is supported in k8s 1.25 as beta release
		// so if the version is below 1.25, we need to use client side validation to validate the object
		if semver.Compare(r.ServerVersion, "v1.25.0") < 0 {
			if err := r.validateObject(obj.existingObj); err != nil {
				message := fmt.Sprintf("Error validating the object %s, the error is `%v`", obj.name, err)

				return true, message, "", updateNeeded, nil
			}
		}

		isInform := remediation.IsInform()

		// It's possible the dry run request shows the object does match. This can happen if the ConfigurationPolicy
		// specifies an empty map and the API server omits it from the return value.
		dryRunUpdatedObj, err := res.Update(context.TODO(), obj.existingObj, metav1.UpdateOptions{
			FieldValidation: metav1.FieldValidationStrict,
			DryRun:          []string{metav1.DryRunAll},
		})
		if err != nil {
			// If it's a conflict, refetch the object and try again.
			if k8serrors.IsConflict(err) {
				log.Info("The object was updating during the evaluation. Trying again.")

				rv, getErr := res.Get(context.TODO(), obj.existingObj.GetName(), metav1.GetOptions{})
				if getErr == nil {
					obj.existingObj = rv

					return r.checkAndUpdateResource(obj, objectT, remediation)
				}
			}

			// Handle all errors not related to updating immutable fields here
			if !k8serrors.IsInvalid(err) {
				message := getUpdateErrorMsg(err, obj.existingObj.GetKind(), obj.name)
				if message == "" {
					message = fmt.Sprintf(
						"Error issuing a dry run update request for the object `%v`, the error is `%v`",
						obj.name,
						err,
					)
				}

				// If the user specifies an unknown or invalid field, it comes back as a bad request.
				if k8serrors.IsBadRequest(err) {
					r.setEvaluatedObject(obj.policy, obj.existingObj, false, message)
				}

				return true, message, "", updateNeeded, nil
			}

			// If an update is invalid (i.e. modifying Pod spec fields), then return noncompliant since that
			// confirms some fields don't match and can't be fixed with an update. If a recreate option is
			// specified, then the update may proceed when enforced.
			needsRecreate = true
			recreateOption := objectT.RecreateOption

			if isInform || !(recreateOption == policyv1.Always || recreateOption == policyv1.IfRequired) {
				log.Info(fmt.Sprintf("Dry run update failed with error: %s", err.Error()))

				// Remove noisy fields such as managedFields from the diff
				removeFieldsForComparison(existingObjectCopy)
				removeFieldsForComparison(obj.existingObj)

				diff = handleDiff(log, recordDiff, true, existingObjectCopy, obj.existingObj)

				if !isInform {
					// Don't include the error message in the compliance status because that can be very long. The
					// user can check the diff or the logs for more information.
					message = fmt.Sprintf(
						`%s cannot be updated, likely due to immutable fields not matching, you may `+
							`set spec["object-templates"][].recreateOption to recreate the object`,
						getMsgPrefix(&obj),
					)
				}

				r.setEvaluatedObject(obj.policy, obj.existingObj, false, message)

				return true, message, diff, false, nil
			}
		} else {
			removeFieldsForComparison(dryRunUpdatedObj)

			if reflect.DeepEqual(dryRunUpdatedObj.Object, existingObjectCopy.Object) {
				log.Info(
					"A mismatch was detected but a dry run update didn't make any changes. Assuming the object " +
						"is compliant.",
				)

				r.setEvaluatedObject(obj.policy, obj.existingObj, true, "")

				return false, "", "", false, nil
			}

			diff = handleDiff(log, recordDiff, isInform, existingObjectCopy, dryRunUpdatedObj)
		}

		// The object would have been updated, so if it's inform, return as noncompliant.
		if isInform {
			r.setEvaluatedObject(obj.policy, obj.existingObj, false, "")

			return true, "", diff, false, nil
		}

		// If it's not inform (i.e. enforce), update the object

		// At this point, if a recreate is needed, we know the user opted in, otherwise, the dry run update
		// failed and would have returned before now.
		if needsRecreate || objectT.RecreateOption == policyv1.Always {
			log.Info(
				"Deleting and recreating the object based on the template definition",
				"recreateOption", objectT.RecreateOption,
			)

			err = res.Delete(context.TODO(), obj.name, metav1.DeleteOptions{})
			if err != nil && !k8serrors.IsNotFound(err) {
				message = fmt.Sprintf(`%s failed to delete when recreating with the error %v`, getMsgPrefix(&obj), err)

				return true, message, "", updateNeeded, nil
			}

			attempts := 0

			for {
				updatedObj, err = res.Create(context.TODO(), &obj.desiredObj, metav1.CreateOptions{})
				if !k8serrors.IsAlreadyExists(err) {
					// If there is no error or the error is unexpected, break for the error handling below
					break
				}

				attempts++

				if attempts >= 3 {
					message = fmt.Sprintf(
						`%s timed out waiting for the object to delete during recreate, will retry on the next `+
							`policy evaluation`,
						getMsgPrefix(&obj),
					)

					return true, message, "", updateNeeded, nil
				}

				time.Sleep(time.Second)
			}
		} else {
			log.Info("Updating the object based on the template definition")

			updatedObj, err = res.Update(context.TODO(), obj.existingObj, metav1.UpdateOptions{
				FieldValidation: metav1.FieldValidationStrict,
			})
		}

		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("The object updated during the evaluation. Trying again.")

				rv, getErr := res.Get(context.TODO(), obj.existingObj.GetName(), metav1.GetOptions{})
				if getErr == nil {
					obj.existingObj = rv

					return r.checkAndUpdateResource(obj, objectT, remediation)
				}
			}

			action := "update"

			if needsRecreate || objectT.RecreateOption == policyv1.Always {
				action = "recreate"
			}

			message := getUpdateErrorMsg(err, obj.existingObj.GetKind(), obj.name)
			if message == "" {
				message = fmt.Sprintf("%s failed to %s with the error `%v`", getMsgPrefix(&obj), action, err)
			}

			return true, message, diff, updateNeeded, nil
		}

		if !statusMismatch {
			r.setEvaluatedObject(obj.policy, updatedObj, true, message)
		}
	} else {
		if throwSpecViolation && recordDiff != policyv1.RecordDiffNone {
			// The spec didn't require a change but throwSpecViolation indicates the status didn't match. Handle
			// this diff for this case.
			mergedObjCopy := obj.existingObj.DeepCopy()
			removeFieldsForComparison(mergedObjCopy)

			// The provided isInform value is always true because the status checking can only be inform.
			diff = handleDiff(log, recordDiff, true, existingObjectCopy, mergedObjCopy)
		}

		r.setEvaluatedObject(obj.policy, obj.existingObj, !throwSpecViolation, "")
	}

	return throwSpecViolation, "", diff, updateNeeded, updatedObj
}

func getMsgPrefix(obj *singleObject) string {
	var namespaceMsg string

	if obj.scopedGVR.Namespaced {
		namespaceMsg = fmt.Sprintf(" in namespace %s", obj.namespace)
	}

	return fmt.Sprintf(`%s [%s]%s`, obj.scopedGVR.Resource, obj.name, namespaceMsg)
}

// handleDiff will generate the diff and then log it or return it based on the input recordDiff value. If recordDiff
// is set to None or is set to InStatus with enforce, no diff is generated. This is because the diff is not relevant
// after the object is updated. When recordDiff is set to Censored, a message indicating so is returned.
func handleDiff(
	log logr.Logger,
	recordDiff policyv1.RecordDiff,
	isInform bool,
	existingObject *unstructured.Unstructured,
	mergedObject *unstructured.Unstructured,
) string {
	if !isInform && (recordDiff == policyv1.RecordDiffInStatus || recordDiff == policyv1.RecordDiffCensored) {
		return ""
	}

	var computedDiff string

	if recordDiff != policyv1.RecordDiffNone && recordDiff != policyv1.RecordDiffCensored {
		var err error

		computedDiff, err = generateDiff(existingObject, mergedObject)
		if err != nil {
			log.Error(err, "Failed to generate the diff")

			return ""
		}
	}

	switch recordDiff {
	case policyv1.RecordDiffNone:
		return ""
	case policyv1.RecordDiffLog:
		log.Info("Logging the diff:\n" + computedDiff)
	case policyv1.RecordDiffInStatus:
		return computedDiff
	case policyv1.RecordDiffCensored:
		return `# The difference is redacted because it contains sensitive data. To override, the ` +
			`spec["object-templates"][].recordDiff field must be set to "InStatus" for the difference to be recorded ` +
			`in the policy status. Consider existing access to the ConfigurationPolicy objects and the etcd ` +
			`encryption configuration before you proceed with an override.`
	}

	return ""
}

// handleKeys goes through all of the fields in the desired object and checks if the existing object
// matches. When a field is a map or slice, the value in the existing object will be updated with
// the result of merging its current value with the desired value.
func handleKeys(
	desiredObj unstructured.Unstructured,
	existingObj *unstructured.Unstructured,
	existingObjectCopy *unstructured.Unstructured,
	compType policyv1.ComplianceType,
	mdCompType policyv1.ComplianceType,
) (throwSpecViolation bool, message string, updateNeeded bool, statusMismatch bool) {
	handledKeys := map[string]bool{}

	// Iterate over keys of the desired object to compare with the existing object on the cluster
	for key := range desiredObj.Object {
		handledKeys[key] = true
		isStatus := key == "status"

		// use metadatacompliancetype to evaluate metadata if it is set
		keyComplianceType := compType
		if key == "metadata" && mdCompType != "" {
			keyComplianceType = mdCompType
		}

		// check key for mismatch
		errorMsg, keyUpdateNeeded, mergedObj, skipped := handleSingleKey(
			key, desiredObj, existingObjectCopy, keyComplianceType, false,
		)
		if errorMsg != "" {
			log.Info(errorMsg)

			return true, errorMsg, true, statusMismatch
		}

		if mergedObj == nil && skipped {
			continue
		}

		// only look at labels and annotations for metadata - configurationPolicies do not update other metadata fields
		if key == "metadata" {
			// if it's not the right type, the map will be empty
			mdMap, _ := mergedObj.(map[string]interface{})

			// if either isn't found, they'll just be empty
			mergedAnnotations, _, _ := unstructured.NestedStringMap(mdMap, "annotations")
			mergedLabels, _, _ := unstructured.NestedStringMap(mdMap, "labels")

			existingObj.SetAnnotations(mergedAnnotations)
			existingObj.SetLabels(mergedLabels)
		} else {
			existingObj.UnstructuredContent()[key] = mergedObj
		}

		if keyUpdateNeeded {
			if isStatus {
				throwSpecViolation = true
				statusMismatch = true
			} else {
				updateNeeded = true
			}
		}
	}

	// If the complianceType is "mustonlyhave", then compare the existing object's keys,
	// skipping over: previously compared keys, metadata, and status.
	if compType.IsMustOnlyHave() {
		for key := range existingObj.Object {
			if handledKeys[key] || key == "status" || key == "metadata" {
				continue
			}

			// for ServiceAccounts, ignore "secrets" and "imagePullSecrets" fields, as these are managed by Kubernetes
			if (existingObj.GetKind() == "ServiceAccount" && existingObj.GetAPIVersion() == "v1") &&
				(key == "secrets" || key == "imagePullSecrets") {
				continue
			}

			delete(existingObj.Object, key)

			updateNeeded = true
		}
	}

	return
}

func removeFieldsForComparison(obj *unstructured.Unstructured) {
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(
		obj.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration",
	)
	// The generation might actually bump but the API output might be the same.
	unstructured.RemoveNestedField(obj.Object, "metadata", "generation")
}

// setEvaluatedObject updates the cache to indicate that the ConfigurationPolicy has evaluated this
// object at its current resourceVersion.
func (r *ConfigurationPolicyReconciler) setEvaluatedObject(
	policy *policyv1.ConfigurationPolicy, currentObject *unstructured.Unstructured, compliant bool, msg string,
) {
	policyMap := &sync.Map{}

	loadedPolicyMap, loaded := r.processedPolicyCache.LoadOrStore(policy.GetUID(), policyMap)
	if loaded {
		policyMap = loadedPolicyMap.(*sync.Map)
	}

	policyMap.Store(
		currentObject.GetUID(),
		cachedEvaluationResult{
			resourceVersion: currentObject.GetResourceVersion(),
			compliant:       compliant,
			msg:             msg,
		},
	)
}

// alreadyEvaluated will determine if this ConfigurationPolicy has already evaluated this object at its current
// resourceVersion.
func (r *ConfigurationPolicyReconciler) alreadyEvaluated(
	policy *policyv1.ConfigurationPolicy, currentObject *unstructured.Unstructured,
) (evaluated bool, compliant bool, msg string) {
	if policy == nil || currentObject == nil {
		return false, false, ""
	}

	loadedPolicyMap, loaded := r.processedPolicyCache.Load(policy.GetUID())
	if !loaded {
		return false, false, ""
	}

	policyMap := loadedPolicyMap.(*sync.Map)

	result, loaded := policyMap.Load(currentObject.GetUID())
	if !loaded {
		return false, false, ""
	}

	resultTyped := result.(cachedEvaluationResult)

	return resultTyped.resourceVersion == currentObject.GetResourceVersion(), resultTyped.compliant, resultTyped.msg
}

func getUpdateErrorMsg(err error, kind string, name string) string {
	if k8serrors.IsNotFound(err) {
		return fmt.Sprintf("`%v` is not present and must be created", kind)
	}

	if err != nil && strings.Contains(err.Error(), "strict decoding error:") {
		return fmt.Sprintf("Error validating the object %s, the error is `%v`", name, err)
	}

	return ""
}

// addForUpdate calculates the compliance status of a configurationPolicy and updates the status field. The sendEvent
// argument determines if a status update event should be sent on the parent policy and configuration policy.
// Regardless of the sendEvent parameter, events will be sent if the compliance or policy generation changes.
func (r *ConfigurationPolicyReconciler) addForUpdate(policy *policyv1.ConfigurationPolicy, sendEvent bool) {
	compliant := true

	if policy.Spec == nil {
		compliant = false
	} else {
		for index := range policy.Status.CompliancyDetails {
			if policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
				compliant = false

				break
			}
		}
	}

	previousComplianceState := policy.Status.ComplianceState

	if policy.ObjectMeta.DeletionTimestamp != nil {
		policy.Status.ComplianceState = policyv1.Terminating
	} else if len(policy.Status.CompliancyDetails) == 0 {
		policy.Status.ComplianceState = policyv1.UnknownCompliancy
	} else if compliant {
		policy.Status.ComplianceState = policyv1.Compliant
	} else {
		policy.Status.ComplianceState = policyv1.NonCompliant
	}

	// Always send an event if the ComplianceState changed
	if previousComplianceState != policy.Status.ComplianceState {
		sendEvent = true
	}

	// Always try to send an event when the generation changes
	if policy.Status.LastEvaluatedGeneration != policy.Generation {
		sendEvent = true
	}

	policy.Status.LastEvaluated = time.Now().UTC().Format(time.RFC3339)
	policy.Status.LastEvaluatedGeneration = policy.Generation

	err := r.updatePolicyStatus(policy, sendEvent)
	policyLog := log.WithValues("name", policy.Name, "namespace", policy.Namespace)

	if k8serrors.IsConflict(err) {
		policyLog.Error(err, "Tried to re-update status before previous update could be applied, retrying next loop")
	} else if err != nil {
		policyLog.Error(err, "Could not update status, will retry")

		parent := ""
		if len(policy.OwnerReferences) > 0 {
			parent = policy.OwnerReferences[0].Name
		}

		policySystemErrorsCounter.WithLabelValues(parent, policy.GetName(), "status-update-failed").Add(1)
	} else {
		r.lastEvaluatedCache.Store(policy.UID, policy.Status.LastEvaluated)
	}
}

// updatePolicyStatus updates the status of the configurationPolicy if new conditions are added and generates an event
// on the parent policy and configuration policy with the compliance decision if the sendEvent argument is true.
func (r *ConfigurationPolicyReconciler) updatePolicyStatus(
	policy *policyv1.ConfigurationPolicy, sendEvent bool,
) error {
	if sendEvent {
		log.Info("Sending parent policy compliance event")

		// If the compliance event can't be created, then don't update the ConfigurationPolicy
		// status. As long as that hasn't been updated, everything will be retried next loop.
		if err := r.sendComplianceEvent(policy); err != nil {
			return err
		}
	}

	log.V(2).Info(
		"Updating configurationPolicy status", "status", policy.Status.ComplianceState, "policy", policy.GetName(),
	)

	evaluatedUID := policy.UID
	updatedStatus := policy.Status

	maxRetries := 3
	for i := 1; i <= maxRetries; i++ {
		err := r.Get(context.TODO(), types.NamespacedName{Namespace: policy.Namespace, Name: policy.Name}, policy)
		if err != nil {
			log.Info(fmt.Sprintf("Failed to refresh policy; using previously fetched version: %s", err))
		} else {
			policy.Status = updatedStatus

			// If the UID has changed, then the policy has been deleted and created again. Do not update the status,
			// because it was calculated based on a previous version. If sendEvent is true, that event might be useful
			// and it can be emitted. By leaving the status blank, the policy will be reevaluated and send a new event.
			if evaluatedUID != policy.UID {
				log.Info("The ConfigurationPolicy was recreated after it was evaluated. Skipping the status update.")

				// Reset the original UID so that if there are more status updates (i.e. batches), the status on the
				// API server is never updated.
				policy.UID = evaluatedUID

				break
			}
		}

		err = r.Status().Update(context.TODO(), policy)
		if err != nil {
			if i == maxRetries {
				return err
			}

			log.Info(fmt.Sprintf("Failed to update policy status. Retrying (attempt %d/%d): %s", i, maxRetries, err))
		} else {
			break
		}
	}

	if sendEvent {
		log.V(1).Info("Sending policy status update event")

		condMessages := make([]string, 0, len(policy.Status.CompliancyDetails))

		for _, compliancyDetail := range policy.Status.CompliancyDetails {
			if len(compliancyDetail.Conditions) != 0 {
				// NOTE: this will drop additional conditions, if there is more than one.
				// There should only ever be one condition at a time, either of type 'violation' if the
				// resource is NonCompliant, or type 'notification' if it is Compliant.
				condMessages = append(condMessages, compliancyDetail.Conditions[0].Message)
			}
		}

		eventType := eventNormal
		if policy.Status.ComplianceState == policyv1.NonCompliant {
			eventType = eventWarning
		}

		eventMessage := fmt.Sprintf("%s: %s", policy.Status.ComplianceState, strings.Join(condMessages, "; "))
		log.Info("Policy status message", "policy", policy.GetName(), "status", eventMessage)

		r.Recorder.Event(
			policy,
			eventType,
			"Policy updated",
			fmt.Sprintf("Policy status is %s", eventMessage),
		)
	}

	return nil
}

// recordInfoEvent adds an informational event to the queue to be emitted (it does not emit it
// synchronously). This event is not used for compliance, but may be used by other tools.
func (r *ConfigurationPolicyReconciler) recordInfoEvent(plc *policyv1.ConfigurationPolicy, violation bool) {
	eventType := eventNormal
	if violation {
		eventType = eventWarning
	}

	r.Recorder.Event(
		plc,
		eventType,
		"policy: "+plc.GetName(),
		// Always use the default message for info events
		defaultComplianceMessage(plc),
	)
}

func (r *ConfigurationPolicyReconciler) sendComplianceEvent(instance *policyv1.ConfigurationPolicy) error {
	if len(instance.OwnerReferences) == 0 {
		return nil // there is nothing to do, since no owner is set
	}

	// we are making an assumption that the GRC policy has a single owner, or we chose the first owner in the list
	ownerRef := instance.OwnerReferences[0]
	now := time.Now()
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			// This event name matches the convention of recorders from client-go
			Name:      fmt.Sprintf("%v.%x", ownerRef.Name, now.UnixNano()),
			Namespace: instance.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       ownerRef.Kind,
			Namespace:  instance.Namespace, // k8s ensures owners are always in the same namespace
			Name:       ownerRef.Name,
			UID:        ownerRef.UID,
			APIVersion: ownerRef.APIVersion,
		},
		Reason:  fmt.Sprintf(eventFmtStr, instance.Namespace, instance.Name),
		Message: r.customComplianceMessage(instance),
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
			Kind:       instance.Kind,
			Namespace:  instance.Namespace,
			Name:       instance.Name,
			UID:        instance.UID,
			APIVersion: instance.APIVersion,
		},
		ReportingController: ControllerName,
		ReportingInstance:   r.InstanceName,
	}

	eventAnnotations := map[string]string{}

	instanceAnnotations := instance.GetAnnotations()
	if instanceAnnotations[common.ParentDBIDAnnotation] != "" {
		eventAnnotations[common.ParentDBIDAnnotation] = instanceAnnotations[common.ParentDBIDAnnotation]
	}

	if instanceAnnotations[common.PolicyDBIDAnnotation] != "" {
		eventAnnotations[common.PolicyDBIDAnnotation] = instanceAnnotations[common.PolicyDBIDAnnotation]
	}

	if len(eventAnnotations) > 0 {
		event.Annotations = eventAnnotations
	}

	if instance.Status.ComplianceState != policyv1.Compliant {
		event.Type = "Warning"
	}

	return r.Create(context.TODO(), event)
}

// defaultComplianceMessage looks through the policy's Compliance and CompliancyDetails and formats
// a message that can be used for compliance events recognized by the framework.
func defaultComplianceMessage(plc *policyv1.ConfigurationPolicy) string {
	if plc.Status.ComplianceState == "" || plc.Status.ComplianceState == policyv1.UnknownCompliancy {
		return "ComplianceState is still unknown"
	}

	defaultTemplate := `
	{{- range .Status.CompliancyDetails -}}
	  ; {{ if (index .Conditions 0) -}}
	    {{- (index .Conditions 0).Type }} - {{ (index .Conditions 0).Message -}}
	  {{- end -}}
	{{- end }}`

	// `Must` is ok here because an invalid template would be caught by tests
	t := template.Must(template.New("default-msg").Parse(defaultTemplate))

	var result strings.Builder

	result.WriteString(string(plc.Status.ComplianceState))

	if err := t.Execute(&result, plc); err != nil {
		log.Error(err, "failed to execute default template", "PolicyName", plc.Name)

		// Fallback to just returning the compliance state - this will be recognized by the framework,
		// but will be missing any details.
		return string(plc.Status.ComplianceState)
	}

	return result.String()
}

// customComplianceMessage uses the custom template in the policy (if provided by the user) to
// format a compliance message that can be used by the framework. If an error occurs with the
// template, the default message will be used, appended with details for the error. If no custom
// template was specified for the current compliance, then the default message is used.
func (r *ConfigurationPolicyReconciler) customComplianceMessage(plc *policyv1.ConfigurationPolicy) string {
	customTemplate := plc.Spec.CustomMessage.Compliant

	if plc.Status.ComplianceState != policyv1.Compliant {
		customTemplate = plc.Spec.CustomMessage.NonCompliant
	}

	defaultMessage := defaultComplianceMessage(plc)

	// No custom template was provided for the current situation
	if customTemplate == "" {
		return defaultMessage
	}

	customMessage, err := r.doCustomMessage(plc, customTemplate, defaultMessage)
	if err != nil {
		return fmt.Sprintf("%v (failure processing the custom message: %v)", defaultMessage, err.Error())
	}

	// Add the compliance prefix if not present (it is required by the framework)
	if !strings.HasPrefix(customMessage, string(plc.Status.ComplianceState)+"; ") {
		customMessage = string(plc.Status.ComplianceState) + "; " + customMessage
	}

	return customMessage
}

// doCustomMessage parses and executes the custom template, returning an error if something goes
// wrong. The data that the template receives includes the '.DefaultMessage' string and a '.Policy'
// object, which has the full current state of the configuration policy, including status fields
// like relatedObjects. If the policy is using the dynamic watcher, then the '.object' field on each
// related object will have the *full* current state of that object, otherwise only some identifying
// information is available there.
func (r *ConfigurationPolicyReconciler) doCustomMessage(
	plc *policyv1.ConfigurationPolicy, customTemplate string, defaultMessage string,
) (string, error) {
	tmpl, err := template.New("custom-msg").Parse(customTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse custom template: %w", err)
	}

	// Converting the policy to a map allows users to access fields via the yaml/json field names
	// (ie the lowercase versions), which they are likely more familiar with.
	plcMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(plc)
	if err != nil {
		return "", fmt.Errorf("failed to convert policy to unstructured: %w)", err)
	}

	// Only add the full related object information when it can be pulled from the cache
	if currentlyUsingWatch(plc) {
		// Paranoid checks to ensure that the policy has a status of the right format
		plcStatus, ok := plcMap["status"].(map[string]any)
		if !ok {
			goto messageTemplating
		}

		relObjs, ok := plcStatus["relatedObjects"].([]any)
		if !ok {
			goto messageTemplating
		}

		for i, relObj := range plc.Status.RelatedObjects {
			objNS := relObj.Object.Metadata.Namespace
			objName := relObj.Object.Metadata.Name
			objGVK := schema.FromAPIVersionAndKind(relObj.Object.APIVersion, relObj.Object.Kind)

			fullObj, err := r.getObjectFromCache(plc, objNS, objName, objGVK)
			if err == nil && fullObj != nil {
				if _, ok := relObjs[i].(map[string]any); ok {
					relObjs[i].(map[string]any)["object"] = fullObj.Object
				}
			}
		}
	}

messageTemplating:
	templateData := map[string]any{
		"DefaultMessage": defaultMessage,
		"Policy":         plcMap,
	}

	var customMsg strings.Builder

	if err := tmpl.Execute(&customMsg, templateData); err != nil {
		return "", fmt.Errorf("failed to execute: %w", err)
	}

	return customMsg.String(), nil
}

// getDeployment gets the Deployment object associated with this controller. If the controller is running outside of
// a cluster, no Deployment object or error will be returned.
func getDeployment(client client.Client) (*appsv1.Deployment, error) {
	key, err := common.GetOperatorNamespacedName()
	if err != nil {
		// Running locally
		if errors.Is(err, common.ErrNoNamespace) || errors.Is(err, common.ErrRunLocal) {
			return nil, nil
		}

		return nil, err
	}

	deployment := appsv1.Deployment{}
	if err := client.Get(context.TODO(), key, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

func IsBeingUninstalled(client client.Client) (bool, error) {
	deployment, err := getDeployment(client)
	if deployment == nil || err != nil {
		return false, err
	}

	return deployment.Annotations[common.UninstallingAnnotation] == "true", nil
}
