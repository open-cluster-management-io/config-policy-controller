// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package configurationpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	policyv1alpha1 "github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/apis/policies/v1alpha1"
	common "github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/common"
	alerttargetcontroller "github.ibm.com/OMaaS/alerttargetcontroller/pkg/apis/alerttargetcontroller/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"k8s.io/client-go/restmapper"
)

var log = logf.Log.WithName("controller_configurationpolicy")

// Finalizer used to ensure consistency when deleting a CRD
const Finalizer = "finalizer.policies.ibm.com"

const grcCategory = "system-and-information-integrity"

// availablePolicies is a cach all all available polices
var availablePolicies common.SyncedPolicyMap

// PlcChan a channel used to pass policies ready for update
var PlcChan chan *policyv1alpha1.ConfigurationPolicy

var recorder record.EventRecorder

var config *rest.Config

var syncAlertTargets bool

var CemWebhookURL string

var clusterName string

// KubeClient a k8s client used for k8s native resources
var KubeClient *kubernetes.Interface

var reconcilingAgent *ReconcileConfigurationPolicy

// NamespaceWatched defines which namespace we can watch for the GRC policies and ignore others
var NamespaceWatched string

// EventOnParent specifies if we also want to send events to the parent policy. Available options are yes/no/ifpresent
var EventOnParent string

// PrometheusAddr port addr for prom metrics
var PrometheusAddr string

// Add creates a new ConfigurationPolicy Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileConfigurationPolicy{client: mgr.GetClient(), scheme: mgr.GetScheme(), recorder: mgr.GetEventRecorderFor("configurationpolicy-controller")}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("configurationpolicy-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ConfigurationPolicy
	err = c.Watch(&source.Kind{Type: &policyv1alpha1.ConfigurationPolicy{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	// Watch for changes to secondary resource Pods and requeue the owner ConfigurationPolicy
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &policyv1alpha1.ConfigurationPolicy{},
	})
	if err != nil {
		return err
	}

	return nil
}

// Initialize to initialize some controller variables
func Initialize(kubeconfig *rest.Config, kClient *kubernetes.Interface, mgr manager.Manager, namespace, eventParent string,
	syncAlert bool, clustName string) {
	KubeClient = kClient
	PlcChan = make(chan *policyv1alpha1.ConfigurationPolicy, 100) //buffering up to 100 policies for update

	NamespaceWatched = namespace

	EventOnParent = strings.ToLower(eventParent)

	recorder, _ = common.CreateRecorder(*KubeClient, "policy-controller")
	config = kubeconfig
	syncAlertTargets = syncAlert

	if clustName == "" {
		clusterName = "mcm-managed-cluster"
	} else {
		clusterName = clustName
	}
}

// blank assignment to verify that ReconcileConfigurationPolicy implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileConfigurationPolicy{}

// ReconcileConfigurationPolicy reconciles a ConfigurationPolicy object
type ReconcileConfigurationPolicy struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	recorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a ConfigurationPolicy object and makes changes based on the state read
// and what is in the ConfigurationPolicy.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileConfigurationPolicy) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling ConfigurationPolicy")

	// Fetch the ConfigurationPolicy instance
	instance := &policyv1alpha1.ConfigurationPolicy{}
	if reconcilingAgent == nil {
		reconcilingAgent = r
	}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// name of our mcm custom finalizer
	myFinalizerName := Finalizer

	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		updateNeeded := false
		// The object is not being deleted, so if it might not have our finalizer,
		// then lets add the finalizer and update the object.
		if !containsString(instance.ObjectMeta.Finalizers, myFinalizerName) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, myFinalizerName)
			updateNeeded = true
		}
		if !ensureDefaultLabel(instance) {
			updateNeeded = true
		}
		if updateNeeded {
			if err := r.client.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
		instance.Status.CompliancyDetails = nil //reset CompliancyDetails
		err := handleAddingPolicy(instance)
		if err != nil {
			glog.V(3).Infof("Failed to handleAddingPolicy")
		}
	} else {
		handleRemovingPolicy(instance)
		// The object is being deleted
		if containsString(instance.ObjectMeta.Finalizers, myFinalizerName) {
			// our finalizer is present, so lets handle our external dependency
			if err := r.deleteExternalDependency(instance); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return reconcile.Result{}, err
			}

			// remove our finalizer from the list and update it.
			instance.ObjectMeta.Finalizers = removeString(instance.ObjectMeta.Finalizers, myFinalizerName)
			if err := r.client.Update(context.Background(), instance); err != nil {
				return reconcile.Result{Requeue: true}, nil
			}
		}
		// Our finalizer has finished, so the reconciler can do nothing.
		return reconcile.Result{}, nil
	}
	glog.V(3).Infof("reason: successful processing, subject: policy/%v, namespace: %v, according to policy: %v, additional-info: none",
		instance.Name, instance.Namespace, instance.Name)

	// Pod already exists - don't requeue
	// reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	return reconcile.Result{}, nil
}

