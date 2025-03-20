package controllers

import (
	"context"
	"errors"
	"strconv"
	"strings"

	templates "github.com/stolostron/go-template-utils/v6/pkg/templates"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	yaml "sigs.k8s.io/yaml"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
)

type hubResolver interface {
	getClient() client.Client
	getHubDynamicWatcher() depclient.DynamicWatcher
	getClusterName() string
	getHubClient() *kubernetes.Clientset
}

func resolveHubTemplates(ctx context.Context, r hubResolver, policyCopy client.Object) error {
	yamlBytes, err := yaml.Marshal(policyCopy)
	if err != nil { // This condition is likely impossible.
		return err
	}

	if !templates.HasTemplate(yamlBytes, "{{hub", false) {
		return nil
	}

	hubDynamicWatcher := r.getHubDynamicWatcher()

	if hubDynamicWatcher == nil {
		return errors.New("the governance-standalone-hub-templating addon must be enabled to resolve " +
			"hub templates on the managed cluster")
	}

	objID := depclient.ObjectIdentifier{
		Group:     policyCopy.GetObjectKind().GroupVersionKind().Group,
		Version:   policyCopy.GetObjectKind().GroupVersionKind().Version,
		Kind:      policyCopy.GetObjectKind().GroupVersionKind().Kind,
		Namespace: policyCopy.GetNamespace(),
		Name:      policyCopy.GetName(),
	}

	if err := hubDynamicWatcher.StartQueryBatch(objID); err != nil {
		log.Error(err, "Failed to start the query batch")

		return err
	}

	defer func() {
		if err := hubDynamicWatcher.EndQueryBatch(objID); err != nil {
			log.Error(err, "Failed to stop the query batch using the dynamic watcher", "watcher", objID)
		}
	}()

	tmplConfig := templates.Config{
		StartDelim:          "{{hub",
		StopDelim:           "hub}}",
		SkipBatchManagement: true,
	}

	hubTemplateResolver, err := templates.NewResolverWithDynamicWatcher(hubDynamicWatcher, tmplConfig)
	if err != nil {
		log.Error(err, "Failed to create hub template resolver")

		return err
	}

	resolveOptions := &templates.ResolveOptions{
		InputIsYAML: true,
		Watcher:     &objID,
	}

	if strings.Contains(string(yamlBytes), ".ManagedClusterLabels") {
		resolveOptions.ContextTransformers = append(
			resolveOptions.ContextTransformers, addManagedClusterLabels(r.getClusterName()))
	}

	tmplCtx := templateCtx{ManagedClusterName: r.getClusterName()}

	cachedResultObject := &corev1.Secret{}

	cacheKey := types.NamespacedName{
		Namespace: policyCopy.GetNamespace(),
		Name:      string(policyCopy.GetUID()) + "-last-resolved",
	}

	cacheGetErr := r.getClient().Get(ctx, cacheKey, cachedResultObject)

	result, err := hubTemplateResolver.ResolveTemplate(yamlBytes, tmplCtx, resolveOptions)
	if err != nil {
		// Check if the hub is accessible
		if _, connErr := r.getHubClient().ServerVersion(); connErr != nil {
			// Assume the hub is currently inaccessible.
			if cacheGetErr != nil {
				return err
			}

			// check the cache-secret generation to actually verify if it's up-to-date
			inputGeneration := policyCopy.GetGeneration()

			jsonErr := json.Unmarshal(cachedResultObject.Data["policy.json"], policyCopy)
			if jsonErr != nil {
				return err
			}

			if inputGeneration == policyCopy.GetGeneration() {
				return nil
			}
		}

		// The hub is accessible, clear the cache and return the template error

		if cacheGetErr != nil {
			if !k8serrors.IsNotFound(cacheGetErr) {
				// can't clear the cache right now, but this should be retried anyway
				return cacheGetErr
			}
		}

		// ignore error; if the delete fails, it will be retried anyway
		_ = r.getClient().Delete(ctx, cachedResultObject)

		return err
	}

	jsonErr := json.Unmarshal(result.ResolvedJSON, policyCopy)
	if jsonErr != nil {
		log.Error(jsonErr, "unable to unmarshal resolved hub template JSON into a policy")

		return err
	}

	if k8serrors.IsNotFound(cacheGetErr) {
		// cache secret not found, so create it
		resolutionToCache := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cacheKey.Namespace,
				Name:      cacheKey.Name,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: policyCopy.GetObjectKind().GroupVersionKind().GroupVersion().String(),
					Kind:       policyCopy.GetObjectKind().GroupVersionKind().Kind,
					Name:       policyCopy.GetName(),
					UID:        policyCopy.GetUID(),
				}},
			},
			Data: map[string][]byte{
				"policy.json": result.ResolvedJSON,
			},
		}

		if err := r.getClient().Create(ctx, &resolutionToCache); err != nil {
			log.Error(err, "failure to create secret")

			return err
		}

		return nil
	} else if cacheGetErr != nil {
		// in order to UPDATE the cache secret, the GET must be successful. So a retry is necessary.
		log.Error(cacheGetErr, "failed to get cached secret, and needed it after a successful resolution")

		return cacheGetErr
	}

	cachedResultObject.Data["policy.json"] = result.ResolvedJSON

	err = r.getClient().Update(ctx, cachedResultObject)
	if err != nil {
		log.Error(err, "failed to update cached secret with successful resolution")

		return err
	}

	return nil
}

