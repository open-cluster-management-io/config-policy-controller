// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	templates "github.com/stolostron/go-template-utils/v3/pkg/templates"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
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
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/record"
	kubeopenapivalidation "k8s.io/kube-openapi/pkg/util/proto/validation"
	"k8s.io/kubectl/pkg/util/openapi"
	openapivalidation "k8s.io/kubectl/pkg/util/openapi/validation"
	"k8s.io/kubectl/pkg/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	yaml "sigs.k8s.io/yaml"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	common "open-cluster-management.io/config-policy-controller/pkg/common"
)

const (
	ControllerName       string = "configuration-policy-controller"
	CRDName              string = "configurationpolicies.policy.open-cluster-management.io"
	pruneObjectFinalizer string = "policy.open-cluster-management.io/delete-related-objects"
)

var log = ctrl.Log.WithName(ControllerName)

// PlcChan a channel used to pass policies ready for update
var PlcChan chan *policyv1.ConfigurationPolicy

var (
	eventNormal  = "Normal"
	eventWarning = "Warning"
	eventFmtStr  = "policy: %s/%s"
	plcFmtStr    = "policy: %s"
)

var (
	reasonWantFoundExists    = "Resource found as expected"
	reasonWantFoundNoMatch   = "Resource found but does not match"
	reasonWantFoundDNE       = "Resource not found but should exist"
	reasonWantNotFoundExists = "Resource found but should not exist"
	reasonWantNotFoundDNE    = "Resource not found as expected"
	reasonCleanupError       = "Error cleaning up child objects"
)

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigurationPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&policyv1.ConfigurationPolicy{}).
		Complete(r)
}

// blank assignment to verify that ConfigurationPolicyReconciler implements reconcile.Reconciler
var _ reconcile.Reconciler = &ConfigurationPolicyReconciler{}

type cachedEncryptionKey struct {
	key         []byte
	previousKey []byte
}

// nolint: structcheck
type discoveryInfo struct {
	apiResourceList        []*metav1.APIResourceList
	apiGroups              []*restmapper.APIGroupResources
	discoveryLastRefreshed time.Time
}