// PeriodicallyExecSamplePolicies always check status
func PeriodicallyExecSamplePolicies(freq uint) {
	var plcToUpdateMap map[string]*policyv1alpha1.ConfigurationPolicy
	for {
		start := time.Now()
		printMap(availablePolicies.PolicyMap)
		plcToUpdateMap = make(map[string]*policyv1alpha1.ConfigurationPolicy)
		for namespace, policy := range availablePolicies.PolicyMap {
			//For each namespace, fetch all the RoleBindings in that NS according to the policy selector
			//For each RoleBindings get the number of users
			//update the status internal map
			//no difference between enforce and inform here
			roleBindingList, err := (*common.KubeClient).RbacV1().RoleBindings(namespace).
				List(metav1.ListOptions{LabelSelector: labels.Set(policy.Spec.LabelSelector).String()})
			if err != nil {
				glog.Errorf("reason: communication error, subject: k8s API server, namespace: %v, according to policy: %v, additional-info: %v\n",
					namespace, policy.Name, err)
				continue
			}
			userViolationCount, GroupViolationCount := checkViolationsPerNamespace(roleBindingList, policy)
			if strings.EqualFold(string(policy.Spec.RemediationAction), string(policyv1alpha1.Enforce)) {
				glog.V(5).Infof("Enforce is set, but ignored :-)")
			}
			if addViolationCount(policy, userViolationCount, GroupViolationCount, namespace) {
				plcToUpdateMap[policy.Name] = policy
			}
			handlePolicyTemplates(policy)
			checkComplianceBasedOnDetails(policy)
		}
		err := checkUnNamespacedPolicies(plcToUpdateMap)
		if err != nil {
			glog.V(3).Infof("Failed to checkUnNamespacedPolicies")
		}

		//update status of all policies that changed:
		faultyPlc, err := updatePolicyStatus(plcToUpdateMap)
		if err != nil {
			glog.Errorf("reason: policy update error, subject: policy/%v, namespace: %v, according to policy: %v, additional-info: %v\n",
				faultyPlc.Name, faultyPlc.Namespace, faultyPlc.Name, err)
		}

		// making sure that if processing is > freq we don't sleep
		// if freq > processing we sleep for the remaining duration
		elapsed := time.Since(start) / 1000000000 // convert to seconds
		if float64(freq) > float64(elapsed) {
			remainingSleep := float64(freq) - float64(elapsed)
			time.Sleep(time.Duration(remainingSleep) * time.Second)
		}
		if KubeClient == nil {
			return
		}
	}
}

func handlePolicyTemplates(plc *policyv1alpha1.ConfigurationPolicy) {
	if reflect.DeepEqual(plc.Labels["ignore"], "true") {
		plc.Status = policyv1alpha1.ConfigurationPolicyStatus{
			ComplianceState:   policyv1alpha1.UnknownCompliancy,
			CompliancyDetails: map[string]map[string][]string{},
		}
		return
	}
	namespace := plc.Namespace
	// relevantNamespaces := getPolicyNamespaces(ctx, plc)
	for indx, policyT := range plc.Spec.PolicyTemplates {
		glog.V(5).Infof("Handling Policy template [%v] from Policy `%v` in namespace `%v`", indx, plc.Name, namespace)
		handlePolicyObjects(policyT, plc, namespace, KubeClient, indx)
	}
}