type templateCtx struct {
	ManagedClusterName   string
	ManagedClusterLabels map[string]string
}

func addManagedClusterLabels(clusterName string) func(templates.CachingQueryAPI, interface{}) (interface{}, error) {
	return func(api templates.CachingQueryAPI, ctx interface{}) (interface{}, error) {
		typedCtx, ok := ctx.(templateCtx)
		if !ok {
			return ctx, nil
		}

		managedClusterGVK := schema.GroupVersionKind{
			Group:   "cluster.open-cluster-management.io",
			Version: "v1",
			Kind:    "ManagedCluster",
		}

		managedCluster, err := api.Get(managedClusterGVK, "", clusterName)
		if err != nil {
			return ctx, err
		}

		typedCtx.ManagedClusterLabels = managedCluster.GetLabels()

		return typedCtx, nil
	}
}

func (r *ConfigurationPolicyReconciler) getClient() client.Client {
	return r.Client
}

func (r *ConfigurationPolicyReconciler) getHubDynamicWatcher() depclient.DynamicWatcher {
	return r.HubDynamicWatcher
}

func (r *ConfigurationPolicyReconciler) getClusterName() string {
	return r.ClusterName
}

func (r *ConfigurationPolicyReconciler) getHubClient() *kubernetes.Clientset {
	return r.HubClient
}

func (r *ConfigurationPolicyReconciler) resolveHubTemplates(
	ctx context.Context, policy *policyv1.ConfigurationPolicy,
) error {
	if disableAnnotation, ok := policy.GetAnnotations()["policy.open-cluster-management.io/disable-templates"]; ok {
		disableTemplates, _ := strconv.ParseBool(disableAnnotation) // on error, templates will not be disabled

		if disableTemplates {
			return nil
		}
	}

	// Ignore status and most metadata when resolving templates.
	// This is also the information that will be saved in the secret.
	policyCopy := policyv1.ConfigurationPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       policy.Kind,
			APIVersion: policy.APIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       policy.Name,
			Namespace:  policy.Namespace,
			Generation: policy.Generation,
			UID:        policy.UID,
		},
		Spec: *policy.Spec.DeepCopy(),
	}

	if err := resolveHubTemplates(ctx, r, &policyCopy); err != nil {
		return err
	}

	policy.Spec = policyCopy.Spec

	return nil
}

func (r *OperatorPolicyReconciler) getClient() client.Client {
	return r.Client
}

func (r *OperatorPolicyReconciler) getHubDynamicWatcher() depclient.DynamicWatcher {
	return r.HubDynamicWatcher
}

func (r *OperatorPolicyReconciler) getClusterName() string {
	return r.ClusterName
}

func (r *OperatorPolicyReconciler) getHubClient() *kubernetes.Clientset {
	return r.HubClient
}

func (r *OperatorPolicyReconciler) resolveHubTemplates(
	ctx context.Context, policy *policyv1beta1.OperatorPolicy,
) error {
	if disableAnnotation, ok := policy.GetAnnotations()["policy.open-cluster-management.io/disable-templates"]; ok {
		disableTemplates, _ := strconv.ParseBool(disableAnnotation) // on error, templates will not be disabled

		if disableTemplates {
			return nil
		}
	}

	// Ignore status and most metadata when resolving templates.
	// This is also the information that will be saved in the secret.
	policyCopy := policyv1beta1.OperatorPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       policy.Kind,
			APIVersion: policy.APIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       policy.Name,
			Namespace:  policy.Namespace,
			Generation: policy.Generation,
			UID:        policy.UID,
		},
		Spec: *policy.Spec.DeepCopy(),
	}

	if err := resolveHubTemplates(ctx, r, &policyCopy); err != nil {
		return err
	}

	policy.Spec = policyCopy.Spec

	return nil
}