// ConfigurationPolicyReconciler reconciles a ConfigurationPolicy object
type ConfigurationPolicyReconciler struct {
	cachedEncryptionKey *cachedEncryptionKey
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
	DecryptionConcurrency uint8
	// Determines the number of Go routines that can evaluate policies concurrently.
	EvaluationConcurrency uint8
	Scheme                *runtime.Scheme
	Recorder              record.EventRecorder
	InstanceName          string
	// The Kubernetes client to use when evaluating/enforcing policies. Most times, this will be the same cluster
	// where the controller is running.
	TargetK8sClient        kubernetes.Interface
	TargetK8sDynamicClient dynamic.Interface
	TargetK8sConfig        *rest.Config
	// Whether custom metrics collection is enabled
	EnableMetrics bool
	discoveryInfo
	// This is used to fetch and parse OpenAPI documents to perform client-side validation of object definitions.
	openAPIParser *openapi.CachedOpenAPIParser
	// A lock when performing actions that are not thread safe (i.e. reassigning object properties).
	lock sync.RWMutex
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile currently does nothing except that it removes a policy's metric when the policy is deleted. All the logic
// is handled in the PeriodicallyExecConfigPolicies method.
func (r *ConfigurationPolicyReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	policy := &policyv1.ConfigurationPolicy{}

	err := r.Get(ctx, request.NamespacedName, policy)
	if k8serrors.IsNotFound(err) {
		// If the metric was not deleted, that means the policy was never evaluated so it can be ignored.
		_ = policyEvalSecondsCounter.DeleteLabelValues(request.Name)
		_ = policyEvalCounter.DeleteLabelValues(request.Name)
		_ = plcTempsProcessSecondsCounter.DeleteLabelValues(request.Name)
		_ = plcTempsProcessCounter.DeleteLabelValues(request.Name)
		_ = compareObjEvalCounter.DeletePartialMatch(prometheus.Labels{"config_policy_name": request.Name})
		_ = compareObjSecondsCounter.DeletePartialMatch(prometheus.Labels{"config_policy_name": request.Name})
		_ = policyRelatedObjectGauge.DeletePartialMatch(
			prometheus.Labels{"policy": fmt.Sprintf("%s/%s", request.Namespace, request.Name)})
		_ = policyUserErrorsCounter.DeletePartialMatch(prometheus.Labels{"template": request.Name})
		_ = policySystemErrorsCounter.DeletePartialMatch(prometheus.Labels{"template": request.Name})
	}

	return reconcile.Result{}, nil
}

// PeriodicallyExecConfigPolicies loops through all configurationpolicies in the target namespace and triggers
// template handling for each one. This function drives all the work the configuration policy controller does.
func (r *ConfigurationPolicyReconciler) PeriodicallyExecConfigPolicies(
	ctx context.Context, freq uint, elected <-chan struct{},
) {
	log.Info("Waiting for leader election before periodically evaluating configuration policies")

	select {
	case <-elected:
	case <-ctx.Done():
		return
	}

	const waiting = 10 * time.Minute

	exiting := false
	deploymentFinalizerRemoved := false

	for !exiting {
		if !deploymentFinalizerRemoved {
			err := r.removeLegacyDeploymentFinalizer()
			if err == nil {
				deploymentFinalizerRemoved = true
			} else {
				log.Error(err, "Failed to remove the legacy Deployment finalizer. Will try again.")
			}
		}

		start := time.Now()
		policiesList := policyv1.ConfigurationPolicyList{}

		var skipLoop bool
		var discoveryErr error

		if len(r.apiResourceList) == 0 || len(r.apiGroups) == 0 {
			discoveryErr = r.refreshDiscoveryInfo()
		}

		// If it's been more than 10 minutes since the last refresh, then refresh the discovery info, but ignore
		// any errors since the cache can still be used. If a policy encounters an API resource type not in the
		// cache, the discovery info refresh will be handled there. This periodic refresh is to account for
		// deleted CRDs or strange edits to the CRD (e.g. converted it from namespaced to not).
		if time.Since(r.discoveryLastRefreshed) >= waiting {
			_ = r.refreshDiscoveryInfo()
		}

		if discoveryErr == nil {
			// This retrieves the policies from the controller-runtime cache populated by the watch.
			err := r.List(context.TODO(), &policiesList)
			if err != nil {
				log.Error(err, "Failed to list the ConfigurationPolicy objects to evaluate")

				skipLoop = true
			}
		} else {
			skipLoop = true
		}

		cleanupImmediately, err := r.cleanupImmediately()
		if err != nil {
			log.Error(err, "Failed to determine if it's time to cleanup immediately")

			skipLoop = true
		}

		// This is done every loop cycle since the channel needs to be variable in size to account for the number of
		// policies changing.
		policyQueue := make(chan *policyv1.ConfigurationPolicy, len(policiesList.Items))
		var wg sync.WaitGroup

		if !skipLoop {
			log.Info("Processing the policies", "count", len(policiesList.Items))

			// Initialize the related object map
			policyRelatedObjectMap = sync.Map{}

			for i := 0; i < int(r.EvaluationConcurrency); i++ {
				wg.Add(1)

				go r.handlePolicyWorker(policyQueue, &wg)
			}

			for i := range policiesList.Items {
				policy := policiesList.Items[i]
				if !shouldEvaluatePolicy(&policy, cleanupImmediately) {
					continue
				}

				// handle each template in each policy
				policyQueue <- &policy
			}
		}

		close(policyQueue)
		wg.Wait()

		// Update the related object metric after policy processing
		if r.EnableMetrics {
			updateRelatedObjectMetric()
		}
		// Update the evaluation histogram with the elapsed time
		elapsed := time.Since(start).Seconds()
		evalLoopHistogram.Observe(elapsed)
		// making sure that if processing is > freq we don't sleep
		// if freq > processing we sleep for the remaining duration
		if float64(freq) > elapsed {
			remainingSleep := float64(freq) - elapsed
			sleepTime := time.Duration(remainingSleep) * time.Second
			log.V(2).Info("Sleeping before reprocessing the configuration policies", "seconds", sleepTime)
			time.Sleep(sleepTime)
		}

		select {
		case <-ctx.Done():
			exiting = true
		default:
		}
	}
}

// handlePolicyWorker is meant to be used as a Go routine that wraps handleObjectTemplates.
func (r *ConfigurationPolicyReconciler) handlePolicyWorker(
	policyQueue <-chan *policyv1.ConfigurationPolicy, wg *sync.WaitGroup,
) {
	defer wg.Done()

	for policy := range policyQueue {
		before := time.Now().UTC()

		r.handleObjectTemplates(*policy)

		duration := time.Now().UTC().Sub(before)
		seconds := float64(duration) / float64(time.Second)

		policyEvalSecondsCounter.WithLabelValues(policy.Name).Add(seconds)
		policyEvalCounter.WithLabelValues(policy.Name).Inc()
	}
}

func (r *ConfigurationPolicyReconciler) refreshDiscoveryInfo() error {
	log.V(2).Info("Refreshing the discovery info")
	r.lock.Lock()
	defer func() { r.lock.Unlock() }()

	dd := r.TargetK8sClient.Discovery()

	_, apiResourceList, err := dd.ServerGroupsAndResources()
	if err != nil {
		log.Error(err, "Could not get the API resource list")

		return err
	}

	r.apiResourceList = apiResourceList

	apiGroups, err := restmapper.GetAPIGroupResources(dd)
	if err != nil {
		log.Error(err, "Could not get the API groups list")

		return err
	}

	r.apiGroups = apiGroups
	// Reset the OpenAPI cache in case the CRDs were updated since the last fetch.
	r.openAPIParser = openapi.NewOpenAPIParser(dd)
	r.discoveryLastRefreshed = time.Now().UTC()

	return nil
}

// shouldEvaluatePolicy will determine if the policy is ready for evaluation by examining the
// status.lastEvaluated and status.lastEvaluatedGeneration fields. If a policy has been updated, it
// will always be triggered for evaluation. If the spec.evaluationInterval configuration has been
// met, then that will also trigger an evaluation. If cleanupImmediately is true, then only policies
// with finalizers will be ready for evaluation regardless of the last evaluation.
// cleanupImmediately should be set true when the controller is getting uninstalled.
func shouldEvaluatePolicy(policy *policyv1.ConfigurationPolicy, cleanupImmediately bool) bool {
	log := log.WithValues("policy", policy.GetName())

	// If it's time to clean up such as when the config-policy-controller is being uninstalled, only evaluate policies
	// with a finalizer to remove the finalizer.
	if cleanupImmediately {
		return len(policy.Finalizers) != 0
	}

	if policy.ObjectMeta.DeletionTimestamp != nil {
		log.V(2).Info("The policy has been deleted and is waiting for object cleanup. Will evaluate it now.")

		return true
	}

	if policy.Status.LastEvaluatedGeneration != policy.Generation {
		log.V(2).Info("The policy has been updated. Will evaluate it now.")

		return true
	}

	if policy.Status.LastEvaluated == "" {
		log.V(2).Info("The policy's status.lastEvaluated field is not set. Will evaluate it now.")

		return true
	}

	lastEvaluated, err := time.Parse(time.RFC3339, policy.Status.LastEvaluated)
	if err != nil {
		log.Error(err, "The policy has an invalid status.lastEvaluated value. Will evaluate it now.")

		return true
	}

	var interval time.Duration

	if policy.Status.ComplianceState == policyv1.Compliant && policy.Spec != nil {
		interval, err = policy.Spec.EvaluationInterval.GetCompliantInterval()
	} else if policy.Status.ComplianceState == policyv1.NonCompliant && policy.Spec != nil {
		interval, err = policy.Spec.EvaluationInterval.GetNonCompliantInterval()
	} else {
		log.V(2).Info("The policy has an unknown compliance. Will evaluate it now.")

		return true
	}

	if errors.Is(err, policyv1.ErrIsNever) {
		log.Info("Skipping the policy evaluation due to the spec.evaluationInterval value being set to never")

		return false
	} else if err != nil {
		log.Error(
			err,
			"The policy has an invalid spec.evaluationInterval value. Will evaluate it now.",
			"spec.evaluationInterval.compliant", policy.Spec.EvaluationInterval.Compliant,
			"spec.evaluationInterval.noncompliant", policy.Spec.EvaluationInterval.NonCompliant,
		)

		return true
	}

	nextEvaluation := lastEvaluated.Add(interval)
	if nextEvaluation.Sub(time.Now().UTC()) > 0 {
		log.Info("Skipping the policy evaluation due to the policy not reaching the evaluation interval")

		return false
	}

	return true
}

// getTemplateConfigErrorMsg converts a configuration error from `NewResolver` or `SetEncryptionConfig` to a message
// to be used as a policy noncompliant message.
func getTemplateConfigErrorMsg(err error) string {
	if errors.Is(err, templates.ErrInvalidAESKey) || errors.Is(err, templates.ErrAESKeyNotSet) {
		return `The "policy-encryption-key" Secret contains an invalid AES key`
	} else if errors.Is(err, templates.ErrInvalidIV) {
		return fmt.Sprintf(`The "%s" annotation value is not a valid initialization vector`, IVAnnotation)
	}

	// This should never happen unless go-template-utils is updated and this doesn't account for a new error
	// type that can happen with the input configuration.
	return fmt.Sprintf("An unexpected error occurred when configuring the template resolver: %v", err)
}

type objectTemplateDetails struct {
	kind         string
	name         string
	namespace    string
	isNamespaced bool
}

// getObjectTemplateDetails retrieves values from the object templates and returns an array of
// objects containing the retrieved values.
// It also gathers namespaces for this policy if necessary:
//
//	If a namespaceSelector is present AND objects are namespaced without a namespace specified
func (r *ConfigurationPolicyReconciler) getObjectTemplateDetails(
	plc policyv1.ConfigurationPolicy,
) ([]objectTemplateDetails, []string, bool, error) {
	templateObjs := make([]objectTemplateDetails, len(plc.Spec.ObjectTemplates))
	selectedNamespaces := []string{}
	queryNamespaces := false

	for idx, objectT := range plc.Spec.ObjectTemplates {
		unstruct, err := unmarshalFromJSON(objectT.ObjectDefinition.Raw)
		if err != nil {
			return templateObjs, selectedNamespaces, false, err
		}

		templateObjs[idx].isNamespaced = r.isObjectNamespaced(&unstruct, true)
		// strings.TrimSpace() is needed here because a multi-line value will have '\n' in it
		templateObjs[idx].kind = strings.TrimSpace(unstruct.GetKind())
		templateObjs[idx].name = strings.TrimSpace(unstruct.GetName())
		templateObjs[idx].namespace = strings.TrimSpace(unstruct.GetNamespace())

		if templateObjs[idx].isNamespaced && templateObjs[idx].namespace == "" {
			queryNamespaces = true
		}
	}

	// If required, query for namespaces specified in NamespaceSelector for objects to use
	if queryNamespaces {
		// Retrieve the namespaces based on filters in NamespaceSelector
		selector := plc.Spec.NamespaceSelector
		// If MatchLabels/MatchExpressions/Include were not provided, return no namespaces
		if selector.MatchLabels == nil && selector.MatchExpressions == nil && len(selector.Include) == 0 {
			log.Info("namespaceSelector is empty. Skipping namespace retrieval.")
		} else {
			// If an error occurred in the NamespaceSelector, update the policy status and abort
			var err error
			selectedNamespaces, err = common.GetSelectedNamespaces(r.TargetK8sClient, selector)
			if err != nil {
				errMsg := "Error filtering namespaces with provided namespaceSelector"
				log.Error(
					err, errMsg,
					"namespaceSelector", fmt.Sprintf("%+v", selector))

				reason := "namespaceSelector error"
				msg := fmt.Sprintf(
					"%s: %s", errMsg, err.Error())
				statusChanged := addConditionToStatus(&plc, 0, false, reason, msg)
				if statusChanged {
					r.Recorder.Event(
						&plc,
						eventWarning,
						fmt.Sprintf(plcFmtStr, plc.GetName()),
						convertPolicyStatusToString(&plc),
					)
				}

				return templateObjs, selectedNamespaces, statusChanged, err
			}

			if len(selectedNamespaces) == 0 {
				log.Info("Fetching namespaces with provided NamespaceSelector returned no namespaces.",
					"namespaceSelector", fmt.Sprintf("%+v", selector))
			}
		}
	}

	return templateObjs, selectedNamespaces, false, nil
}

func (r *ConfigurationPolicyReconciler) cleanUpChildObjects(plc policyv1.ConfigurationPolicy) []string {
	deletionFailures := []string{}

	for _, object := range plc.Status.RelatedObjects {
		// set up client for object deletion
		gvk := schema.FromAPIVersionAndKind(object.Object.APIVersion, object.Object.Kind)

		log := log.WithValues("policy", plc.GetName(), "groupVersionKind", gvk.String())

		r.lock.RLock()
		mapper := restmapper.NewDiscoveryRESTMapper(r.apiGroups)
		r.lock.RUnlock()

		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			log.Error(err, "Could not get resource mapping for child object")

			deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
				object.Object.Metadata.Name, object.Object.Metadata.Namespace))

			continue
		}

		namespaced := object.Object.Metadata.Namespace != ""

		// determine whether object should be deleted
		needsDelete := false

		existing, _ := getObject(
			namespaced,
			object.Object.Metadata.Namespace,
			object.Object.Metadata.Name,
			mapping.Resource,
			r.TargetK8sDynamicClient,
		)

		// object does not exist, no deletion logic needed
		if existing == nil {
			continue
		}

		if strings.EqualFold(string(plc.Spec.RemediationAction), "enforce") {
			if string(plc.Spec.PruneObjectBehavior) == "DeleteAll" {
				needsDelete = true
			} else {
				// if prune behavior is DeleteIfCreated, we need to check whether createdByPolicy
				// is true and the UID is not stale
				uid, uidFound, err := unstructured.NestedString(existing.Object, "metadata", "uid")

				if !uidFound || err != nil {
					log.Error(err, "Tried to pull UID from obj but the field did not exist or was not a string")
				} else if object.Properties != nil &&
					object.Properties.CreatedByPolicy != nil &&
					*object.Properties.CreatedByPolicy &&
					object.Properties.UID == uid {
					needsDelete = true
				}
			}
		}

		// delete object if needed
		if needsDelete {
			// if object has already been deleted and is stuck, no need to redo delete request
			_, deletionTimeFound, _ := unstructured.NestedString(existing.Object, "metadata", "deletionTimestamp")
			if deletionTimeFound {
				log.Error(err, "Error: tried to delete object, but delete is hanging")

				deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
					object.Object.Metadata.Name, object.Object.Metadata.Namespace))

				continue
			}

			var res dynamic.ResourceInterface
			if namespaced {
				res = r.TargetK8sDynamicClient.Resource(mapping.Resource).Namespace(object.Object.Metadata.Namespace)
			} else {
				res = r.TargetK8sDynamicClient.Resource(mapping.Resource)
			}

			if completed, err := deleteObject(res, object.Object.Metadata.Name,
				object.Object.Metadata.Namespace); !completed {
				deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
					object.Object.Metadata.Name, object.Object.Metadata.Namespace))

				log.Error(err, "Error: Failed to delete object during child object pruning")
			} else {
				obj, _ := getObject(
					namespaced,
					object.Object.Metadata.Namespace,
					object.Object.Metadata.Name,
					mapping.Resource,
					r.TargetK8sDynamicClient,
				)

				if obj != nil {
					log.Error(err, "Error: tried to delete object, but delete is hanging")

					deletionFailures = append(deletionFailures, gvk.String()+fmt.Sprintf(` "%s" in namespace %s`,
						object.Object.Metadata.Name, object.Object.Metadata.Namespace))

					continue
				}

				log.Info("Object successfully deleted as part of child object pruning")
			}
		}
	}

	return deletionFailures
}