func handlePolicyObjects(policyT *policyv1alpha1.PolicyTemplate, policy *policyv1alpha1.ConfigurationPolicy, ns string, kclient *kubernetes.Interface, index int) {
	namespaced := true
	updateNeeded := false

	dd := (*kclient).Discovery()
	apigroups, err := restmapper.GetAPIGroupResources(dd)
	if err != nil {
		glog.Fatal(err)
	}

	restmapper := restmapper.NewDiscoveryRESTMapper(apigroups)
	ext := policyT.ObjectDefinition
	glog.V(9).Infof("reading raw object: %v", string(ext.Raw))
	versions := &runtime.VersionedObjects{}
	_, gvk, dErr := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, versions)
	mapping, err := restmapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	restconfig := config
	restconfig.GroupVersion = &schema.GroupVersion{
		Group:   mapping.GroupVersionKind.Group,
		Version: mapping.GroupVersionKind.Version,
	}
	gvr := schema.GroupVersionResource{
		Group:    mapping.GroupVersionKind.Group,
		Version:  mapping.GroupVersionKind.Version,
		Resource: mapping.GroupVersionKind.Kind,
	}
	dclient, err := dynamic.NewForConfig(restconfig)
	if err != nil {
		glog.Fatal(err)
	}
	if dErr != nil {
		decodeErr := fmt.Sprintf("Decoding error, please check your policy file! Aborting handling the object template at index [%v] in policy `%v` with error = `%v`", index, policy.Name, err)
		glog.Errorf(decodeErr)
		//policy.Status.Message = decodeErr
		updatePolicy(policy, 0, &dclient, &gvr)
		return
	}

	if err != nil {
		message := fmt.Sprintf("mapping error from raw object: `%v`", err)
		prefix := "no matches for kind \""
		startIdx := strings.Index(err.Error(), prefix)
		if startIdx == -1 {
			glog.Errorf(message, err)
		} else {
			afterPrefix := err.Error()[(startIdx + len(prefix)):len(err.Error())]
			kind := afterPrefix[0:(strings.Index(afterPrefix, "\" "))]
			message = "couldn't find mapping resource with kind " + kind + ", please check if you have corresponding policy controller deployed"
			glog.Errorf(message)
		}
		cond := &policyv1alpha1.Condition{
			Type:               "violation",
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s creation error",
			Message:            message,
		}
		if policyT.Status.ComplianceState != policyv1alpha1.NonCompliant {
			updateNeeded = true
		}
		policyT.Status.ComplianceState = policyv1alpha1.NonCompliant
		policyT.Status.Conditions = AppendCondition(policyT.Status.Conditions, cond, gvk.GroupKind().Kind, false)
		if updateNeeded {
			recorder.Event(policy, "Warning", cond.Reason, cond.Message)
			addForUpdate(policy, 0, &dclient, &gvr)
		}
		return
	}
	glog.V(9).Infof("mapping found from raw object: %v", mapping)

	apiresourcelist, err := dd.ServerResources()
	if err != nil {
		glog.Fatal(err)
	}

	rsrc := mapping.Resource
	for _, apiresourcegroup := range apiresourcelist {
		if apiresourcegroup.GroupVersion == join(mapping.GroupVersionKind.Group, "/", mapping.GroupVersionKind.Version) {
			for _, apiresource := range apiresourcegroup.APIResources {
				if apiresource.Name == mapping.Resource.Resource && apiresource.Kind == mapping.GroupVersionKind.Kind {
					rsrc = mapping.Resource
					namespaced = apiresource.Namespaced
					glog.V(7).Infof("is raw object namespaced? %v", namespaced)
				}
			}
		}
	}
	var unstruct unstructured.Unstructured
	unstruct.Object = make(map[string]interface{})
	var blob interface{}
	if err = json.Unmarshal(ext.Raw, &blob); err != nil {
		glog.Fatal(err)
	}
	unstruct.Object = blob.(map[string]interface{}) //set object to the content of the blob after Unmarshalling

	name := ""
	if md, ok := unstruct.Object["metadata"]; ok {

		metadata := md.(map[string]interface{})
		if objectName, ok := metadata["name"]; ok {
			name = objectName.(string)
		}
	}

	exists := objectExists(namespaced, ns, name, rsrc, unstruct, dclient)

	if !exists {
		// policy object doesn't exist let's create it
		created, err := createObject(namespaced, ns, name, rsrc, unstruct, dclient, policy)
		if !created {
			message := fmt.Sprintf("%v `%v` is missing, and cannot be created, reason: `%v`", rsrc.Resource, name, err)
			cond := &policyv1alpha1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation error",
				Message:            message,
			}
			if policyT.Status.ComplianceState != policyv1alpha1.NonCompliant {
				updateNeeded = true
				policyT.Status.ComplianceState = policyv1alpha1.NonCompliant
			}

			if !checkPolicyMessageSimilarity(policyT, cond) {
				policyT.Status.Conditions = AppendCondition(policyT.Status.Conditions, cond, rsrc.Resource, false)
				updateNeeded = true
			}
			if updateNeeded {
				addForUpdate(policy, 0, &dclient, &gvr)
			}
		}
		if err != nil {
			glog.Errorf("error creating policy object `%v` from policy `%v`", name, policy.Name)

		}
	} else {
		updated, msg := updateTemplate(namespaced, ns, name, rsrc, unstruct, dclient, unstruct.Object["kind"].(string), policy)
		if !updated && msg != "" {
			cond := &policyv1alpha1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s update template error",
				Message:            msg,
			}
			if policyT.Status.ComplianceState != policyv1alpha1.NonCompliant {
				updateNeeded = true
				policyT.Status.ComplianceState = policyv1alpha1.NonCompliant
			}

			if !checkPolicyMessageSimilarity(policyT, cond) {
				policyT.Status.Conditions = AppendCondition(policyT.Status.Conditions, cond, rsrc.Resource, false)
				updateNeeded = true
			}
			if updateNeeded {
				addForUpdate(policy, 0, &dclient, &gvr)
			}
			glog.Errorf(msg)
		}
	}
}

func checkPolicyMessageSimilarity(policyT *policyv1alpha1.PolicyTemplate, cond *policyv1alpha1.Condition) bool {
	same := true
	lastIndex := len(policyT.Status.Conditions)
	if lastIndex > 0 {
		oldCond := policyT.Status.Conditions[lastIndex-1]
		if !IsSimilarToLastCondition(oldCond, *cond) {
			// policyT.Status.Conditions = AppendCondition(policyT.Status.Conditions, cond, "policy", false)
			same = false
		}
	} else {
		// policyT.Status.Conditions = AppendCondition(policyT.Status.Conditions, cond, "policy", false)
		same = false
	}
	return same
}

func objectExists(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface) (result bool) {
	exists := false
	if !namespaced {
		res := dclient.Resource(rsrc)
		_, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				glog.V(6).Infof("response to retrieve a non namespaced object `%v` from the api-server: %v", name, err)
				exists = false
				return exists
			}
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)

		} else {
			exists = true
			glog.V(6).Infof("object `%v` retrieved from the api server\n", name)
		}
	} else {
		res := dclient.Resource(rsrc).Namespace(namespace)
		_, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				exists = false
				glog.V(6).Infof("response to retrieve a namespaced object `%v` from the api-server: %v", name, err)
				return exists
			}
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			exists = true
			glog.V(6).Infof("object `%v` retrieved from the api server\n", name)
		}
	}
	return exists
}

func createObject(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface, parent *policyv1alpha1.ConfigurationPolicy) (result bool, erro error) {
	var err error
	created := false
	// set ownerReference for mutaionPolicy and override remediationAction
	if parent != nil {
		plcOwnerReferences := *metav1.NewControllerRef(parent, schema.GroupVersionKind{
			Group:   policyv1alpha1.SchemeGroupVersion.Group,
			Version: policyv1alpha1.SchemeGroupVersion.Version,
			Kind:    "Policy",
		})
		labels := unstruct.GetLabels()
		if labels == nil {
			labels = map[string]string{"cluster-namespace": namespace}
		} else {
			labels["cluster-namespace"] = namespace
		}
		unstruct.SetLabels(labels)
		unstruct.SetOwnerReferences([]metav1.OwnerReference{plcOwnerReferences})
		if spec, ok := unstruct.Object["spec"]; ok {
			specObject := spec.(map[string]interface{})
			if _, ok := specObject["remediationAction"]; ok {
				specObject["remediationAction"] = parent.Spec.RemediationAction
			}
		}
	}

	glog.V(6).Infof("createObject:  `%s`", unstruct)

	if !namespaced {
		res := dclient.Resource(rsrc)

		_, err = res.Create(&unstruct, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				created = true
				glog.V(9).Infof("%v\n", err.Error())
			} else {
				glog.Errorf("Error creating the object `%v`, the error is `%v`", name, errors.ReasonForError(err))
			}
		} else {
			created = true
			glog.V(4).Infof("Resource `%v` created\n", name)
		}
	} else {
		res := dclient.Resource(rsrc).Namespace(namespace)
		_, err = res.Create(&unstruct, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				created = true
				glog.V(9).Infof("%v\n", err.Error())
			} else {
				glog.Errorf("Error creating the object `%v`, the error is `%v`", name, errors.ReasonForError(err))
			}
		} else {
			created = true
			glog.V(4).Infof("Resource `%v` created\n", name)

		}
	}
	return created, err
}

func updateTemplate(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface, typeStr string, parent *policyv1alpha1.ConfigurationPolicy) (success bool, message string) {
	if namespaced {
		res := dclient.Resource(rsrc).Namespace(namespace)
		existingObj, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			newObj := unstruct.Object["spec"]
			oldObj := existingObj.UnstructuredContent()["spec"]
			if parent != nil {
				// overwrite remediation from parent
				newObj.(map[string]interface{})["remediationAction"] = parent.Spec.RemediationAction
			}
			updateNeeded := !(reflect.DeepEqual(newObj, oldObj))
			mapMtx := sync.RWMutex{}
			mapMtx.Lock()
			existingObj.UnstructuredContent()["spec"] = newObj
			mapMtx.Unlock()
			if updateNeeded {
				glog.V(4).Infof("Updating %v template `%v`...", typeStr, name)
				_, err = res.Update(existingObj, metav1.UpdateOptions{})
				if errors.IsNotFound(err) {
					message := fmt.Sprintf("`%v` is not present and must be created", typeStr)
					return false, message
				}
				if err != nil {
					message := fmt.Sprintf("Error updating the object `%v`, the error is `%v`", name, err)
					return false, message
				}
				glog.V(4).Infof("Resource `%v` updated\n", name)
				return true, ""
			}
		}
	} else {
		res := dclient.Resource(rsrc)
		existingObj, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			newObj := unstruct.Object["spec"]
			oldObj := existingObj.UnstructuredContent()["spec"]
			updateNeeded := !(reflect.DeepEqual(newObj, oldObj))
			oldMap := existingObj.UnstructuredContent()["metadata"].(map[string]interface{})
			resVer := oldMap["resourceVersion"]
			mapMtx := sync.RWMutex{}
			mapMtx.Lock()
			unstruct.Object["metadata"].(map[string]interface{})["resourceVersion"] = resVer
			mapMtx.Unlock()
			if updateNeeded {
				glog.V(4).Infof("Updating %v template `%v`...", typeStr, name)
				_, err = res.Update(&unstruct, metav1.UpdateOptions{})
				if errors.IsNotFound(err) {
					message := fmt.Sprintf("`%v` is not present and must be created", typeStr)
					return false, message
				}
				if err != nil {
					message := fmt.Sprintf("Error updating the object `%v`, the error is `%v`", name, err)
					return false, message
				}
				glog.V(4).Infof("Resource `%v` updated\n", name)
				return true, ""
			}
		}
	}
	return false, ""
}