// cleanupImmediately returns true when the cluster is in a state where configurationpolicies should
// be removed as soon as possible, ignoring the pruneObjectBehavior of the policies. This is the
// case when the controller is being uninstalled or the CRD is being deleted.
func (r *ConfigurationPolicyReconciler) cleanupImmediately() (bool, error) {
	beingUninstalled, beingUninstalledErr := r.isBeingUninstalled()
	if beingUninstalledErr == nil && beingUninstalled {
		return true, nil
	}

	defDeleting, defErr := r.definitionIsDeleting()
	if defErr == nil && defDeleting {
		return true, nil
	}

	if beingUninstalledErr == nil && defErr == nil {
		// if either was deleting, we would've already returned.
		return false, nil
	}

	// At least one had an unexpected error, so the decision can't be made right now
	//nolint:errorlint // we can't choose just one of the errors to "correctly" wrap
	return false, fmt.Errorf(
		"isBeingUninstalled error: '%v', definitionIsDeleting error: '%v'", beingUninstalledErr, defErr,
	)
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

// handleObjectTemplates iterates through all policy templates in a given policy and processes them
func (r *ConfigurationPolicyReconciler) handleObjectTemplates(plc policyv1.ConfigurationPolicy) {
	log := log.WithValues("policy", plc.GetName())
	log.V(1).Info("Processing object templates")

	// initialize the RelatedObjects for this Configuration Policy
	oldRelated := append([]policyv1.RelatedObject{}, plc.Status.RelatedObjects...)
	relatedObjects := []policyv1.RelatedObject{}
	parentStatusUpdateNeeded := false

	validationErr := ""
	if plc.Spec == nil {
		validationErr = "Policy does not have a Spec specified"
	} else if plc.Spec.RemediationAction == "" {
		validationErr = "Policy does not have a RemediationAction specified"
	}

	// error if no spec or remediationAction is specified
	if validationErr != "" {
		message := validationErr
		log.Info(message)
		statusChanged := addConditionToStatus(&plc, 0, false, "Invalid spec", message)

		if statusChanged {
			r.Recorder.Event(&plc, eventWarning,
				fmt.Sprintf(plcFmtStr, plc.GetName()), convertPolicyStatusToString(&plc))
		}

		r.checkRelatedAndUpdate(plc, relatedObjects, oldRelated, statusChanged)

		parent := ""
		if len(plc.OwnerReferences) > 0 {
			parent = plc.OwnerReferences[0].Name
		}

		policyUserErrorsCounter.WithLabelValues(parent, plc.GetName(), "invalid-template").Add(1)

		return
	}

	// object handling for when configurationPolicy is deleted
	if plc.Spec.PruneObjectBehavior == "DeleteIfCreated" || plc.Spec.PruneObjectBehavior == "DeleteAll" {
		cleanupNow, err := r.cleanupImmediately()
		if err != nil {
			log.Error(err, "Error determining whether to cleanup immediately, requeueing policy")

			return
		}

		if cleanupNow {
			if objHasFinalizer(&plc, pruneObjectFinalizer) {
				plc.SetFinalizers(removeObjFinalizer(&plc, pruneObjectFinalizer))

				err := r.Update(context.TODO(), &plc)
				if err != nil {
					log.V(1).Error(err, "Error removing finalizer for configuration policy", plc)
				}
			}

			return
		}

		// set finalizer if it hasn't been set
		if !objHasFinalizer(&plc, pruneObjectFinalizer) {
			plc.SetFinalizers(addObjFinalizer(&plc, pruneObjectFinalizer))

			err := r.Update(context.TODO(), &plc)
			if err != nil {
				log.V(1).Error(err, "Error setting finalizer for configuration policy", plc)
			}
		}

		// kick off object deletion if configurationPolicy has been deleted
		if plc.ObjectMeta.DeletionTimestamp != nil {
			log.V(1).Info("Config policy has been deleted, handling child objects")

			failures := r.cleanUpChildObjects(plc)

			if len(failures) == 0 {
				log.V(1).Info("Objects have been successfully cleaned up, removing finalizer")
				plc.SetFinalizers(removeObjFinalizer(&plc, pruneObjectFinalizer))

				err := r.Update(context.TODO(), &plc)
				if err != nil {
					log.V(1).Error(err, "Error unsetting finalizer for configuration policy", plc)
				}
			} else {
				log.V(1).Info("Object cleanup failed, some objects have not been deleted from the cluster")

				statusChanged := addConditionToStatus(
					&plc,
					0,
					false,
					reasonCleanupError,
					"Failed to delete objects: "+strings.Join(failures, ", "))
				if statusChanged {
					parentStatusUpdateNeeded = true

					r.Recorder.Event(
						&plc,
						eventWarning,
						fmt.Sprintf(plcFmtStr, plc.GetName()),
						convertPolicyStatusToString(&plc),
					)
				}

				// don't change related objects while deletion is in progress
				r.checkRelatedAndUpdate(plc, oldRelated, oldRelated, parentStatusUpdateNeeded)
			}

			return
		}
	} else if objHasFinalizer(&plc, pruneObjectFinalizer) {
		// if pruneObjectBehavior is none, no finalizer is needed
		plc.SetFinalizers(removeObjFinalizer(&plc, pruneObjectFinalizer))
		err := r.Update(context.TODO(), &plc)
		if err != nil {
			log.V(1).Error(err, "Error unsetting finalizer for configuration policy", plc)
		}
	}

	addTemplateErrorViolation := func(reason, msg string) {
		log.Info("Setting the policy to noncompliant due to a templating error", "error", msg)

		if reason == "" {
			reason = "Error processing template"
		}

		statusChanged := addConditionToStatus(&plc, 0, false, reason, msg)
		if statusChanged {
			parentStatusUpdateNeeded = true

			r.Recorder.Event(
				&plc,
				eventWarning,
				fmt.Sprintf(plcFmtStr, plc.GetName()),
				convertPolicyStatusToString(&plc),
			)
		}

		r.checkRelatedAndUpdate(plc, relatedObjects, oldRelated, parentStatusUpdateNeeded)
	}

	// initialize apiresources for template processing before starting objectTemplate processing
	// this is optional but since apiresourcelist is already available,
	// use this rather than re-discovering the list for generic-lookup
	r.lock.RLock()
	tmplResolverCfg := templates.Config{KubeAPIResourceList: r.apiResourceList}
	r.lock.RUnlock()

	usedKeyCache := false

	if usesEncryption(plc) {
		var encryptionConfig templates.EncryptionConfig
		var err error

		encryptionConfig, usedKeyCache, err = r.getEncryptionConfig(plc, false)

		if err != nil {
			addTemplateErrorViolation("", err.Error())

			return
		}

		tmplResolverCfg.EncryptionConfig = encryptionConfig
	}

	annotations := plc.GetAnnotations()
	disableTemplates := false

	if disableAnnotation, ok := annotations["policy.open-cluster-management.io/disable-templates"]; ok {
		log.V(2).Info("Found disable-templates annotation", "value", disableAnnotation)

		parsedDisable, err := strconv.ParseBool(disableAnnotation)
		if err != nil {
			log.Error(err, "Could not parse value for disable-templates annotation", "value", disableAnnotation)
		} else {
			disableTemplates = parsedDisable
		}
	}

	// set up raw data for template processing
	var rawDataList [][]byte
	var isRawObjTemplate bool

	if plc.Spec.ObjectTemplatesRaw != "" {
		rawDataList = [][]byte{[]byte(plc.Spec.ObjectTemplatesRaw)}
		isRawObjTemplate = true
	} else {
		for _, objectT := range plc.Spec.ObjectTemplates {
			rawDataList = append(rawDataList, objectT.ObjectDefinition.Raw)
		}
		isRawObjTemplate = false
	}

	tmplResolverCfg.InputIsYAML = isRawObjTemplate

	tmplResolver, err := templates.NewResolver(&r.TargetK8sClient, r.TargetK8sConfig, tmplResolverCfg)
	if err != nil {
		// If the encryption key is invalid, clear the cache.
		if errors.Is(err, templates.ErrInvalidAESKey) || errors.Is(err, templates.ErrAESKeyNotSet) {
			r.cachedEncryptionKey = &cachedEncryptionKey{}
		}

		msg := getTemplateConfigErrorMsg(err)
		addTemplateErrorViolation("", msg)

		return
	}

	log.V(2).Info("Processing the object templates", "count", len(plc.Spec.ObjectTemplates))

	if !disableTemplates {
		startTime := time.Now().UTC()

		var objTemps []*policyv1.ObjectTemplate

		// process object templates for go template usage
		for i, rawData := range rawDataList {
			// first check to make sure there are no hub-templates with delimiter - {{hub
			// if one exists, it means the template resolution on the hub did not succeed.
			if templates.HasTemplate(rawData, "{{hub", false) {
				// check to see there is an annotation set to the hub error msg,
				// if not ,set a generic msg
				hubTemplatesErrMsg, ok := annotations["policy.open-cluster-management.io/hub-templates-error"]
				if !ok || hubTemplatesErrMsg == "" {
					// set a generic msg
					hubTemplatesErrMsg = "Error occurred while processing hub-templates, " +
						"check the policy events for more details."
				}

				log.Info(
					"An error occurred while processing hub-templates on the Hub cluster. Cannot process the policy.",
					"message", hubTemplatesErrMsg,
				)

				addTemplateErrorViolation("Error processing hub templates", hubTemplatesErrMsg)

				return
			}

			if templates.HasTemplate(rawData, "", true) {
				log.V(1).Info("Processing policy templates")

				resolvedTemplate, tplErr := tmplResolver.ResolveTemplate(rawData, nil)

				if errors.Is(tplErr, templates.ErrMissingAPIResource) ||
					errors.Is(tplErr, templates.ErrMissingAPIResourceInvalidTemplate) {
					log.V(2).Info(
						"A template encountered an API resource which was not in the API resource list. Refreshing " +
							"it and trying again.",
					)

					discoveryErr := r.refreshDiscoveryInfo()
					if discoveryErr == nil {
						tmplResolver.SetKubeAPIResourceList(r.apiResourceList)
						resolvedTemplate, tplErr = tmplResolver.ResolveTemplate(rawData, nil)
					} else {
						log.V(2).Info(
							"Failed to refresh the API discovery information after a template encountered an unknown " +
								"API resource type. Continuing with the assumption that the discovery information " +
								"was correct.",
						)
					}
				}

				// If the error is because the padding is invalid, this either means the encrypted value was not
				// generated by the "protect" template function or the AES key is incorrect. Control for a stale
				// cached key.
				if usedKeyCache && errors.Is(tplErr, templates.ErrInvalidPKCS7Padding) {
					log.V(2).Info(
						"The template decryption failed likely due to an invalid encryption key, will refresh " +
							"the encryption key cache and try the decryption again",
					)
					var encryptionConfig templates.EncryptionConfig
					encryptionConfig, usedKeyCache, err = r.getEncryptionConfig(plc, true)

					if err != nil {
						addTemplateErrorViolation("", err.Error())

						return
					}

					tmplResolverCfg.EncryptionConfig = encryptionConfig

					err := tmplResolver.SetEncryptionConfig(encryptionConfig)
					if err != nil {
						// If the encryption key is invalid, clear the cache.
						if errors.Is(err, templates.ErrInvalidAESKey) || errors.Is(err, templates.ErrAESKeyNotSet) {
							r.cachedEncryptionKey = &cachedEncryptionKey{}
						}

						msg := getTemplateConfigErrorMsg(err)
						addTemplateErrorViolation("", msg)

						return
					}

					resolvedTemplate, tplErr = tmplResolver.ResolveTemplate(rawData, nil)
				}

				if tplErr != nil {
					addTemplateErrorViolation("", tplErr.Error())

					return
				}

				// If raw data, only one passthrough is needed, since all the object templates are in it
				if isRawObjTemplate {
					err := json.Unmarshal(resolvedTemplate.ResolvedJSON, &objTemps)
					if err != nil {
						addTemplateErrorViolation("Error unmarshalling raw template", err.Error())

						return
					}

					plc.Spec.ObjectTemplates = objTemps

					break
				}

				// Otherwise, set the resolved data for use in further processing
				plc.Spec.ObjectTemplates[i].ObjectDefinition.Raw = resolvedTemplate.ResolvedJSON
			} else if isRawObjTemplate {
				// Unmarshal raw template YAML into object if that has not already been done by the template
				// resolution function
				err = yaml.Unmarshal(rawData, &objTemps)
				if err != nil {
					addTemplateErrorViolation("Error parsing the YAML in the object-templates-raw field", err.Error())

					return
				}

				plc.Spec.ObjectTemplates = objTemps

				break
			}
		}

		if r.EnableMetrics {
			durationSeconds := time.Since(startTime).Seconds()
			plcTempsProcessSecondsCounter.WithLabelValues(plc.GetName()).Add(durationSeconds)
			plcTempsProcessCounter.WithLabelValues(plc.GetName()).Inc()
		}
	}

	// Parse and fetch details from each object in each objectTemplate, and gather namespaces if required
	var templateObjs []objectTemplateDetails
	var selectedNamespaces []string
	var objTmplStatusChangeNeeded bool

	templateObjs, selectedNamespaces, objTmplStatusChangeNeeded, err = r.getObjectTemplateDetails(plc)

	if objTmplStatusChangeNeeded {
		parentStatusUpdateNeeded = true
	}

	if err != nil {
		if parentStatusUpdateNeeded {
			r.checkRelatedAndUpdate(plc, relatedObjects, oldRelated, parentStatusUpdateNeeded)
		}

		return
	}

	for indx, objectT := range plc.Spec.ObjectTemplates {
		nonCompliantObjects := map[string]map[string]interface{}{}
		compliantObjects := map[string]map[string]interface{}{}
		enforce := strings.EqualFold(string(plc.Spec.RemediationAction), string(policyv1.Enforce))
		kind := ""
		objShouldExist := !strings.EqualFold(string(objectT.ComplianceType), string(policyv1.MustNotHave))

		// If the object does not have a namespace specified, use the previously retrieved namespaces
		// from the NamespaceSelector. If no namespaces are found/specified, use the value from the
		// object so that the objectTemplate is processed:
		// - For clusterwide resources, an empty string will be expected
		// - For namespaced resources, handleObjects() will update status with a no namespace message if
		//   it's an empty string or else will use the namespace defined in the object
		var relevantNamespaces []string
		if templateObjs[indx].isNamespaced && templateObjs[indx].namespace == "" && len(selectedNamespaces) != 0 {
			relevantNamespaces = selectedNamespaces
		} else {
			relevantNamespaces = []string{templateObjs[indx].namespace}
		}

		numCompliant := 0
		numNonCompliant := 0
		handled := false

		// iterate through all namespaces the configurationpolicy is set on
		for _, ns := range relevantNamespaces {
			log.Info(
				"Handling the object template for the relevant namespace",
				"namespace", ns,
				"desiredName", templateObjs[indx].name,
				"index", indx,
			)

			names, compliant, reason, objKind, related, statusUpdateNeeded := r.handleObjects(
				objectT, ns, templateObjs[indx], indx, &plc,
			)

			if statusUpdateNeeded {
				parentStatusUpdateNeeded = true
			}

			if objKind != "" {
				kind = objKind
			}

			if names == nil {
				// object template enforced, already handled in handleObjects
				handled = true
			} else {
				enforce = false
				if !compliant {
					if len(names) == 0 {
						numNonCompliant++
					} else {
						numNonCompliant += len(names)
					}
					nonCompliantObjects[ns] = map[string]interface{}{
						"names":  names,
						"reason": reason,
					}
				} else {
					numCompliant += len(names)
					compliantObjects[ns] = map[string]interface{}{
						"names":  names,
						"reason": reason,
					}
				}
			}

			for _, object := range related {
				relatedObjects = updateRelatedObjectsStatus(relatedObjects, object)
			}
		}
		// violations for enforce configurationpolicies are already handled in handleObjects,
		// so we only need to generate a violation if the remediationAction is set to inform
		if !handled && !enforce {
			objData := map[string]interface{}{
				"indx":        indx,
				"kind":        kind,
				"desiredName": templateObjs[indx].name,
				"namespaced":  templateObjs[indx].isNamespaced,
			}

			statusUpdateNeeded := createInformStatus(
				objShouldExist, numCompliant, numNonCompliant, compliantObjects, nonCompliantObjects, &plc, objData,
			)
			if statusUpdateNeeded {
				parentStatusUpdateNeeded = true
			}
		}
	}

	r.checkRelatedAndUpdate(plc, relatedObjects, oldRelated, parentStatusUpdateNeeded)
}

// checkRelatedAndUpdate checks the related objects field and triggers an update on the ConfigurationPolicy
func (r *ConfigurationPolicyReconciler) checkRelatedAndUpdate(
	plc policyv1.ConfigurationPolicy, related, oldRelated []policyv1.RelatedObject, sendEvent bool,
) {
	sortRelatedObjectsAndUpdate(&plc, related, oldRelated, r.EnableMetrics)
	// An update always occurs to account for the lastEvaluated status field
	r.addForUpdate(&plc, sendEvent)
}

// helper function to check whether related objects has changed
func sortRelatedObjectsAndUpdate(
	plc *policyv1.ConfigurationPolicy, related, oldRelated []policyv1.RelatedObject, collectMetrics bool,
) {
	sort.SliceStable(related, func(i, j int) bool {
		if related[i].Object.Kind != related[j].Object.Kind {
			return related[i].Object.Kind < related[j].Object.Kind
		}
		if related[i].Object.Metadata.Namespace != related[j].Object.Metadata.Namespace {
			return related[i].Object.Metadata.Namespace < related[j].Object.Metadata.Namespace
		}

		return related[i].Object.Metadata.Name < related[j].Object.Metadata.Name
	})

	update := false

	// Instantiate found objects for the related object metric
	found := map[string]bool{}

	if collectMetrics {
		for _, obj := range oldRelated {
			found[getObjectString(obj)] = false
		}
	}

	// Format policy for related object metric
	policyVal := fmt.Sprintf("%s/%s", plc.Namespace, plc.Name)

	for i, newEntry := range related {
		var objKey string
		// Collect the policy and related object for related object metric
		if collectMetrics {
			objKey = getObjectString(newEntry)
			policiesArray := []string{}

			if objValue, ok := policyRelatedObjectMap.Load(objKey); ok {
				policiesArray = append(policiesArray, objValue.([]string)...)
			}

			policiesArray = append(policiesArray, policyVal)
			policyRelatedObjectMap.Store(objKey, policiesArray)
		}

		for _, oldEntry := range oldRelated {
			// Get matching objects
			if gocmp.Equal(newEntry.Object, oldEntry.Object) {
				if oldEntry.Properties != nil &&
					newEntry.Properties != nil &&
					newEntry.Properties.CreatedByPolicy != nil &&
					!(*newEntry.Properties.CreatedByPolicy) {
					// Use the old properties if they existed and this is not a newly created resource
					related[i].Properties = oldEntry.Properties

					if collectMetrics {
						found[objKey] = true
					}

					break
				}
			}
		}
	}

	// Clean up old related object metrics if the related object list changed
	if collectMetrics {
		for _, obj := range oldRelated {
			objString := getObjectString(obj)
			if !found[objString] {
				_ = policyRelatedObjectGauge.DeleteLabelValues(objString, policyVal)
			}
		}
	}

	if len(oldRelated) == len(related) {
		for i, entry := range oldRelated {
			if !gocmp.Equal(entry, related[i]) {
				update = true
			}
		}
	} else {
		update = true
	}

	if update {
		plc.Status.RelatedObjects = related
	}
}

// helper function that appends a condition (violation or compliant) to the status of a configurationpolicy
func addConditionToStatus(
	plc *policyv1.ConfigurationPolicy, index int, compliant bool, reason string, message string,
) (updateNeeded bool) {
	cond := &policyv1.Condition{
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	var complianceState policyv1.ComplianceState

	if reason == reasonCleanupError {
		complianceState = policyv1.Terminating
		cond.Type = "violation"
	} else if compliant {
		complianceState = policyv1.Compliant
		cond.Type = "notification"
	} else {
		complianceState = policyv1.NonCompliant
		cond.Type = "violation"
	}

	log := log.WithValues("policy", plc.GetName(), "complianceState", complianceState)

	if compliant && plc.Spec != nil && plc.Spec.EvaluationInterval.Compliant == "never" {
		msg := `This policy will not be evaluated again due to spec.evaluationInterval.compliant being set to "never"`
		log.Info(msg)
		cond.Message += fmt.Sprintf(". %s.", msg)
	} else if !compliant && plc.Spec != nil && plc.Spec.EvaluationInterval.NonCompliant == "never" {
		msg := "This policy will not be evaluated again due to spec.evaluationInterval.noncompliant " +
			`being set to "never"`
		log.Info(msg)
		cond.Message += fmt.Sprintf(". %s.", msg)
	}

	if len(plc.Status.CompliancyDetails) <= index {
		plc.Status.CompliancyDetails = append(plc.Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: complianceState,
			Conditions:      []policyv1.Condition{},
		})
	}

	if plc.Status.CompliancyDetails[index].ComplianceState != complianceState {
		updateNeeded = true
	}

	plc.Status.CompliancyDetails[index].ComplianceState = complianceState

	// do not add condition unless it does not already appear in the status
	if !checkMessageSimilarity(plc.Status.CompliancyDetails[index].Conditions, cond) {
		conditions := AppendCondition(plc.Status.CompliancyDetails[index].Conditions, cond, "", false)
		plc.Status.CompliancyDetails[index].Conditions = conditions
		updateNeeded = true
	}

	if updateNeeded {
		log.Info("Will update the policy status")
	}

	return updateNeeded
}

// createInformStatus updates the status field for a configurationpolicy with remediationAction=inform
// based on how many compliant/noncompliant objects are found when processing the templates in the configurationpolicy
func createInformStatus(
	objShouldExist bool,
	numCompliant,
	numNonCompliant int,
	compliantObjects,
	nonCompliantObjects map[string]map[string]interface{},
	plc *policyv1.ConfigurationPolicy,
	objData map[string]interface{},
) bool {
	//nolint:forcetypeassert
	desiredName := objData["desiredName"].(string)
	//nolint:forcetypeassert
	indx := objData["indx"].(int)
	//nolint:forcetypeassert
	kind := objData["kind"].(string)
	//nolint:forcetypeassert
	namespaced := objData["namespaced"].(bool)

	if kind == "" {
		return false
	}

	var compObjs map[string]map[string]interface{}
	var compliant bool

	if numNonCompliant > 0 {
		compliant = false
		compObjs = nonCompliantObjects
	} else if objShouldExist && numCompliant == 0 {
		// Special case: No resources found is NonCompliant
		compliant = false
		compObjs = nonCompliantObjects
	} else {
		compliant = true
		compObjs = compliantObjects
	}

	return createStatus(desiredName, kind, compObjs, namespaced, plc, indx, compliant, objShouldExist)
}

// handleObjects controls the processing of each individual object template within a configurationpolicy
func (r *ConfigurationPolicyReconciler) handleObjects(
	objectT *policyv1.ObjectTemplate,
	namespace string,
	objDetails objectTemplateDetails,
	index int,
	policy *policyv1.ConfigurationPolicy,
) (
	objNameList []string,
	compliant bool,
	reason string,
	rsrcKind string,
	relatedObjects []policyv1.RelatedObject,
	statusUpdateNeeded bool,
) {
	log := log.WithValues("policy", policy.GetName(), "index", index, "objectNamespace", namespace)

	if namespace != "" {
		log.V(2).Info("Handling object template")
	} else {
		log.V(2).Info("Handling object template, no namespace specified")
	}

	ext := objectT.ObjectDefinition

	// map raw object to a resource, generate a violation if resource cannot be found
	mapping, statusUpdateNeeded := r.getMapping(ext, policy, index)
	if mapping == nil {
		return nil, false, "", "", nil, statusUpdateNeeded
	}

	unstruct, err := unmarshalFromJSON(ext.Raw)
	if err != nil {
		os.Exit(1)
	}

	exists := true
	objNames := []string{}
	remediation := policy.Spec.RemediationAction

	// If the parsed namespace doesn't match the object namespace, something in the calling function went wrong
	if objDetails.namespace != "" && objDetails.namespace != namespace {
		panic(fmt.Sprintf("Error: provided namespace '%s' does not match object namespace '%s'",
			namespace, objDetails.namespace))
	}

	if objDetails.isNamespaced && namespace == "" {
		objName := objDetails.name
		kindWithoutNS := objDetails.kind
		log.Info(
			"The object template is namespaced but no namespace is specified. Cannot process.",
			"name", objName,
			"kind", kindWithoutNS,
		)
		// namespaced but none specified, generate violation
		msg := fmt.Sprintf("namespaced object %s of kind %s has no namespace specified "+
			"from the policy namespaceSelector nor the object metadata",
			objName, kindWithoutNS,
		)
		statusUpdateNeeded = addConditionToStatus(policy, index, false, "K8s missing namespace", msg)

		if statusUpdateNeeded {
			eventType := eventNormal
			if index < len(policy.Status.CompliancyDetails) &&
				policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
				eventType = eventWarning
			}

			r.Recorder.Event(policy, eventType, fmt.Sprintf(eventFmtStr, policy.GetName(), objDetails.name),
				convertPolicyStatusToString(policy))
		}

		return nil, false, "", "", nil, statusUpdateNeeded
	}

	var object *unstructured.Unstructured

	if objDetails.name != "" { // named object, so checking just for the existence of the specific object
		// If the object couldn't be retrieved, this will be handled later on.
		object, _ = getObject(
			objDetails.isNamespaced, namespace, objDetails.name, mapping.Resource, r.TargetK8sDynamicClient,
		)

		exists = object != nil

		objNames = append(objNames, objDetails.name)
	} else if objDetails.kind != "" { // no name, so we are checking for the existence of any object of this kind
		log.V(1).Info(
			"The object template does not specify a name. Will search for matching objects in the namespace.",
		)
		objNames = append(
			objNames,
			getNamesOfKind(
				unstruct,
				mapping.Resource,
				objDetails.isNamespaced,
				namespace,
				r.TargetK8sDynamicClient,
				strings.ToLower(string(objectT.ComplianceType)),
			)...,
		)

		// we do not support enforce on unnamed templates
		if !strings.EqualFold(string(remediation), "inform") {
			log.Info(
				"The object template does not specify a name. Setting the remediation action to inform.",
				"oldRemediationAction", remediation,
			)
		}
		remediation = "inform"

		if len(objNames) == 0 {
			exists = false
		}
	}

	objShouldExist := !strings.EqualFold(string(objectT.ComplianceType), string(policyv1.MustNotHave))
	rsrcKind = mapping.Resource.Resource

	if len(objNames) == 1 {
		name := objNames[0]
		singObj := singleObject{
			policy:      policy,
			gvr:         mapping.Resource,
			object:      object,
			name:        name,
			namespace:   namespace,
			namespaced:  objDetails.isNamespaced,
			shouldExist: objShouldExist,
			index:       index,
			unstruct:    unstruct,
		}

		log.V(2).Info("Handling a single object template")

		var creationInfo *policyv1.ObjectProperties

		objNames, compliant, rsrcKind, statusUpdateNeeded, creationInfo = r.handleSingleObj(
			singObj, remediation, exists, objectT,
		)
		// The message string for single objects is different than for multiple
		reason = generateSingleObjReason(objShouldExist, compliant, exists)
		// Enforce could clear the objNames array so use name instead
		relatedObjects = addRelatedObjects(
			compliant,
			mapping.Resource,
			objDetails.kind,
			namespace,
			objDetails.isNamespaced,
			[]string{name},
			reason,
			creationInfo,
		)
	} else { // This case only occurs when the desired object is not named
		if objShouldExist {
			if exists {
				compliant = true
				reason = reasonWantFoundExists
			} else {
				compliant = false
				reason = reasonWantFoundDNE
			}
		} else {
			if exists {
				compliant = false
				reason = reasonWantNotFoundExists
			} else {
				compliant = true
				reason = reasonWantNotFoundDNE
			}
		}

		relatedObjects = addRelatedObjects(
			compliant,
			mapping.Resource,
			objDetails.kind,
			namespace,
			objDetails.isNamespaced,
			objNames,
			reason,
			nil,
		)

		if !statusUpdateNeeded {
			log.V(2).Info("The status did not change for this object template")
		}
	}

	return objNames, compliant, reason, rsrcKind, relatedObjects, statusUpdateNeeded
}

// generateSingleObjReason is a helper function to create a compliant/noncompliant message for a named object
func generateSingleObjReason(objShouldExist bool, compliant bool, exists bool) (rsn string) {
	reason := ""

	if objShouldExist && compliant {
		reason = reasonWantFoundExists
	} else if objShouldExist && !compliant && exists {
		reason = reasonWantFoundNoMatch
	} else if objShouldExist && !compliant {
		reason = reasonWantFoundDNE
	} else if !objShouldExist && compliant {
		reason = reasonWantNotFoundDNE
	} else if !objShouldExist && !compliant {
		reason = reasonWantNotFoundExists
	}

	return reason
}

type singleObject struct {
	policy      *policyv1.ConfigurationPolicy
	gvr         schema.GroupVersionResource
	object      *unstructured.Unstructured
	name        string
	namespace   string
	namespaced  bool
	shouldExist bool
	index       int
	unstruct    unstructured.Unstructured
}

// handleSingleObj takes in an object template (for a named object) and its data and determines whether
// the object on the cluster is compliant or not
func (r *ConfigurationPolicyReconciler) handleSingleObj(
	obj singleObject,
	remediation policyv1.RemediationAction,
	exists bool,
	objectT *policyv1.ObjectTemplate,
) (
	objNameList []string,
	compliance bool,
	rsrcKind string,
	statusUpdateNeeded bool,
	creationInfo *policyv1.ObjectProperties,
) {
	objLog := log.WithValues("object", obj.name, "policy", obj.policy.Name, "index", obj.index)

	var err error
	var compliant bool

	compliantObject := map[string]map[string]interface{}{
		obj.namespace: {
			"names": []string{obj.name},
		},
	}

	if !exists && obj.shouldExist {
		// it is a musthave and it does not exist, so it must be created
		if strings.EqualFold(string(remediation), string(policyv1.Enforce)) {
			// object is missing, so send noncompliant event
			_ = createStatus("", obj.gvr.Resource, compliantObject, obj.namespaced, obj.policy,
				obj.index, false, true)
			obj.policy.Status.ComplianceState = policyv1.NonCompliant
			statusStr := convertPolicyStatusToString(obj.policy)
			objLog.Info("Sending an update policy status event", "policy", obj.policy.Name, "status", statusStr)
			r.Recorder.Event(obj.policy, eventWarning, fmt.Sprintf(eventFmtStr, obj.policy.GetName(), obj.name),
				statusStr)
			// update parent policy status
			r.addForUpdate(obj.policy, true)

			var uid string
			statusUpdateNeeded, uid, err = r.enforceByCreatingOrDeleting(obj)

			if err != nil {
				// violation created for handling error
				objLog.Error(err, "Could not handle missing musthave object")
			} else {
				created := true
				creationInfo = &policyv1.ObjectProperties{
					CreatedByPolicy: &created,
					UID:             uid,
				}
				compliant = true
			}
		} else { // inform
			compliant = false
		}
	}

	if exists && !obj.shouldExist {
		// it is a mustnothave but it exist, so it must be deleted
		if strings.EqualFold(string(remediation), string(policyv1.Enforce)) {
			statusUpdateNeeded, _, err = r.enforceByCreatingOrDeleting(obj)
			if err != nil {
				objLog.Error(err, "Could not handle existing mustnothave object")
			}
		} else { // inform
			compliant = false
		}
	}

	if !exists && !obj.shouldExist {
		log.V(1).Info("The object does not exist and is compliant with the mustnothave compliance type")
		// it is a must not have and it does not exist, so it is compliant
		compliant = true

		if strings.EqualFold(string(remediation), string(policyv1.Enforce)) {
			log.V(2).Info("Entering `does not exist` and `must not have`")

			statusUpdateNeeded = createStatus("", obj.gvr.Resource, compliantObject, obj.namespaced, obj.policy,
				obj.index, compliant, false)
		}
	}

	processingErr := false
	specViolation := false

	// object exists and the template requires it, so we need to check specific fields to see if we have a match
	if exists {
		log.V(2).Info("The object already exists. Verifying the object fields match what is desired.")

		compType := strings.ToLower(string(objectT.ComplianceType))
		mdCompType := strings.ToLower(string(objectT.MetadataComplianceType))

		before := time.Now().UTC()

		throwSpecViolation, msg, pErr, triedUpdate, updatedObj := r.checkAndUpdateResource(obj, compType, mdCompType,
			remediation)

		if triedUpdate {
			// object has a mismatch and needs an update to be enforced, throw violation for mismatch
			_ = createStatus("", obj.gvr.Resource, compliantObject, obj.namespaced, obj.policy, obj.index,
				false, true)
			obj.policy.Status.ComplianceState = policyv1.NonCompliant
			statusStr := convertPolicyStatusToString(obj.policy)
			objLog.Info("Sending an update policy status event", "policy", obj.policy.Name, "status", statusStr)
			r.Recorder.Event(obj.policy, eventWarning, fmt.Sprintf(eventFmtStr, obj.policy.GetName(), obj.name),
				statusStr)
			// update parent policy status
			r.addForUpdate(obj.policy, true)
		}

		duration := time.Now().UTC().Sub(before)
		seconds := float64(duration) / float64(time.Second)
		compareObjSecondsCounter.WithLabelValues(
			obj.policy.Name,
			obj.namespace,
			fmt.Sprintf("%s.%s", obj.gvr.Resource, obj.name),
		).Add(seconds)
		compareObjEvalCounter.WithLabelValues(
			obj.policy.Name,
			obj.namespace,
			fmt.Sprintf("%s.%s", obj.gvr.Resource, obj.name),
		).Inc()

		if throwSpecViolation {
			specViolation = throwSpecViolation
			compliant = false
		} else if msg != "" {
			statusUpdateNeeded = addConditionToStatus(obj.policy, obj.index, false, "K8s update template error", msg)
		} else if obj.shouldExist {
			// it is a must have and it does exist, so it is compliant
			compliant = true
			if strings.EqualFold(string(remediation), string(policyv1.Enforce)) {
				if updatedObj {
					// object updated in checkAndUpdateResource, send event
					reason := "K8s update success"
					idStr := identifierStr([]string{obj.name}, obj.namespace, obj.namespaced)
					msg := fmt.Sprintf("%v %v was updated successfully", obj.gvr.Resource, idStr)

					statusUpdateNeeded = addConditionToStatus(obj.policy, obj.index, true, reason, msg)
				} else {
					statusUpdateNeeded = createStatus("", obj.gvr.Resource, compliantObject, obj.namespaced, obj.policy,
						obj.index, compliant, true)
				}
				created := false
				creationInfo = &policyv1.ObjectProperties{
					CreatedByPolicy: &created,
					UID:             "",
				}
			}
		}

		processingErr = pErr
	}

	if statusUpdateNeeded {
		eventType := eventNormal
		if obj.index < len(obj.policy.Status.CompliancyDetails) &&
			obj.policy.Status.CompliancyDetails[obj.index].ComplianceState == policyv1.NonCompliant {
			eventType = eventWarning
			compliant = false
		}

		if compliant {
			obj.policy.Status.ComplianceState = policyv1.Compliant
		} else {
			obj.policy.Status.ComplianceState = policyv1.NonCompliant
		}

		statusStr := convertPolicyStatusToString(obj.policy)

		log.V(1).Info("Sending an update policy status event", "policy", obj.policy.Name, "status", statusStr)
		r.Recorder.Event(obj.policy, eventType, fmt.Sprintf(eventFmtStr, obj.policy.GetName(), obj.name), statusStr)

		return nil, compliant, "", statusUpdateNeeded, creationInfo
	}

	if processingErr {
		return nil, false, "", statusUpdateNeeded, creationInfo
	}

	if strings.EqualFold(string(remediation), string(policyv1.Inform)) || specViolation {
		return []string{obj.name}, compliant, obj.gvr.Resource, statusUpdateNeeded, creationInfo
	}

	return nil, compliant, "", statusUpdateNeeded, creationInfo
}