func updatePolicy(plc *policyv1alpha1.ConfigurationPolicy, retry int, dclient *dynamic.Interface, gvr *schema.GroupVersionResource) error {
	setStatus(plc)
	copy := plc.DeepCopy()

	var tmp policyv1alpha1.ConfigurationPolicy
	tmp = *plc

	crdClient := (*dclient).Resource(*gvr)
	_, err := crdClient.Namespace(tmp.Namespace).Get(tmp.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Error fetching policy %v, from the K8s API server the error is: %v", plc.Name, err)
	}

	if copy.ResourceVersion != tmp.ResourceVersion {
		copy.ResourceVersion = tmp.ResourceVersion
	}

	_, err = crdClient.Namespace(tmp.Namespace).Update(newConfigurationPolicy(copy), metav1.UpdateOptions{})
	if err != nil {
		glog.Errorf("Error update policy %v, the error is: %v", plc.Name, err)
	}
	glog.V(2).Infof("Updated the policy `%v` in namespace `%v`", plc.Name, plc.Namespace)

	return err
}

// AppendCondition check and appends conditions
func AppendCondition(conditions []policyv1alpha1.Condition, newCond *policyv1alpha1.Condition, resourceType string, resolved ...bool) (conditionsRes []policyv1alpha1.Condition) {
	defer recoverFlow()
	lastIndex := len(conditions)
	if lastIndex > 0 {
		oldCond := conditions[lastIndex-1]
		if IsSimilarToLastCondition(oldCond, *newCond) {
			conditions[lastIndex-1] = *newCond
			return conditions
		}
		//different than the last event, trigger event
		if syncAlertTargets {
			res, err := triggerEvent(*newCond, resourceType, resolved)
			if err != nil {
				glog.Errorf("event failed to be triggered: %v", err)
			}
			glog.V(3).Infof("event triggered: %v", res)
		}

	} else {
		//first condition => trigger event
		if syncAlertTargets {
			res, err := triggerEvent(*newCond, resourceType, resolved)
			if err != nil {
				glog.Errorf("event failed to be triggered: %v", err)
			}
			glog.V(3).Infof("event triggered: %v", res)
		}
		conditions = append(conditions, *newCond)
		return conditions
	}
	conditions[lastIndex-1] = *newCond
	return conditions
}

//IsSimilarToLastCondition checks the diff, so that we don't keep updating with the same info
func IsSimilarToLastCondition(oldCond policyv1alpha1.Condition, newCond policyv1alpha1.Condition) bool {
	if reflect.DeepEqual(oldCond.Status, newCond.Status) &&
		reflect.DeepEqual(oldCond.Reason, newCond.Reason) &&
		reflect.DeepEqual(oldCond.Message, newCond.Message) &&
		reflect.DeepEqual(oldCond.Type, newCond.Type) {
		return true
	}
	return false
}

func triggerEvent(cond policyv1alpha1.Condition, resourceType string, resolved []bool) (res string, err error) {

	resolutionResult := false
	eventType := "notification"
	eventSeverity := "Normal"
	if len(resolved) > 0 {
		resolutionResult = resolved[0]
	}
	if !resolutionResult {
		eventSeverity = "Critical"
		eventType = "violation"
	}
	WebHookURL := GetCEMWebhookURL(NamespaceWatched, clusterName, config)
	event := common.CEMEvent{
		Resource: common.Resource{
			Name:    "compliance-issue",
			Cluster: clusterName,
			Type:    resourceType,
		},
		Summary:    cond.Message,
		Severity:   eventSeverity,
		Timestamp:  cond.LastTransitionTime.String(),
		Resolution: resolutionResult,
		Sender: common.Sender{
			Name:    "MCM Policy Controller",
			Cluster: clusterName,
			Type:    "K8s controller",
		},
		Type: common.Type{
			StatusOrThreshold: cond.Reason,
			EventType:         eventType,
		},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	result, err := common.PostEvent(WebHookURL, payload)
	return result, err
}

// GetCEMWebhookURL populate the webhook value from a CRD
func GetCEMWebhookURL(namespace, clusterName string, config *rest.Config) (url string) {

	alertClient, err := rest.UnversionedRESTClientFor(config)
	if err != nil {
		return ""
	}
	at := createAlertTargetInstance(namespace, clusterName)
	atmeta, err := meta.Accessor(at)
	if err != nil {
		return ""
	}
	err = alertClient.Get().
		Name(atmeta.GetName()).
		Namespace(atmeta.GetNamespace()).
		Resource("alerttargets").
		Body(at).
		Do().
		Into(&at)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Errorf("The CRD instance `%v` of the alertTarget is not found, therefore no events will be triggered", fmt.Sprintf("%s-%s", namespace, clusterName))
		} else {
			glog.Errorf("The CRD instance `%v` of the alertTarget is not accessible, therefore no events will be triggered", fmt.Sprintf("%s-%s", namespace, clusterName))
		}
		return ""
	}

	url = extractURL(at)

	glog.Infof("CEM Webhook URL found: %s", url)
	return url
}

func extractURL(instance alerttargetcontroller.AlertTarget) string {
	return instance.Spec.ComplianceWebhook
}