// isObjectNamespaced determines if the input object is a namespaced resource. When refreshIfNecessary
// is true, the discovery information will be refreshed if the resource cannot be found.
func (r *ConfigurationPolicyReconciler) isObjectNamespaced(
	object *unstructured.Unstructured, refreshIfNecessary bool,
) bool {
	gvk := object.GetObjectKind().GroupVersionKind()
	gv := gvk.GroupVersion().String()

	r.lock.RLock()

	for _, apiResourceGroup := range r.apiResourceList {
		if apiResourceGroup.GroupVersion == gv {
			for _, apiResource := range apiResourceGroup.APIResources {
				if apiResource.Kind == gvk.Kind {
					namespaced := apiResource.Namespaced
					log.V(2).Info("Found resource in apiResourceList", "namespaced", namespaced, "gvk", gvk.String())
					r.lock.RUnlock()

					return namespaced
				}
			}

			// Break early in the event all the API resources in the matching group version have been exhausted
			break
		}
	}

	r.lock.RUnlock()

	// The API resource wasn't found. Try refreshing the cache and trying again.
	if refreshIfNecessary {
		log.V(2).Info("Did not find the resource in apiResourceList. Will refresh and try again.", "gvk", gvk.String())

		err := r.refreshDiscoveryInfo()
		if err != nil {
			return false
		}

		return r.isObjectNamespaced(object, false)
	}

	return false
}