func createAlertTargetInstance(namespace, clusterName string) (instance alerttargetcontroller.AlertTarget) {
	atName := fmt.Sprintf("%s-%s", namespace, clusterName)
	at := alerttargetcontroller.AlertTarget{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AlertTarget",
			APIVersion: "v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      atName,
			Namespace: namespace,
		},
	}
	return at
}

func addForUpdate(policy *policyv1alpha1.ConfigurationPolicy, retry int, dclient *dynamic.Interface, gvr *schema.GroupVersionResource) {
	compliant := true
	for _, policyT := range policy.Spec.PolicyTemplates {
		if policyT.Status.ComplianceState == policyv1alpha1.NonCompliant {
			compliant = false
		}
	}
	if compliant {
		policy.Status.ComplianceState = policyv1alpha1.Compliant
	} else {
		policy.Status.ComplianceState = policyv1alpha1.NonCompliant
	}

	err := updatePolicy(policy, retry, dclient, gvr)
	if err != nil {
		time.Sleep(100) //giving enough time to sync
	}
}

func ensureDefaultLabel(instance *policyv1alpha1.ConfigurationPolicy) (updateNeeded bool) {
	//we need to ensure this label exists -> category: "System and Information Integrity"
	if instance.ObjectMeta.Labels == nil {
		newlbl := make(map[string]string)
		newlbl["category"] = grcCategory
		instance.ObjectMeta.Labels = newlbl
		return true
	}
	if _, ok := instance.ObjectMeta.Labels["category"]; !ok {
		instance.ObjectMeta.Labels["category"] = grcCategory
		return true
	}
	if instance.ObjectMeta.Labels["category"] != grcCategory {
		instance.ObjectMeta.Labels["category"] = grcCategory
		return true
	}
	return false
}

func checkUnNamespacedPolicies(plcToUpdateMap map[string]*policyv1alpha1.ConfigurationPolicy) error {
	plcMap := convertMaptoPolicyNameKey()
	// group the policies with cluster users and the ones with groups
	// take the plc with min users and groups and make it your baseline
	ClusteRoleBindingList, err := (*common.KubeClient).RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
	if err != nil {
		glog.Errorf("reason: communication error, subject: k8s API server, namespace: all, according to policy: none, additional-info: %v\n", err)
		return err
	}

	clusterLevelUsers, clusterLevelGroups := checkAllClusterLevel(ClusteRoleBindingList)

	for _, policy := range plcMap {
		var userViolationCount, groupViolationCount int
		if policy.Spec.MaxClusterRoleBindingUsers < clusterLevelUsers && policy.Spec.MaxClusterRoleBindingUsers >= 0 {
			userViolationCount = clusterLevelUsers - policy.Spec.MaxClusterRoleBindingUsers
		}
		if policy.Spec.MaxClusterRoleBindingGroups < clusterLevelGroups && policy.Spec.MaxClusterRoleBindingGroups >= 0 {
			groupViolationCount = clusterLevelGroups - policy.Spec.MaxClusterRoleBindingGroups
		}
		if addViolationCount(policy, userViolationCount, groupViolationCount, "cluster-wide") {
			plcToUpdateMap[policy.Name] = policy
		}
		checkComplianceBasedOnDetails(policy)
	}

	return nil
}

func checkAllClusterLevel(clusterRoleBindingList *v1.ClusterRoleBindingList) (userV, groupV int) {
	usersMap := make(map[string]bool)
	groupsMap := make(map[string]bool)
	for _, clusterRoleBinding := range clusterRoleBindingList.Items {
		for _, subject := range clusterRoleBinding.Subjects {
			if subject.Kind == "User" {
				usersMap[subject.Name] = true
			}
			if subject.Kind == "Group" {
				groupsMap[subject.Name] = true
			}
		}
	}
	return len(usersMap), len(groupsMap)
}

func convertMaptoPolicyNameKey() map[string]*policyv1alpha1.ConfigurationPolicy {
	plcMap := make(map[string]*policyv1alpha1.ConfigurationPolicy)
	for _, policy := range availablePolicies.PolicyMap {
		plcMap[policy.Name] = policy
	}
	return plcMap
}

func checkViolationsPerNamespace(roleBindingList *v1.RoleBindingList, plc *policyv1alpha1.ConfigurationPolicy) (userV, groupV int) {
	usersMap := make(map[string]bool)
	groupsMap := make(map[string]bool)
	for _, roleBinding := range roleBindingList.Items {
		for _, subject := range roleBinding.Subjects {
			if subject.Kind == "User" {
				usersMap[subject.Name] = true
			}
			if subject.Kind == "Group" {
				groupsMap[subject.Name] = true
			}
		}
	}
	var userViolationCount, groupViolationCount int
	if plc.Spec.MaxRoleBindingUsersPerNamespace < len(usersMap) && plc.Spec.MaxRoleBindingUsersPerNamespace >= 0 {
		userViolationCount = (len(usersMap) - plc.Spec.MaxRoleBindingUsersPerNamespace)
	}
	if plc.Spec.MaxRoleBindingGroupsPerNamespace < len(groupsMap) && plc.Spec.MaxRoleBindingGroupsPerNamespace >= 0 {
		groupViolationCount = (len(groupsMap) - plc.Spec.MaxRoleBindingGroupsPerNamespace)
	}
	return userViolationCount, groupViolationCount
}

func addViolationCount(plc *policyv1alpha1.ConfigurationPolicy, userCount int, groupCount int, namespace string) bool {
	changed := false
	msg := fmt.Sprintf("%s violations detected in namespace `%s`, there are %v users violations and %v groups violations",
		fmt.Sprint(userCount+groupCount),
		namespace,
		userCount,
		groupCount)
	if plc.Status.CompliancyDetails == nil {
		plc.Status.CompliancyDetails = make(map[string]map[string][]string)
	}
	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
		plc.Status.CompliancyDetails[plc.Name] = make(map[string][]string)
	}
	if plc.Status.CompliancyDetails[plc.Name][namespace] == nil {
		plc.Status.CompliancyDetails[plc.Name][namespace] = []string{}
	}
	if len(plc.Status.CompliancyDetails[plc.Name][namespace]) == 0 {
		plc.Status.CompliancyDetails[plc.Name][namespace] = []string{msg}
		changed = true
		return changed
	}
	firstNum := strings.Split(plc.Status.CompliancyDetails[plc.Name][namespace][0], " ")
	if len(firstNum) > 0 {
		if firstNum[0] == fmt.Sprint(userCount+groupCount) {
			return false
		}
	}
	plc.Status.CompliancyDetails[plc.Name][namespace][0] = msg
	changed = true
	return changed
}

func checkComplianceBasedOnDetails(plc *policyv1alpha1.ConfigurationPolicy) {
	plc.Status.ComplianceState = policyv1alpha1.Compliant
	if plc.Status.CompliancyDetails == nil {
		return
	}
	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
		return
	}
	if len(plc.Status.CompliancyDetails[plc.Name]) == 0 {
		return
	}
	for namespace, msgList := range plc.Status.CompliancyDetails[plc.Name] {
		if len(msgList) > 0 {
			violationNum := strings.Split(plc.Status.CompliancyDetails[plc.Name][namespace][0], " ")
			if len(violationNum) > 0 {
				if violationNum[0] != fmt.Sprint(0) {
					plc.Status.ComplianceState = policyv1alpha1.NonCompliant
				}
			}
		} else {
			return
		}
	}
}

func checkComplianceChangeBasedOnDetails(plc *policyv1alpha1.ConfigurationPolicy) (complianceChanged bool) {
	//used in case we also want to know not just the compliance state, but also whether the compliance changed or not.
	previous := plc.Status.ComplianceState
	if plc.Status.CompliancyDetails == nil {
		plc.Status.ComplianceState = policyv1alpha1.UnknownCompliancy
		return reflect.DeepEqual(previous, plc.Status.ComplianceState)
	}
	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
		plc.Status.ComplianceState = policyv1alpha1.UnknownCompliancy
		return reflect.DeepEqual(previous, plc.Status.ComplianceState)
	}
	if len(plc.Status.CompliancyDetails[plc.Name]) == 0 {
		plc.Status.ComplianceState = policyv1alpha1.UnknownCompliancy
		return reflect.DeepEqual(previous, plc.Status.ComplianceState)
	}
	plc.Status.ComplianceState = policyv1alpha1.Compliant
	for namespace, msgList := range plc.Status.CompliancyDetails[plc.Name] {
		if len(msgList) > 0 {
			violationNum := strings.Split(plc.Status.CompliancyDetails[plc.Name][namespace][0], " ")
			if len(violationNum) > 0 {
				if violationNum[0] != fmt.Sprint(0) {
					plc.Status.ComplianceState = policyv1alpha1.NonCompliant
				}
			}
		} else {
			return reflect.DeepEqual(previous, plc.Status.ComplianceState)
		}
	}
	if plc.Status.ComplianceState != policyv1alpha1.NonCompliant {
		plc.Status.ComplianceState = policyv1alpha1.Compliant
	}
	return reflect.DeepEqual(previous, plc.Status.ComplianceState)
}

func updatePolicyStatus(policies map[string]*policyv1alpha1.ConfigurationPolicy) (*policyv1alpha1.ConfigurationPolicy, error) {
	for _, instance := range policies { // policies is a map where: key = plc.Name, value = pointer to plc
		err := reconcilingAgent.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return instance, err
		}
		if EventOnParent != "no" {
			createParentPolicyEvent(instance)
		}
		if reconcilingAgent.recorder != nil {
			reconcilingAgent.recorder.Event(instance, "Normal", "Policy updated", fmt.Sprintf("Policy status is: %v", instance.Status.ComplianceState))
		}
	}
	return nil, nil
}

func setStatus(policy *policyv1alpha1.ConfigurationPolicy) {
	compliant := true
	for _, policyT := range policy.Spec.PolicyTemplates {

		if policyT.Status.ComplianceState == policyv1alpha1.NonCompliant {
			compliant = false
		}
	}
	if compliant {
		policy.Status.ComplianceState = policyv1alpha1.Compliant
	} else {
		policy.Status.ComplianceState = policyv1alpha1.NonCompliant
	}
}