// getMapping takes in a raw object, decodes it, and maps it to an existing group/kind
func (r *ConfigurationPolicyReconciler) getMapping(
	ext runtime.RawExtension,
	policy *policyv1.ConfigurationPolicy,
	index int,
) (mapping *meta.RESTMapping, updateNeeded bool) {
	log := log.WithValues("policy", policy.GetName(), "index", index)

	_, gvk, err := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, nil)
	if err != nil {
		// generate violation if object cannot be decoded and update the configpolicy
		decodeErr := fmt.Sprintf("Decoding error, please check your policy file!"+
			" Aborting handling the object template at index [%v] in policy `%v` with error = `%v`",
			index, policy.Name, err)

		log.Error(err, "Could not decode object")

		if len(policy.Status.CompliancyDetails) <= index {
			policy.Status.CompliancyDetails = append(policy.Status.CompliancyDetails, policyv1.TemplateStatus{
				ComplianceState: policyv1.NonCompliant,
				Conditions:      []policyv1.Condition{},
			})
		}

		policy.Status.CompliancyDetails[index].ComplianceState = policyv1.NonCompliant
		policy.Status.CompliancyDetails[index].Conditions = []policyv1.Condition{
			{
				Type:               "violation",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s decode object definition error",
				Message:            decodeErr,
			},
		}

		return nil, true
	}

	// initializes a mapping between Kind and APIVersion to a resource name
	r.lock.RLock()
	mapper := restmapper.NewDiscoveryRESTMapper(r.apiGroups)
	r.lock.RUnlock()

	mapping, err = mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	mappingErrMsg := ""

	if err != nil {
		// if the restmapper fails to find a mapping to a resource, generate a violation and update the configpolicy
		prefix := "no matches for kind \""
		startIdx := strings.Index(err.Error(), prefix)

		if startIdx == -1 {
			log.Error(err, "Could not identify mapping error from raw object", "gvk", gvk)
		} else {
			afterPrefix := err.Error()[(startIdx + len(prefix)):len(err.Error())]
			kind := afterPrefix[0:(strings.Index(afterPrefix, "\" "))]
			mappingErrMsg = "couldn't find mapping resource with kind " + kind +
				", please check if you have CRD deployed"

			log.Error(err, "Could not map resource, do you have the CRD deployed?", "kind", kind)

			parent := ""
			if len(policy.OwnerReferences) > 0 {
				parent = policy.OwnerReferences[0].Name
			}

			policyUserErrorsCounter.WithLabelValues(parent, policy.GetName(), "no-object-CRD").Add(1)
		}

		errMsg := err.Error()
		if mappingErrMsg != "" {
			errMsg = mappingErrMsg
			cond := &policyv1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation error",
				Message:            mappingErrMsg,
			}

			if len(policy.Status.CompliancyDetails) <= index {
				policy.Status.CompliancyDetails = append(policy.Status.CompliancyDetails, policyv1.TemplateStatus{
					ComplianceState: policyv1.NonCompliant,
					Conditions:      []policyv1.Condition{},
				})
			}

			if policy.Status.CompliancyDetails[index].ComplianceState != policyv1.NonCompliant {
				updateNeeded = true
			}

			policy.Status.CompliancyDetails[index].ComplianceState = policyv1.NonCompliant

			if !checkMessageSimilarity(policy.Status.CompliancyDetails[index].Conditions, cond) {
				conditions := AppendCondition(policy.Status.CompliancyDetails[index].Conditions,
					cond, gvk.GroupKind().Kind, false)
				policy.Status.CompliancyDetails[index].Conditions = conditions
				updateNeeded = true
			}
		}

		if updateNeeded {
			// generate an event on the configurationpolicy if a violation is created
			r.Recorder.Event(policy, eventWarning, fmt.Sprintf(plcFmtStr, policy.GetName()), errMsg)
		}

		return nil, updateNeeded
	}

	log.V(2).Info(
		"Found the API mapping for the object template",
		"group", gvk.Group,
		"version", gvk.Version,
		"kind", gvk.Kind,
	)

	return mapping, updateNeeded
}

// buildNameList is a helper function to pull names of resources that match an objectTemplate from a list of resources
func buildNameList(
	unstruct unstructured.Unstructured, complianceType string, resList *unstructured.UnstructuredList,
) (kindNameList []string) {
	for i := range resList.Items {
		uObj := resList.Items[i]
		match := true

		for key := range unstruct.Object {
			// if any key in the object generates a mismatch, the object does not match the template and we
			// do not add its name to the list
			errorMsg, updateNeeded, _, skipped := handleSingleKey(key, unstruct, &uObj, complianceType)
			if !skipped {
				if errorMsg != "" || updateNeeded {
					match = false
				}
			}
		}

		if match {
			kindNameList = append(kindNameList, uObj.Object["metadata"].(map[string]interface{})["name"].(string))
		}
	}

	return kindNameList
}