func getContainerID(pod corev1.Pod, containerName string) string {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.ContainerID
		}
	}
	return ""
}

func handleRemovingPolicy(plc *policyv1alpha1.ConfigurationPolicy) {
	for k, v := range availablePolicies.PolicyMap {
		if v.Name == plc.Name {
			availablePolicies.RemoveObject(k)
		}
	}
}

func handleAddingPolicy(plc *policyv1alpha1.ConfigurationPolicy) error {
	allNamespaces, err := common.GetAllNamespaces()
	if err != nil {
		glog.Errorf("reason: error fetching the list of available namespaces, subject: K8s API server, namespace: all, according to policy: %v, additional-info: %v",
			plc.Name, err)
		return err
	}
	//clean up that policy from the existing namepsaces, in case the modification is in the namespace selector
	for _, ns := range allNamespaces {
		if policy, found := availablePolicies.GetObject(ns); found {
			if policy.Name == plc.Name {
				availablePolicies.RemoveObject(ns)
			}
		}
	}
	selectedNamespaces := common.GetSelectedNamespaces(plc.Spec.NamespaceSelector.Include, plc.Spec.NamespaceSelector.Exclude, allNamespaces)
	for _, ns := range selectedNamespaces {
		availablePolicies.AddObject(ns, plc)
	}
	return err
}

func newConfigurationPolicy(plc *policyv1alpha1.ConfigurationPolicy) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       plc.Kind,
			"apiVersion": plc.APIVersion,
			"metadata":   plc.GetObjectMeta(),
			"spec":       plc.Spec,
		},
	}
}

//=================================================================
//deleteExternalDependency in case the CRD was related to non-k8s resource
//nolint
func (r *ReconcileConfigurationPolicy) deleteExternalDependency(instance *policyv1alpha1.ConfigurationPolicy) error {
	glog.V(0).Infof("reason: CRD deletion, subject: policy/%v, namespace: %v, according to policy: none, additional-info: none\n",
		instance.Name,
		instance.Namespace)
	// Ensure that delete implementation is idempotent and safe to invoke
	// multiple types for same object.
	return nil
}

//=================================================================
// Helper function to join strings
func join(strs ...string) string {
	var result string
	if strs[0] == "" {
		return strs[len(strs)-1]
	}
	for _, str := range strs {
		result += str
	}
	return result
}

//=================================================================
// Helper functions to check if a string exists in a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

//=================================================================
// Helper functions to remove a string from a slice of strings.
func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

//=================================================================
// Helper functions that pretty prints a map
func printMap(myMap map[string]*policyv1alpha1.ConfigurationPolicy) {
	if len(myMap) == 0 {
		fmt.Println("Waiting for policies to be available for processing... ")
		return
	}
	fmt.Println("Available policies in namespaces: ")

	for k, v := range myMap {
		fmt.Printf("namespace = %v; policy = %v \n", k, v.Name)
	}
}

func createParentPolicyEvent(instance *policyv1alpha1.ConfigurationPolicy) {
	if len(instance.OwnerReferences) == 0 {
		return //there is nothing to do, since no owner is set
	}
	// we are making an assumption that the GRC policy has a single owner, or we chose the first owner in the list
	if string(instance.OwnerReferences[0].UID) == "" {
		return //there is nothing to do, since no owner UID is set
	}

	parentPlc := createParentPolicy(instance)

	reconcilingAgent.recorder.Event(&parentPlc,
		corev1.EventTypeNormal,
		fmt.Sprintf("policy: %s/%s", instance.Namespace, instance.Name),
		convertPolicyStatusToString(instance))
}

func createParentPolicy(instance *policyv1alpha1.ConfigurationPolicy) policyv1alpha1.ConfigurationPolicy {
	ns := common.ExtractNamespaceLabel(instance)
	if ns == "" {
		ns = NamespaceWatched
	}
	plc := policyv1alpha1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.OwnerReferences[0].Name,
			Namespace: ns, // we are making an assumption here that the parent policy is in the watched-namespace passed as flag
			UID:       instance.OwnerReferences[0].UID,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigurationPolicy",
			APIVersion: " policies.ibm.com/v1alpha1",
		},
	}
	return plc
}

//=================================================================
// convertPolicyStatusToString to be able to pass the status as event
func convertPolicyStatusToString(plc *policyv1alpha1.ConfigurationPolicy) (results string) {
	result := "ComplianceState is still undetermined"
	if plc.Status.ComplianceState == "" {
		return result
	}
	result = string(plc.Status.ComplianceState)

	if plc.Status.CompliancyDetails == nil {
		return result
	}
	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
		return result
	}
	for _, v := range plc.Status.CompliancyDetails[plc.Name] {
		result += fmt.Sprintf("; %s", strings.Join(v, ", "))
	}
	return result
}

func recoverFlow() {
	if r := recover(); r != nil {
		fmt.Println("ALERT!!!! -> recovered from ", r)
	}
}