// getNamesOfKind returns an array with names of all of the resources found
// matching the GVK specified.
func getNamesOfKind(
	unstruct unstructured.Unstructured,
	rsrc schema.GroupVersionResource,
	namespaced bool,
	ns string,
	dclient dynamic.Interface,
	complianceType string,
) (kindNameList []string) {
	if namespaced {
		res := dclient.Resource(rsrc).Namespace(ns)

		resList, err := res.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			log.Error(err, "Could not list resources", "rsrc", rsrc, "namespaced", namespaced)

			return kindNameList
		}

		return buildNameList(unstruct, complianceType, resList)
	}

	res := dclient.Resource(rsrc)

	resList, err := res.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Error(err, "Could not list resources", "rsrc", rsrc, "namespaced", namespaced)

		return kindNameList
	}

	return buildNameList(unstruct, complianceType, resList)
}

// enforceByCreatingOrDeleting can handle the situation where a musthave or mustonlyhave object is
// completely missing (as opposed to existing, but not matching the desired state), or where a
// mustnothave object does exist. Eg, it does not handle the case where a targeted update would need
// to be made to an object.
func (r *ConfigurationPolicyReconciler) enforceByCreatingOrDeleting(obj singleObject) (
	result bool, uid string, erro error,
) {
	log := log.WithValues(
		"object", obj.name,
		"policy", obj.policy.Name,
		"objectNamespace", obj.namespace,
		"objectTemplateIndex", obj.index,
	)
	idStr := identifierStr([]string{obj.name}, obj.namespace, obj.namespaced)

	var res dynamic.ResourceInterface
	if obj.namespaced {
		res = r.TargetK8sDynamicClient.Resource(obj.gvr).Namespace(obj.namespace)
	} else {
		res = r.TargetK8sDynamicClient.Resource(obj.gvr)
	}

	var completed bool
	var reason, msg string
	var err error

	if obj.shouldExist {
		log.Info("Enforcing the policy by creating the object")

		if obj.object, err = r.createObject(res, obj.unstruct); obj.object == nil {
			reason = "K8s creation error"
			msg = fmt.Sprintf("%v %v is missing, and cannot be created, reason: `%v`", obj.gvr.Resource, idStr, err)
		} else {
			log.V(2).Info("Created missing must have object", "resource", obj.gvr.Resource, "name", obj.name)
			reason = "K8s creation success"
			msg = fmt.Sprintf("%v %v was missing, and was created successfully", obj.gvr.Resource, idStr)

			var uidIsString bool
			uid, uidIsString, err = unstructured.NestedString(obj.object.Object, "metadata", "uid")

			if !uidIsString || err != nil {
				log.Error(err, "Tried to set UID in status but the field is not a string")
			}

			completed = true
		}
	} else {
		log.Info("Enforcing the policy by deleting the object")

		if completed, err = deleteObject(res, obj.name, obj.namespace); !completed {
			reason = "K8s deletion error"
			msg = fmt.Sprintf("%v %v exists, and cannot be deleted, reason: `%v`", obj.gvr.Resource, idStr, err)
		} else {
			reason = "K8s deletion success"
			msg = fmt.Sprintf("%v %v existed, and was deleted successfully", obj.gvr.Resource, idStr)
			obj.object = nil
		}
	}

	return addConditionToStatus(obj.policy, obj.index, completed, reason, msg), uid, err
}

// checkMessageSimilarity decides whether to append a new condition to a configurationPolicy status
// based on whether it is too similar to the previous one
func checkMessageSimilarity(conditions []policyv1.Condition, cond *policyv1.Condition) bool {
	same := true
	lastIndex := len(conditions)

	if lastIndex > 0 {
		oldCond := conditions[lastIndex-1]
		if !IsSimilarToLastCondition(oldCond, *cond) {
			same = false
		}
	} else {
		same = false
	}

	return same
}

// getObject gets the object with the dynamic client and returns the object if found.
func getObject(
	namespaced bool,
	namespace string,
	name string,
	rsrc schema.GroupVersionResource,
	dclient dynamic.Interface,
) (object *unstructured.Unstructured, err error) {
	objLog := log.WithValues("name", name, "namespaced", namespaced, "namespace", namespace)
	objLog.V(2).Info("Checking if the object exists")

	var res dynamic.ResourceInterface
	if namespaced {
		res = dclient.Resource(rsrc).Namespace(namespace)
	} else {
		res = dclient.Resource(rsrc)
	}

	object, err = res.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			objLog.V(2).Info("Got 'Not Found' response for object from the API server")

			return nil, nil
		}

		objLog.Error(err, "Could not retrieve object from the API server")

		return nil, err
	}

	objLog.V(2).Info("Retrieved object from the API server")

	return object, nil
}

func (r *ConfigurationPolicyReconciler) createObject(
	res dynamic.ResourceInterface, unstruct unstructured.Unstructured,
) (object *unstructured.Unstructured, err error) {
	objLog := log.WithValues("name", unstruct.GetName(), "namespace", unstruct.GetNamespace())
	objLog.V(2).Info("Entered createObject", "unstruct", unstruct)

	if err := r.validateObject(&unstruct); err != nil {
		return nil, err
	}

	object, err = res.Create(context.TODO(), &unstruct, metav1.CreateOptions{})
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

// mergeSpecs is a wrapper for the recursive function to merge 2 maps. It marshals the objects into JSON
// to make sure they are valid objects before calling the merge function
func mergeSpecs(templateVal, existingVal interface{}, ctype string) (interface{}, error) {
	data1, err := json.Marshal(templateVal)
	if err != nil {
		return nil, err
	}

	data2, err := json.Marshal(existingVal)
	if err != nil {
		return nil, err
	}

	var j1, j2 interface{}

	err = json.Unmarshal(data1, &j1)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data2, &j2)
	if err != nil {
		return nil, err
	}

	return mergeSpecsHelper(j1, j2, ctype), nil
}

// mergeSpecsHelper is a helper function that takes an object from the existing object and merges in
// all the data that is different in the template. This way, comparing the merged object to the one
// that exists on the cluster will tell you whether the existing object is compliant with the template.
// This function uses recursion to check mismatches in nested objects and is the basis for most
// comparisons the controller makes.
func mergeSpecsHelper(templateVal, existingVal interface{}, ctype string) interface{} {
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
				templateVal[k] = mergeSpecsHelper(v1, v2, ctype)
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
			return mergeArrays(templateVal, existingVal, ctype)
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

// mergeArrays is a helper function that takes a list from the existing object and merges in all the data that is
// different in the template. This way, comparing the merged object to the one that exists on the cluster will tell
// you whether the existing object is compliant with the template
func mergeArrays(newArr []interface{}, old []interface{}, ctype string) (result []interface{}) {
	if ctype == "mustonlyhave" {
		return newArr
	}

	newArrCopy := append([]interface{}{}, newArr...)
	idxWritten := map[int]bool{}

	for i := range newArrCopy {
		idxWritten[i] = false
	}

	// create a set with a key for each unique item in the list
	oldItemSet := map[string]map[string]interface{}{}

	for _, val2 := range old {
		key := fmt.Sprint(val2)

		if entry, ok := oldItemSet[key]; ok {
			oldItemSet[key]["count"] = entry["count"].(int) + 1
		} else {
			oldItemSet[key] = map[string]interface{}{
				"count": 1,
				"value": val2,
			}
		}
	}

	seen := map[string]bool{}

	// Iterate both arrays in order to favor the case when the object is already compliant.
	for _, val2 := range old {
		key := fmt.Sprint(val2)
		if seen[key] {
			continue
		} else {
			seen[key] = true
		}

		data := oldItemSet[key]
		count := 0
		reqCount := data["count"]
		val2 := data["value"]
		// for each list item in the existing array, iterate through the template array and try to find a match
		for newArrIdx, val1 := range newArrCopy {
			if idxWritten[newArrIdx] {
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
				mergedObj, _ = compareSpecs(val1, val2, ctype)
			default:
				mergedObj = val1
			}
			// if a match is found, this field is already in the template, so we can skip it in future checks
			if sameNamedObjects || equalObjWithSort(mergedObj, val2) {
				count++

				newArr[newArrIdx] = mergedObj
				idxWritten[newArrIdx] = true
			}

			// If the result of merging val1 (template) into val2 (existing value) matched val2 for the required count,
			// move on to the next existing value.
			if count == reqCount {
				break
			}
		}
		// if an item in the existing object cannot be found in the template, we add it to the template array
		// to produce the merged array
		if count < reqCount.(int) {
			for i := 0; i < (reqCount.(int) - count); i++ {
				newArr = append(newArr, val2)
			}
		}
	}

	return newArr
}

// compareLists is a wrapper function that creates a merged list for musthave
// and returns the template list for mustonlyhave
func compareLists(newList []interface{}, oldList []interface{}, ctype string) (updatedList []interface{}, err error) {
	if ctype != "mustonlyhave" {
		return mergeArrays(newList, oldList, ctype), nil
	}

	// mustonlyhave scenario: go through existing list and merge everything in order
	// then add all other template items in order afterward
	mergedList := []interface{}{}

	for idx, item := range newList {
		if idx < len(oldList) {
			newItem, err := mergeSpecs(item, oldList[idx], ctype)
			if err != nil {
				return nil, err
			}

			mergedList = append(mergedList, newItem)
		} else {
			mergedList = append(mergedList, item)
		}
	}

	return mergedList, nil
}

// compareSpecs is a wrapper function that creates a merged map for mustHave
// and returns the template map for mustonlyhave
func compareSpecs(
	newSpec, oldSpec map[string]interface{}, ctype string,
) (updatedSpec map[string]interface{}, err error) {
	if ctype == "mustonlyhave" {
		return newSpec, nil
	}
	// if compliance type is musthave, create merged object to compare on
	merged, err := mergeSpecs(newSpec, oldSpec, ctype)
	if err != nil {
		return merged.(map[string]interface{}), err
	}

	return merged.(map[string]interface{}), nil
}

// handleSingleKey checks whether a key/value pair in an object template matches with that in the existing
// resource on the cluster
func handleSingleKey(
	key string, desiredObj unstructured.Unstructured, existingObj *unstructured.Unstructured, complianceType string,
) (errormsg string, update bool, merged interface{}, skip bool) {
	log := log.WithValues("name", existingObj.GetName(), "namespace", existingObj.GetNamespace())
	var err error

	updateNeeded := false

	if isDenylisted(key) {
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
			mergedValue, err = compareLists(desiredValue, existingValue, complianceType)
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
			mergedValue, err = compareSpecs(desiredValue, existingValue, complianceType)
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

	// sort objects before checking equality to ensure they're in the same order
	if !equalObjWithSort(mergedValue, existingValue) {
		updateNeeded = true
	}

	return "", updateNeeded, mergedValue, false
}

// validateObject performs client-side validation of the input object using the server's OpenAPI definitions that are
// cached. An error is returned if the input object is invalid or the OpenAPI data could not be fetched.
func (r *ConfigurationPolicyReconciler) validateObject(object *unstructured.Unstructured) error {
	// Parse() handles caching of the OpenAPI data.
	r.lock.RLock()
	openAPIResources, err := r.openAPIParser.Parse()
	r.lock.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to retrieve the OpenAPI data from the Kubernetes API: %w", err)
	}

	schema := validation.ConjunctiveSchema{
		openapivalidation.NewSchemaValidation(openAPIResources),
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

// checkAndUpdateResource checks each individual key of a resource and passes it to handleKeys to see if it
// matches the template and update it if the remediationAction is enforce. UpdateNeeded indicates whether the
// function tried to update the child object and updateSucceeded indicates whether the update was applied
// successfully.
func (r *ConfigurationPolicyReconciler) checkAndUpdateResource(
	obj singleObject,
	complianceType string,
	mdComplianceType string,
	remediation policyv1.RemediationAction,
) (throwSpecViolation bool, message string, processingErr bool, updateNeeded bool, updateSucceeded bool) {
	log := log.WithValues(
		"policy", obj.policy.Name, "name", obj.name, "namespace", obj.namespace, "resource", obj.gvr.Resource,
	)

	if obj.object == nil {
		log.Info("Skipping update: Previous object retrieval from the API server failed")

		return false, "", false, false, false
	}

	var res dynamic.ResourceInterface
	if obj.namespaced {
		res = r.TargetK8sDynamicClient.Resource(obj.gvr).Namespace(obj.namespace)
	} else {
		res = r.TargetK8sDynamicClient.Resource(obj.gvr)
	}

	var err error
	var statusUpdated bool

	updateSucceeded = false

	for key := range obj.unstruct.Object {
		isStatus := key == "status"

		// use metadatacompliancetype to evaluate metadata if it is set
		keyComplianceType := complianceType
		if key == "metadata" && mdComplianceType != "" {
			keyComplianceType = mdComplianceType
		}

		// check key for mismatch
		errorMsg, keyUpdateNeeded, mergedObj, skipped := handleSingleKey(
			key, obj.unstruct, obj.object, keyComplianceType,
		)
		if errorMsg != "" {
			log.Info(errorMsg)

			return false, errorMsg, true, false, false
		}

		if mergedObj == nil && skipped {
			continue
		}

		// only look at labels and annotations for metadata - configurationPolicies do not update other metadata fields
		if key == "metadata" {
			mergedAnnotations := mergedObj.(map[string]interface{})["annotations"]
			mergedLabels := mergedObj.(map[string]interface{})["labels"]
			obj.object.UnstructuredContent()["metadata"].(map[string]interface{})["annotations"] = mergedAnnotations
			obj.object.UnstructuredContent()["metadata"].(map[string]interface{})["labels"] = mergedLabels
		} else {
			obj.object.UnstructuredContent()[key] = mergedObj
		}

		if keyUpdateNeeded {
			if strings.EqualFold(string(remediation), string(policyv1.Inform)) {
				return true, "", false, false, false
			} else if isStatus {
				statusUpdated = true
				log.Info("Ignoring an update to the object status", "key", key)
			} else {
				updateNeeded = true
				log.Info("Queuing an update for the object due to a value mismatch", "key", key)
			}
		}
	}

	if updateNeeded {
		log.V(2).Info("Updating the object based on the template definition")

		if err := r.validateObject(obj.object); err != nil {
			message := fmt.Sprintf("Error validating the object %s, the error is `%v`", obj.name, err)

			return false, message, true, updateNeeded, false
		}

		_, err = res.Update(context.TODO(), obj.object, metav1.UpdateOptions{})
		if k8serrors.IsNotFound(err) {
			message := fmt.Sprintf("`%v` is not present and must be created", obj.object.GetKind())

			return false, message, true, updateNeeded, false
		}

		if err != nil {
			message := fmt.Sprintf("Error updating the object `%v`, the error is `%v`", obj.name, err)

			return false, message, true, updateNeeded, false
		}

		updateSucceeded = true
	}

	return statusUpdated, "", false, updateNeeded, updateSucceeded
}

// AppendCondition check and appends conditions to the policy status
func AppendCondition(
	conditions []policyv1.Condition, newCond *policyv1.Condition, resourceType string, resolved ...bool,
) (conditionsRes []policyv1.Condition) {
	defer recoverFlow()

	lastIndex := len(conditions)
	if lastIndex > 0 {
		oldCond := conditions[lastIndex-1]
		if IsSimilarToLastCondition(oldCond, *newCond) {
			conditions[lastIndex-1] = *newCond

			return conditions
		}
	} else {
		// first condition => trigger event
		conditions = append(conditions, *newCond)

		return conditions
	}

	conditions[lastIndex-1] = *newCond

	return conditions
}

// IsSimilarToLastCondition checks the diff, so that we don't keep updating with the same info
func IsSimilarToLastCondition(oldCond policyv1.Condition, newCond policyv1.Condition) bool {
	return reflect.DeepEqual(oldCond.Status, newCond.Status) &&
		reflect.DeepEqual(oldCond.Reason, newCond.Reason) &&
		reflect.DeepEqual(oldCond.Message, newCond.Message) &&
		reflect.DeepEqual(oldCond.Type, newCond.Type)
}

// addForUpdate calculates the compliance status of a configurationPolicy and updates the status field. The sendEvent
// argument determines if a status update event should be sent on the parent policy and configuration policy.
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

	_, err := r.updatePolicyStatus(policy, sendEvent)
	policyLog := log.WithValues("name", policy.Name, "namespace", policy.Namespace)

	if k8serrors.IsConflict(err) {
		policyLog.Error(err, "Tried to re-update status before previous update could be applied, retrying next loop")
	} else if err != nil {
		policyLog.Error(err, "Could not update status, retrying next loop")

		parent := ""
		if len(policy.OwnerReferences) > 0 {
			parent = policy.OwnerReferences[0].Name
		}

		policySystemErrorsCounter.WithLabelValues(parent, policy.GetName(), "status-update-failed").Add(1)
	}
}

// updatePolicyStatus updates the status of the configurationPolicy if new conditions are added and generates an event
// on the parent policy and configuration policy with the compliance decision if the sendEvent argument is true.
func (r *ConfigurationPolicyReconciler) updatePolicyStatus(
	policy *policyv1.ConfigurationPolicy,
	sendEvent bool,
) (*policyv1.ConfigurationPolicy, error) {
	if sendEvent {
		log.V(1).Info("Sending parent policy compliance event")

		// If the compliance event can't be created, then don't update the ConfigurationPolicy
		// status. As long as that hasn't been updated, everything will be retried next loop.
		if err := r.sendComplianceEvent(policy); err != nil {
			return policy, err
		}
	}

	log.V(2).Info(
		"Updating configurationPolicy status", "status", policy.Status.ComplianceState, "policy", policy.GetName(),
	)

	err := r.Status().Update(context.TODO(), policy)
	if err != nil {
		return policy, err
	}

	if sendEvent {
		log.V(1).Info("Sending policy status update event")

		r.Recorder.Event(policy, "Normal", "Policy updated",
			fmt.Sprintf("Policy status is: %v", policy.Status.ComplianceState))
	}

	return nil, nil
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
		Message: convertPolicyStatusToString(instance),
		Source: corev1.EventSource{
			Component: ControllerName,
			Host:      r.InstanceName,
		},
		FirstTimestamp: metav1.NewTime(now),
		LastTimestamp:  metav1.NewTime(now),
		Count:          1,
		Type:           "Normal",
		EventTime:      metav1.NewMicroTime(now),
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

	if instance.Status.ComplianceState != policyv1.Compliant {
		event.Type = "Warning"
	}

	return r.Create(context.TODO(), event)
}

// convertPolicyStatusToString to be able to pass the status as event
func convertPolicyStatusToString(plc *policyv1.ConfigurationPolicy) (results string) {
	if plc.Status.ComplianceState == "" || plc.Status.ComplianceState == policyv1.UnknownCompliancy {
		return "ComplianceState is still unknown"
	}

	result := string(plc.Status.ComplianceState)

	if plc.Status.CompliancyDetails == nil || len(plc.Status.CompliancyDetails) == 0 {
		return result
	}

	for _, v := range plc.Status.CompliancyDetails {
		result += "; "
		for idx, cond := range v.Conditions {
			result += cond.Type + " - " + cond.Message
			if idx != len(v.Conditions)-1 {
				result += ", "
			}
		}
	}

	runeResult := []rune(result)

	if len(runeResult) > 1024 {
		result = string(append(runeResult[:1021], '.', '.', '.'))
	}

	return result
}

// getDeployment gets the Deployment object associated with this controller. If the controller is running outside of
// a cluster, no Deployment object or error will be returned.
func (r *ConfigurationPolicyReconciler) getDeployment() (*appsv1.Deployment, error) {
	key, err := common.GetOperatorNamespacedName()
	if err != nil {
		// Running locally
		if errors.Is(err, common.ErrNoNamespace) || errors.Is(err, common.ErrRunLocal) {
			return nil, nil
		}

		return nil, err
	}

	deployment := appsv1.Deployment{}
	if err := r.Client.Get(context.TODO(), key, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

func (r *ConfigurationPolicyReconciler) isBeingUninstalled() (bool, error) {
	deployment, err := r.getDeployment()
	if deployment == nil || err != nil {
		return false, err
	}

	return deployment.Annotations[common.UninstallingAnnotation] == "true", nil
}

// removeLegacyDeploymentFinalizer removes the policy.open-cluster-management.io/delete-related-objects on the
// Deployment object. This finalizer is no longer needed on the Deployment object, so it is removed.
func (r *ConfigurationPolicyReconciler) removeLegacyDeploymentFinalizer() error {
	deployment, err := r.getDeployment()
	if deployment == nil || err != nil {
		return err
	}

	for i, finalizer := range deployment.Finalizers {
		if finalizer == pruneObjectFinalizer {
			newFinalizers := append(deployment.Finalizers[:i], deployment.Finalizers[i+1:]...)
			deployment.SetFinalizers(newFinalizers)

			log.Info("Removing the legacy finalizer on the controller Deployment")

			err := r.Client.Update(context.TODO(), deployment)

			return err
		}
	}

	return nil
}

func recoverFlow() {
	if r := recover(); r != nil {
		// V(-2) is the error level
		log.V(-2).Info("ALERT!!!! -> recovered from ", "recover", r)
	}
}
