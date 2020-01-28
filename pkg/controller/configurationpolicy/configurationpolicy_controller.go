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
	"bytes"
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
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
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

var mustNotHaveRole = make(map[string]roleOrigin)
var mustHaveRole = make(map[string]roleOrigin)
var mustOnlyHaveRole = make(map[string]roleOrigin)

//UpdatePolicyMap used to keep track of policies to be updated
var UpdatePolicyMap = make(map[string]*policyv1alpha1.ConfigurationPolicy)

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

var restClient *rest.RESTClient

var syncAlertTargets bool

var CemWebhookURL string

var clusterName string

//Mx for making the map thread safe
var Mx sync.RWMutex

//MxUpdateMap for making the map thread safe
var MxUpdateMap sync.RWMutex

// KubeClient a k8s client used for k8s native resources
var KubeClient *kubernetes.Interface

var reconcilingAgent *ReconcileConfigurationPolicy

// NamespaceWatched defines which namespace we can watch for the GRC policies and ignore others
var NamespaceWatched string

// EventOnParent specifies if we also want to send events to the parent policy. Available options are yes/no/ifpresent
var EventOnParent string

// PrometheusAddr port addr for prom metrics
var PrometheusAddr string

type roleOrigin struct {
	roleTemplate *policyv1alpha1.RoleTemplate
	policy       *policyv1alpha1.ConfigurationPolicy
	namespace    string
	//TODO add flatRole representation here to save on calculation
}

type roleCompareResult struct {
	roleName     string
	missingKeys  map[string]map[string]bool
	missingVerbs map[string]map[string]bool

	AdditionalKeys map[string]map[string]bool
	AddtionalVerbs map[string]map[string]bool
}

// PassthruCodecFactory provides methods for retrieving "DirectCodec"s, which do not do conversion.
type PassthruCodecFactory struct {
	serializer.CodecFactory
}

// PluralScheme extends scheme with plurals
type PluralScheme struct {
	Scheme *runtime.Scheme

	// plurals for group, version and kinds
	plurals map[schema.GroupVersionKind]string
}

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
	config.GroupVersion = &schema.GroupVersion{Group: "policies.ibm.com", Version: "v1alpha1"}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = PassthruCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme.Scheme)}
	client, err := rest.RESTClientFor(config)
	if err != nil {
		glog.Fatalf("error creating REST client: %s", err)
	}
	restClient = client

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

	relevantNamespaces := getPolicyNamespaces(instance)
	//for each namespace, check the compliance against the policy:
	for _, ns := range relevantNamespaces {
		handlePolicyPerNamespace(ns, instance, instance.ObjectMeta.DeletionTimestamp.IsZero())
	}

	glog.V(3).Infof("reason: successful processing, subject: policy/%v, namespace: %v, according to policy: %v, additional-info: none",
		instance.Name, instance.Namespace, instance.Name)

	// Pod already exists - don't requeue
	// reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	return reconcile.Result{}, nil
}

func handlePolicyPerNamespace(namespace string, plc *policyv1alpha1.ConfigurationPolicy, added bool) {
	//for each template we should handle each Kind of objects. I.e. iterate on all the items
	//of a given Kind
	glog.V(6).Infof("Policy: %v, namespace: %v\n", plc.Name, namespace)

	//add the role-namespace to a map fo musthave, mustnothave, or mustonylhave
	for _, roleT := range plc.Spec.RoleTemplates {

		roleN := []string{roleT.Name, namespace} //TODO the name can contain a wildcard, so I must handle that
		roleNamespace := strings.Join(roleN, "-")

		switch strings.ToLower(string(roleT.ComplianceType)) {
		case "musthave":
			if added {
				//add the role
				Mx.Lock()
				mustHaveRole[roleNamespace] = roleOrigin{
					roleT,
					plc,
					namespace,
				}
				Mx.Unlock()
				glog.V(4).Infof("the role: %s is added to the 'mustHave' list\n", roleNamespace)
			} else {
				Mx.Lock()
				delete(mustHaveRole, roleNamespace)
				Mx.Unlock()
			}

		case "mustnothave":
			if added {
				//add the role
				Mx.Lock()
				mustNotHaveRole[roleNamespace] = roleOrigin{
					roleT,
					plc,
					namespace,
				}
				Mx.Unlock()
				glog.V(4).Infof("the role: %s is added to the 'mustNotHave' list\n", roleNamespace)
			} else {
				Mx.Lock()
				delete(mustNotHaveRole, roleNamespace)
				Mx.Unlock()
			}

		case "mustonlyhave":
			if added {
				//add the role
				Mx.Lock()
				mustOnlyHaveRole[roleNamespace] = roleOrigin{
					roleT,
					plc,
					namespace,
				}
				Mx.Unlock()
				glog.V(4).Infof("the role: %s is added to the 'mustOnlyHave' list\n", roleNamespace)
			} else {
				Mx.Lock()
				delete(mustOnlyHaveRole, roleNamespace)
				Mx.Unlock()
			}

		}
	}
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

			Mx.Lock()
			for _, rtValue := range mustHaveRole {
				handleMustHaveRole(rtValue)
			}
			Mx.Unlock() //giving the other GO-routine a chance to advance, and modify the map if needed
			Mx.Lock()
			for _, rtValue := range mustNotHaveRole {
				handleMustNotHaveRole(rtValue)
			}
			Mx.Unlock()
			Mx.Lock()
			handleObjectTemplates(policy)
			Mx.Unlock()

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

func handleMustNotHaveRole(rtValue roleOrigin) {
	updateNeeded := false
	//Get the list of roles that satisfy the label:
	opt := &metav1.ListOptions{}
	if rtValue.roleTemplate.Selector != nil {
		if rtValue.roleTemplate.Selector.MatchLabels != nil {
			lbl := createKeyValuePairs(rtValue.roleTemplate.Selector.MatchLabels)
			if lbl == "" {
				opt = &metav1.ListOptions{LabelSelector: lbl}
			}
		}
	}
	roleList, err := (*KubeClient).RbacV1().Roles(rtValue.namespace).List(*opt) //namespace scoped list
	if err != nil {
		rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.UnknownCompliancy
		rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("accessError", rtValue, err, rtValue.roleTemplate.Status.Conditions, "")
		err = updatePolicy(rtValue.policy, 0)
		if err != nil {
			glog.Errorf("Error update policy %v, the error is: %v", rtValue.policy.Name, err)
		}
		glog.Errorf("Error fetching the list Rbac roles from K8s Api-server, the error is: %v", err)
		return
	}

	//I have the list of filtered roles by label, now I need to filter by name pattern
	roleNames := getRoleNames(roleList.Items)
	foundRoles := common.FindPattern(rtValue.roleTemplate.Name, roleNames)

	if len(foundRoles) > 0 { //I need to do delete those roles
		for _, fRole := range foundRoles {
			opt := &metav1.DeleteOptions{}
			if (strings.ToLower(string(rtValue.policy.Spec.RemediationAction)) == strings.ToLower(string(policyv1alpha1.Enforce))) &&
				(strings.ToLower(string(rtValue.roleTemplate.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustNotHave))) {
				err = (*KubeClient).RbacV1().Roles(rtValue.namespace).Delete(fRole, opt)
				if err != nil {
					glog.Errorf("Error deleting role `%v` in namespace `%v` according to policy `%v`, the error is: %v", fRole, rtValue.namespace, rtValue.policy.Name, err)
					rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("failedDeletingExtraRole", rtValue, err, rtValue.roleTemplate.Status.Conditions, fRole)
					updateNeeded = true
				} else {
					glog.V(2).Infof("Deleted role `%v` in namespace `%v` according to policy `%v`", fRole, rtValue.namespace, rtValue.policy.Name)
					rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("deletedExtraRole", rtValue, err, rtValue.roleTemplate.Status.Conditions, fRole)
					rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.Compliant
					updateNeeded = true
				}
			} else if strings.ToLower(string(rtValue.roleTemplate.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustNotHave)) { //inform only
				if rtValue.roleTemplate.Status.ComplianceState != policyv1alpha1.NonCompliant {
					rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.NonCompliant
					updateNeeded = true
				}
				rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("ExtraRole", rtValue, err, rtValue.roleTemplate.Status.Conditions, fRole)
			}
		}
	} else { // role doesn't exists
		if rtValue.roleTemplate.Status.ComplianceState != policyv1alpha1.Compliant {
			rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.Compliant
			rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("notExists", rtValue, err, rtValue.roleTemplate.Status.Conditions, "")
			updateNeeded = true
		}
	}

	if updateNeeded {
		if rtValue.roleTemplate.Status.ComplianceState == policyv1alpha1.NonCompliant {
			recorder.Event(rtValue.policy, "Warning", fmt.Sprintf("policy: %s/%s", rtValue.policy.GetName(), rtValue.roleTemplate.ObjectMeta.GetName()), fmt.Sprintf("%s; %s", rtValue.roleTemplate.Status.ComplianceState, rtValue.roleTemplate.Status.Conditions[0].Message))
		} else {
			recorder.Event(rtValue.policy, "Normal", fmt.Sprintf("policy: %s/%s", rtValue.policy.GetName(), rtValue.roleTemplate.ObjectMeta.GetName()), fmt.Sprintf("%s; %s", rtValue.roleTemplate.Status.ComplianceState, rtValue.roleTemplate.Status.Conditions[0].Message))
		}

		err = updatePolicy(rtValue.policy, 0)
		if err != nil {
			glog.Errorf("Error update policy %v, the error is: %v", rtValue.policy.Name, err)
		} else {
			glog.V(6).Infof("Updated the status in policy %v", rtValue.policy.Name)
		}
	}

}

func handleMustHaveRole(rtValue roleOrigin) {
	var lbl string
	updateNeeded := false
	//Get the list of roles that satisfy the label:
	opt := &metav1.ListOptions{}
	if rtValue.roleTemplate.Selector != nil {
		if rtValue.roleTemplate.Selector.MatchLabels != nil {
			lbl = createKeyValuePairs(rtValue.roleTemplate.Selector.MatchLabels)
			if lbl == "" {
				opt = &metav1.ListOptions{LabelSelector: lbl}
			}
		}
	}

	roleList, err := (*KubeClient).RbacV1().Roles(rtValue.namespace).List(*opt) //namespace scoped list
	if err != nil {

		rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.UnknownCompliancy
		rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("accessError", rtValue, err, rtValue.roleTemplate.Status.Conditions, "")
		err = updatePolicy(rtValue.policy, 0)
		if err != nil {
			glog.Errorf("Error update policy %v, the error is: %v", rtValue.policy.Name, err)
		}
		glog.Errorf("Error fetching the list Rbac roles from K8s Api-server, the error is: %v", err)
		return
	}
	rMap := listToRoleMap(roleList.Items)
	//I have the list of filtered roles by label, now I need to filter by name pattern
	roleNames := getRoleNames(roleList.Items)
	foundRoles := common.FindPattern(rtValue.roleTemplate.Name, roleNames)
	if !strings.Contains(rtValue.roleTemplate.Name, "*") && len(foundRoles) == 0 {
		//it is an exact roles name that must exit, however it was not found => we must create it.
		if strings.ToLower(string(rtValue.policy.Spec.RemediationAction)) == strings.ToLower(string(policyv1alpha1.Enforce)) {
			role := buildRole(rtValue)
			_, err = (*KubeClient).RbacV1().Roles(rtValue.namespace).Create(role)
			if err != nil {

				rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.NonCompliant

				rtValue.policy.Status.ComplianceState = policyv1alpha1.NonCompliant
				rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("createRoleError", rtValue, err, rtValue.roleTemplate.Status.Conditions, "")
				updateNeeded = true
				glog.V(2).Infof("the Rbac role %v in namespace %v from policy %v, was not found among the role list filtered by labels: %v", role.Name,
					role.Namespace, rtValue.policy.Name, lbl)
				glog.Errorf("Error creating the Rbac role %v in namespace %v from policy %v, the error is: %v", role.Name,
					role.Namespace, rtValue.policy.Name, err)

			} else {
				rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.Compliant
				rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("createdRole", rtValue, err, rtValue.roleTemplate.Status.Conditions, "")
				updateNeeded = true
				glog.V(2).Infof("created the Rbac role %v in namespace %v from policy %v", role.Name,
					role.Namespace, rtValue.policy.Name)
			}

		} else { //it is inform only
			rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.NonCompliant

			rtValue.policy.Status.ComplianceState = policyv1alpha1.NonCompliant
			rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("missingRole", rtValue, err, rtValue.roleTemplate.Status.Conditions, rtValue.roleTemplate.Name)
			updateNeeded = true
			glog.V(2).Infof("the Rbac role %v in namespace %v is missing! it should be created according to role template %v in policy %v", rtValue.roleTemplate.Name,
				rtValue.namespace, rtValue.roleTemplate.Name, rtValue.policy.Name)
		}
	} else if len(foundRoles) > 0 { //I need to do a deep comparison after flattening

		for _, fRole := range foundRoles {
			roleN := []string{fRole, rtValue.namespace}
			roleNamespace := strings.Join(roleN, "-")
			actualRole := rMap[roleNamespace]
			actualRoleMap := flattenRole(rMap[roleNamespace])
			roleTMap := flattenRoleTemplate(*rtValue.roleTemplate)
			match, res := deepCompareRoleTtoRole(roleTMap, actualRoleMap)

			message := prettyPrint(*res, rtValue.roleTemplate.Name)
			if !match { // role permission doesn't match
				if strings.ToLower(string(rtValue.policy.Spec.RemediationAction)) == strings.ToLower(string(policyv1alpha1.Enforce)) {
					//I need to update the actual role

					copyActualRole := actualRole.DeepCopy()

					copyActualRole.Rules = getdesiredRules(*rtValue.roleTemplate)

					_, err = (*KubeClient).RbacV1().Roles(rtValue.namespace).Update(copyActualRole)
					if err != nil {
						rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.NonCompliant

						rtValue.policy.Status.ComplianceState = policyv1alpha1.NonCompliant
						rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("mismatch", rtValue, err, rtValue.roleTemplate.Status.Conditions, message)
						updateNeeded = true
						glog.V(2).Infof("Error updating the Rbac role %v in namespace %v from policy %v, the error is: %v", copyActualRole.Name,
							copyActualRole.Namespace, rtValue.policy.Name, err)

					} else {
						rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.Compliant
						rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("mismatchFixed", rtValue, err, rtValue.roleTemplate.Status.Conditions, message)
						updateNeeded = true

						glog.V(2).Infof("Role updated %v to comply to policy %v", copyActualRole.Name, rtValue.policy.Name)

					}

				} else { //it is inform only
					if rtValue.roleTemplate.Status.ComplianceState != policyv1alpha1.NonCompliant {
						rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.NonCompliant
						updateNeeded = true
					}
					if rtValue.policy.Status.ComplianceState != policyv1alpha1.NonCompliant {
						rtValue.policy.Status.ComplianceState = policyv1alpha1.NonCompliant
						updateNeeded = true
					}

					rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("mismatch", rtValue, err, rtValue.roleTemplate.Status.Conditions, message)
					glog.V(2).Infof("INFORM: the Rbac role %v in namespace %v is Patched! it is updated according to role template %v in policy %v", actualRole.Name,
						rtValue.namespace, rtValue.roleTemplate.Name, rtValue.policy.Name)

				}
			} else { // role permission matches
				if rtValue.roleTemplate.Status.ComplianceState != policyv1alpha1.Compliant {
					glog.V(2).Infof("Role %s exists according policy %s", rtValue.roleTemplate.Name, rtValue.policy.Name)
					rtValue.roleTemplate.Status.ComplianceState = policyv1alpha1.Compliant
					rtValue.roleTemplate.Status.Conditions = createRoleTemplateCondition("match", rtValue, err, rtValue.roleTemplate.Status.Conditions, message)
					updateNeeded = true
				}
			}
		}
	}
	if updateNeeded {
		if rtValue.roleTemplate.Status.ComplianceState == policyv1alpha1.NonCompliant {
			recorder.Event(rtValue.policy, "Warning", fmt.Sprintf("policy: %s/%s", rtValue.policy.GetName(), rtValue.roleTemplate.ObjectMeta.GetName()), fmt.Sprintf("%s; %s", rtValue.roleTemplate.Status.ComplianceState, rtValue.roleTemplate.Status.Conditions[0].Message))
		} else {
			recorder.Event(rtValue.policy, "Normal", fmt.Sprintf("policy: %s/%s", rtValue.policy.GetName(), rtValue.roleTemplate.ObjectMeta.GetName()), fmt.Sprintf("%s; %s", rtValue.roleTemplate.Status.ComplianceState, rtValue.roleTemplate.Status.Conditions[0].Message))
		}

		if _, ok := UpdatePolicyMap[rtValue.policy.Name]; !ok {
			MxUpdateMap.Lock()
			UpdatePolicyMap[rtValue.policy.Name] = rtValue.policy
			MxUpdateMap.Unlock()
		}
		updatePolicy(rtValue.policy, 0)
	}
}

func handleObjectTemplates(plc *policyv1alpha1.ConfigurationPolicy) {
	if reflect.DeepEqual(plc.Labels["ignore"], "true") {
		plc.Status = policyv1alpha1.ConfigurationPolicyStatus{
			ComplianceState:   policyv1alpha1.UnknownCompliancy,
			CompliancyDetails: map[string]map[string][]string{},
		}
		return
	}
	relevantNamespaces := getPolicyNamespaces(plc)
	for indx, objectT := range plc.Spec.ObjectTemplates {
		for _, ns := range relevantNamespaces {
			glog.V(5).Infof("Handling Object template [%v] from Policy `%v` in namespace `%v`", indx, plc.Name, ns)
			handleObjects(objectT, ns, indx, plc, KubeClient, config, recorder)
		}
	}
}

func handleObjects(objectT *policyv1alpha1.ObjectTemplate, namespace string, index int, policy *policyv1alpha1.ConfigurationPolicy, clientset *kubernetes.Interface, config *rest.Config, recorder record.EventRecorder) {
	updateNeeded := false
	namespaced := true
	dd := (*clientset).Discovery()
	apigroups, err := restmapper.GetAPIGroupResources(dd)
	if err != nil {
		glog.Fatal(err)
	}

	restmapper := restmapper.NewDiscoveryRESTMapper(apigroups)
	//ext := runtime.RawExtension{}
	ext := objectT.ObjectDefinition
	glog.V(9).Infof("reading raw object: %v", string(ext.Raw))
	versions := &runtime.VersionedObjects{}
	_, gvk, err := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, versions)
	if err != nil {
		decodeErr := fmt.Sprintf("Decoding error, please check your policy file! Aborting handling the object template at index [%v] in policy `%v` with error = `%v`", index, policy.Name, err)
		glog.Errorf(decodeErr)
		//policy.Status.Message = decodeErr
		updatePolicy(policy, 0)
		return
	}
	mapping, err := restmapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	mappingErrMsg := ""
	if err != nil {
		prefix := "no matches for kind \""
		startIdx := strings.Index(err.Error(), prefix)
		if startIdx == -1 {
			glog.Errorf("unidentified mapping error from raw object: `%v`", err)
		} else {
			afterPrefix := err.Error()[(startIdx + len(prefix)):len(err.Error())]
			kind := afterPrefix[0:(strings.Index(afterPrefix, "\" "))]
			mappingErrMsg = "couldn't find mapping resource with kind " + kind + ", please check if you have CRD deployed"
			glog.Errorf(mappingErrMsg)
		}
		errMsg := err.Error()
		if mappingErrMsg != "" {
			errMsg = mappingErrMsg
			cond := &policyv1alpha1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation error",
				Message:            mappingErrMsg,
			}
			if objectT.Status.ComplianceState != policyv1alpha1.NonCompliant {
				updateNeeded = true
			}
			objectT.Status.ComplianceState = policyv1alpha1.NonCompliant

			if !checkMessageSimilarity(objectT, cond) {
				conditions := AppendCondition(objectT.Status.Conditions, cond, gvk.GroupKind().Kind, false)
				objectT.Status.Conditions = conditions
				updateNeeded = true
			}
		}
		if updateNeeded {
			recorder.Event(policy, "Warning", fmt.Sprintf("policy: %s/%s", policy.GetName(), "policy"), errMsg)
			//addForUpdate(policy, 0, &dclient, &gvr)
		}
		return
	}
	glog.V(9).Infof("mapping found from raw object: %v", mapping)

	restconfig := config
	restconfig.GroupVersion = &schema.GroupVersion{
		Group:   mapping.GroupVersionKind.Group,
		Version: mapping.GroupVersionKind.Version,
	}
	dclient, err := dynamic.NewForConfig(restconfig)
	if err != nil {
		glog.Fatal(err)
	}

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

	//namespace := "default"
	name := ""
	if md, ok := unstruct.Object["metadata"]; ok {

		metadata := md.(map[string]interface{})
		if objectName, ok := metadata["name"]; ok {
			//glog.V(9).Infof("metadata[namespace] exists")
			name = objectName.(string)
		}
		/*
			if objectns, ok := metadata["namespace"]; ok {
				//glog.V(9).Infof("metadata[namespace] exists")
				namespace = objectns.(string)
			}
		*/

	}
	exists := objectExists(namespaced, namespace, name, rsrc, unstruct, dclient)

	if !exists && strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustHave)) {
		//it is a musthave and it does not exist, so it must be created
		updateNeeded, err = handleMissingMustHave(objectT, policy.Spec.RemediationAction, namespaced, namespace, name, rsrc, unstruct, dclient)
		if err != nil {
			glog.Errorf("error handling a missing object `%v` that is a must have according to policy `%v`", name, policy.Name)
		}
	}

	if exists && strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustNotHave)) {
		//it is a mustnothave but it exist, so it must be deleted
		updateNeeded, err = handleExistsMustNotHave(objectT, policy.Spec.RemediationAction, namespaced, namespace, name, rsrc, dclient)
		if err != nil {
			glog.Errorf("error handling a existing object `%v` that is a must NOT have according to policy `%v`", name, policy.Name)
		}
	}

	if !exists && strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustNotHave)) {
		//it is a must not have and it does not exist, so it is compliant
		updateNeeded = handleMissingMustNotHave(objectT, name, rsrc)
	}

	if exists && strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustHave)) {
		//it is a must have and it does exist, so it is compliant
		updateNeeded = handleExistsMustHave(objectT, name, rsrc)
	}

	if exists {
		updated, msg := updateTemplate(namespaced, namespace, name, rsrc, unstruct, dclient, unstruct.Object["kind"].(string))
		if !updated && msg != "" {
			cond := &policyv1alpha1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s update template error",
				Message:            msg,
			}
			if objectT.Status.ComplianceState != policyv1alpha1.NonCompliant {
				updateNeeded = true
			}
			objectT.Status.ComplianceState = policyv1alpha1.NonCompliant

			if !checkMessageSimilarity(objectT, cond) {
				conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, false)
				objectT.Status.Conditions = conditions
				updateNeeded = true
			}
			glog.Errorf(msg)
		}
	}

	if updateNeeded {
		eventType := "Normal"
		if objectT.Status.ComplianceState == policyv1alpha1.NonCompliant {
			eventType = "Warning"
		}
		recorder.Event(policy, eventType, fmt.Sprintf("policy: %s/%s", policy.GetName(), name), fmt.Sprintf("%s; %s", objectT.Status.ComplianceState, objectT.Status.Conditions[0].Message))
		addForUpdate(policy, 0, &dclient, &rsrc)
	}
}

func handleMissingMustNotHave(objectT *policyv1alpha1.ObjectTemplate, name string, rsrc schema.GroupVersionResource) bool {
	glog.V(7).Infof("entering `does not exists` & ` must not have`")
	var cond *policyv1alpha1.Condition
	var update bool
	message := fmt.Sprintf("%v `%v` is missing as it should be, therefore the this Object template is compliant", rsrc.Resource, name)
	cond = &policyv1alpha1.Condition{
		Type:               "succeeded",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "K8s must `not` have object already missing",
		Message:            message,
	}
	if objectT.Status.ComplianceState != policyv1alpha1.Compliant {
		update = true
	}
	objectT.Status.ComplianceState = policyv1alpha1.Compliant

	if !checkMessageSimilarity(objectT, cond) {
		conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, true)
		objectT.Status.Conditions = conditions
		update = true
	}
	return update
}

func handleExistsMustHave(objectT *policyv1alpha1.ObjectTemplate, name string, rsrc schema.GroupVersionResource) (updateNeeded bool) {
	var cond *policyv1alpha1.Condition
	var update bool
	message := fmt.Sprintf("%v `%v` exists as it should be, therefore the this Object template is compliant", rsrc.Resource, name)
	cond = &policyv1alpha1.Condition{
		Type:               "notification",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "K8s `must have` object already exists",
		Message:            message,
	}
	if objectT.Status.ComplianceState != policyv1alpha1.Compliant {
		update = true
	}
	objectT.Status.ComplianceState = policyv1alpha1.Compliant

	if !checkMessageSimilarity(objectT, cond) {
		conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, true) //true = resolved
		objectT.Status.Conditions = conditions
		update = true
	}
	return update
}

func handleExistsMustNotHave(objectT *policyv1alpha1.ObjectTemplate, action policyv1alpha1.RemediationAction, namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, dclient dynamic.Interface) (result bool, erro error) {
	glog.V(7).Infof("entering `exists` & ` must not have`")
	var cond *policyv1alpha1.Condition
	var update, deleted bool
	var err error

	if strings.ToLower(string(action)) == strings.ToLower(string(policyv1alpha1.Enforce)) {
		if deleted, err = deleteObject(namespaced, namespace, name, rsrc, dclient); !deleted {
			message := fmt.Sprintf("%v `%v` exists, and cannot be deleted, reason: `%v`", rsrc.Resource, name, err)
			cond = &policyv1alpha1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s deletion error",
				Message:            message,
			}
			if objectT.Status.ComplianceState != policyv1alpha1.NonCompliant {
				update = true
			}
			objectT.Status.ComplianceState = policyv1alpha1.NonCompliant

			if !checkMessageSimilarity(objectT, cond) {
				conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, false)
				objectT.Status.Conditions = conditions
				update = true
			}
		} else { //deleted successfully
			message := fmt.Sprintf("%v `%v` existed, and was deleted successfully", rsrc.Resource, name)
			cond = &policyv1alpha1.Condition{
				Type:               "notification",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s deletion success",
				Message:            message,
			}
			if objectT.Status.ComplianceState != policyv1alpha1.Compliant {
				update = true
			}
			objectT.Status.ComplianceState = policyv1alpha1.Compliant
			if !checkMessageSimilarity(objectT, cond) {
				conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, true)
				objectT.Status.Conditions = conditions
				update = true
			}
		}
	} else { //inform
		message := fmt.Sprintf("%v `%v` exists, and should be deleted", rsrc.Resource, name)
		cond = &policyv1alpha1.Condition{
			Type:               "notification",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s has a must `not` have object",
			Message:            message,
		}
		if objectT.Status.ComplianceState != policyv1alpha1.NonCompliant {
			update = true
		}
		objectT.Status.ComplianceState = policyv1alpha1.NonCompliant
		if !checkMessageSimilarity(objectT, cond) {
			conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, false)
			objectT.Status.Conditions = conditions
			return true, err
		}

	}
	return update, err
}

func handleMissingMustHave(objectT *policyv1alpha1.ObjectTemplate, action policyv1alpha1.RemediationAction, namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface) (result bool, erro error) {
	glog.V(7).Infof("entering `does not exists` & ` must have`")

	var update, created bool
	var err error
	var cond *policyv1alpha1.Condition
	if strings.ToLower(string(action)) == strings.ToLower(string(policyv1alpha1.Enforce)) {
		if created, err = createObject(namespaced, namespace, name, rsrc, unstruct, dclient, nil); !created {
			message := fmt.Sprintf("%v `%v` is missing, and cannot be created, reason: `%v`", rsrc.Resource, name, err)
			cond = &policyv1alpha1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation error",
				Message:            message,
			}
			if objectT.Status.ComplianceState != policyv1alpha1.NonCompliant {
				update = true
			}
			objectT.Status.ComplianceState = policyv1alpha1.NonCompliant

			if !checkMessageSimilarity(objectT, cond) {
				objectT.Status.Conditions = AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, false)
				update = true
			}
		} else { //created successfully
			glog.V(8).Infof("entering `%v` created successfully", name)
			message := fmt.Sprintf("%v `%v` was missing, and was created successfully", rsrc.Resource, name)
			cond = &policyv1alpha1.Condition{
				Type:               "notification",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation success",
				Message:            message,
			}
			if objectT.Status.ComplianceState != policyv1alpha1.Compliant {
				update = true
			}
			objectT.Status.ComplianceState = policyv1alpha1.Compliant

			if !checkMessageSimilarity(objectT, cond) {
				conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, true)
				objectT.Status.Conditions = conditions
				update = true
			}
		}
	} else { //inform
		message := fmt.Sprintf("%v `%v` is missing, and should be created", rsrc.Resource, name)
		cond = &policyv1alpha1.Condition{
			Type:               "violation",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s missing a must have object",
			Message:            message,
		}
		if objectT.Status.ComplianceState != policyv1alpha1.NonCompliant {
			update = true
		}
		objectT.Status.ComplianceState = policyv1alpha1.NonCompliant

		if !checkMessageSimilarity(objectT, cond) {
			conditions := AppendCondition(objectT.Status.Conditions, cond, rsrc.Resource, false)
			objectT.Status.Conditions = conditions
			update = true
		}
	}
	return update, err
}

func deepCompareRoleTtoRole(desired map[string]map[string]map[string]bool, actual map[string]map[string]bool) (match bool, res *roleCompareResult) {
	/*TODO: consider the ["*"] case. e.g.
		apiGroups: ["*"]
	  	resources: ["*"]
	  	verbs: ["*"]
	*/
	matched := true
	compRes := &roleCompareResult{}
	//result := []string{}
	if desired["musthave"] != nil && len(desired["musthave"]) > 0 {
		match, compRes = compareRoleMustHave(desired["musthave"], actual, compRes)
		if !match {
			matched = false
		}
	}
	if desired["mustnothave"] != nil && len(desired["mustnothave"]) > 0 {
		match, compRes = compareRoleMustNotHave(desired["mustnothave"], actual, compRes)
		if !match {
			matched = false
		}
	}
	if desired["mustonlyhave"] != nil && len(desired["mustonlyhave"]) > 0 {
		match, compRes = compareRoleMustOnlyHave(desired["mustonlyhave"], actual, compRes)
		if !match {
			matched = false
		}
	}

	return matched, compRes
}

func compareRoleMustHave(desired map[string]map[string]bool, actual map[string]map[string]bool, compRes *roleCompareResult) (match bool, res *roleCompareResult) {
	match = true
	for key, desG := range desired {
		// must have it means that's the minimum set of rules the role must have, if a rule has more verbes that's ok
		if _, ok := actual[key]; ok {
			for keyVerb := range desG {
				if _, ok := actual[key][keyVerb]; ok {
					// the verb in desG exists in actualresults
				} else {
					//glog.V(2).Infof("INFORM:The verb %s is not found in actual, when looking into key %s", keyVerb, key)
					if compRes.missingVerbs == nil { //initialize the map
						compRes.missingVerbs = make(map[string]map[string]bool)
						if compRes.missingVerbs[key] == nil {
							compRes.missingVerbs[key] = make(map[string]bool)
						}
						compRes.missingVerbs[key][keyVerb] = false
					} else {
						if compRes.missingVerbs[key] == nil {
							compRes.missingVerbs[key] = make(map[string]bool)
						}
						compRes.missingVerbs[key][keyVerb] = false
					}
					match = false
				}
			}
		} else {
			if compRes.missingKeys == nil { //initialize the map
				compRes.missingKeys = make(map[string]map[string]bool)
				if compRes.missingKeys[key] == nil {
					compRes.missingKeys[key] = make(map[string]bool)
				}
				compRes.missingKeys[key] = desired[key]
			} else {
				if compRes.missingKeys[key] == nil {
					compRes.missingKeys[key] = make(map[string]bool)
				}
				compRes.missingKeys[key] = desired[key]
			}
			match = false
		}
	}
	return match, compRes
}

func compareRoleMustNotHave(desired map[string]map[string]bool, actual map[string]map[string]bool, compRes *roleCompareResult) (match bool, res *roleCompareResult) {
	match = true
	for key, desG := range desired {
		// must not have it means that's the set of rules the role NOT must have, if a rule has different verbes that's ok
		if _, ok := actual[key]; ok {
			glog.V(2).Infof("The Key %s is found in actual, and has a value of: %v ", key, actual)
			for keyVerb := range desG {
				if _, ok := actual[key][keyVerb]; ok {
					// the verb in desG exists in actualresults
					if compRes.AddtionalVerbs == nil { //initialize the map
						compRes.AddtionalVerbs = make(map[string]map[string]bool)
						if compRes.AddtionalVerbs[key] == nil {
							compRes.AddtionalVerbs[key] = make(map[string]bool)
						}
						compRes.AddtionalVerbs[key][keyVerb] = false
					} else {
						if compRes.AddtionalVerbs[key] == nil {
							compRes.AddtionalVerbs[key] = make(map[string]bool)
						}
						compRes.AddtionalVerbs[key][keyVerb] = false
					}
					match = false
				}
			}
		}
	}
	return match, compRes
}

func compareRoleMustOnlyHave(desired map[string]map[string]bool, actual map[string]map[string]bool, compRes *roleCompareResult) (match bool, res *roleCompareResult) {
	match = true
	for key, desG := range desired {
		// must have it means that's the minimum set of rules the role must have, if a rule has more verbes that's ok
		if _, ok := actual[key]; ok {
			for keyVerb := range desG {
				if _, ok := actual[key][keyVerb]; ok {
					// the verb in desG exists in actualresults
				} else {
					if compRes.missingVerbs == nil { //initialize the map
						compRes.missingVerbs = make(map[string]map[string]bool)
						if compRes.missingVerbs[key] == nil {
							compRes.missingVerbs[key] = make(map[string]bool)
						}
						compRes.missingVerbs[key][keyVerb] = false
					} else {
						if compRes.missingVerbs[key] == nil {
							compRes.missingVerbs[key] = make(map[string]bool)
						}
						compRes.missingVerbs[key][keyVerb] = false
					}
					match = false
				}
			}
		} else {
			if compRes.missingKeys == nil { //initialize the map
				compRes.missingKeys = make(map[string]map[string]bool)
				if compRes.missingKeys[key] == nil {
					compRes.missingKeys[key] = make(map[string]bool)
				}
				compRes.missingKeys[key] = desired[key]
			} else {
				if compRes.missingKeys[key] == nil {
					compRes.missingKeys[key] = make(map[string]bool)
				}
				compRes.missingKeys[key] = desired[key]
			}
			match = false
		}
	}

	// now we reverse the order

	for key, actl := range actual {
		// must have it means that's the minimum set of rules the role must have, if a rule has more verbes that's ok
		if _, ok := desired[key]; ok {
			for keyVerb := range actl {
				if _, ok := desired[key][keyVerb]; ok {
					// the verb in desG exists in actualresults
				} else {
					//glog.V(2).Infof("INFORM:The verb %s is not found in actual, when looking into key %s", keyVerb, key)
					if compRes.AddtionalVerbs == nil { //initialize the map
						compRes.AddtionalVerbs = make(map[string]map[string]bool)
						if compRes.AddtionalVerbs[key] == nil {
							compRes.AddtionalVerbs[key] = make(map[string]bool)
						}
						compRes.AddtionalVerbs[key][keyVerb] = false
					} else {
						if compRes.AddtionalVerbs[key] == nil {
							compRes.AddtionalVerbs[key] = make(map[string]bool)
						}
						compRes.AddtionalVerbs[key][keyVerb] = false
					}
					match = false
				}
			}
		} else {
			//the key exists in actual, but does not exist in desired, i.e. mustonlyhave
			//based on the latest discussion with Kuan, we will allow this to exist, if we change this, and instead not allow any other keys to exist,
			//we can uncomment the code below
			/*
				if compRes.AdditionalKeys == nil { //initialize the map
					compRes.AdditionalKeys = make(map[string]map[string]bool)
					if compRes.AdditionalKeys[key] == nil {
						compRes.AdditionalKeys[key] = make(map[string]bool)
					}
					compRes.AdditionalKeys[key] = actual[key]
				} else {
					if compRes.AdditionalKeys[key] == nil {
						compRes.AdditionalKeys[key] = make(map[string]bool)
					}
					compRes.AdditionalKeys[key] = actual[key]
				}
				match = false
			*/

		}
	}
	return match, compRes
}

func flattenRole(role rbacv1.Role) map[string]map[string]bool {
	//takes as input a role, and flattens it out
	//from a role we create a map of apigroups. each group has a map of resources, each resource has a map of verbs
	flat := make(map[string]map[string]bool)

	//run thru the roles apigroup and resource and generate a combination
	for _, rule := range role.Rules {
		for _, apiG := range rule.APIGroups {
			for _, res := range rule.Resources {
				key := fmt.Sprintf("%s.%s", res, apiG)
				if _, ok := flat[key]; !ok {
					//the key does not exist, add it
					flat[key] = make(map[string]bool)
				}
				for _, verb := range rule.Verbs {
					if _, ok := flat[key][verb]; ok {
						//the verbs exist
					} else {
						// the verb does not exist
						flat[key][verb] = true
					}
				}
			}
		}
	}
	return flat
}

func flattenRoleTemplate(roleT policyv1alpha1.RoleTemplate) map[string]map[string]map[string]bool {
	//TODO make sure the verbs in mustnothave are not present in the musthave for the same keys
	//TODO make sure that the keys in mustonlyhave are deleted from the musthave and mustnothave
	flatT := make(map[string]map[string]map[string]bool)
	//run thru the roles template apigroup and resource and generate a combination
	for _, rule := range roleT.Rules {
		switch strings.ToLower(string(rule.ComplianceType)) {
		case strings.ToLower(string(policyv1alpha1.MustHave)):
			if flatT["musthave"] == nil {
				flatT["musthave"] = make(map[string]map[string]bool)
			}
			for _, apiG := range rule.PolicyRule.APIGroups {
				for _, res := range rule.PolicyRule.Resources {
					key := fmt.Sprintf("%s.%s", res, apiG)
					if _, ok := flatT["musthave"][key]; !ok {
						//the key does not exist, add it
						flatT["musthave"][key] = make(map[string]bool)
					}
					for _, verb := range rule.PolicyRule.Verbs {
						if _, ok := flatT["musthave"][key][verb]; ok {
							//the verbs exist
						} else {
							// the verb does not exist
							flatT["musthave"][key][verb] = true
						}
					}
				}
			}
		case strings.ToLower(string(policyv1alpha1.MustNotHave)):
			if flatT["mustnothave"] == nil {
				flatT["mustnothave"] = make(map[string]map[string]bool)
			}
			for _, apiG := range rule.PolicyRule.APIGroups {
				for _, res := range rule.PolicyRule.Resources {
					key := fmt.Sprintf("%s.%s", res, apiG)
					if _, ok := flatT["mustnothave"][key]; !ok {
						//the key does not exist, add it
						flatT["mustnothave"][key] = make(map[string]bool)
					}
					for _, verb := range rule.PolicyRule.Verbs {
						if _, ok := flatT["mustnothave"][key][verb]; ok {
							//the verbs exist
						} else {
							// the verb does not exist
							flatT["mustnothave"][key][verb] = true
						}
					}
				}
			}
		case strings.ToLower(string(policyv1alpha1.MustOnlyHave)):
			if flatT["mustonlyhave"] == nil {
				flatT["mustonlyhave"] = make(map[string]map[string]bool)
			}
			for _, apiG := range rule.PolicyRule.APIGroups {
				for _, res := range rule.PolicyRule.Resources {
					key := fmt.Sprintf("%s.%s", res, apiG)
					if _, ok := flatT["mustonlyhave"][key]; !ok {
						//the key does not exist, add it
						flatT["mustonlyhave"][key] = make(map[string]bool)
					}
					for _, verb := range rule.PolicyRule.Verbs {
						if _, ok := flatT["mustonlyhave"][key][verb]; ok {
							//the verbs exist
						} else {
							// the verb does not exist
							flatT["mustonlyhave"][key][verb] = true
						}
					}
				}
			}
		}
	}
	return flatT
}

func getPolicyNamespaces(policy *policyv1alpha1.ConfigurationPolicy) []string {
	//get all namespaces
	allNamespaces := getAllNamespaces()
	//then get the list of included
	includedNamespaces := []string{}
	included := policy.Spec.NamespaceSelector.Include
	for _, value := range included {
		found := common.FindPattern(value, allNamespaces)
		if found != nil {
			includedNamespaces = append(includedNamespaces, found...)
		}

	}
	//then get the list of excluded
	excludedNamespaces := []string{}
	excluded := policy.Spec.NamespaceSelector.Exclude
	for _, value := range excluded {
		found := common.FindPattern(value, allNamespaces)
		if found != nil {
			excludedNamespaces = append(excludedNamespaces, found...)
		}

	}

	//then get the list of deduplicated
	finalList := common.DeduplicateItems(includedNamespaces, excludedNamespaces)
	if len(finalList) == 0 {
		finalList = append(finalList, "default")
	}
	return finalList
}

func getAllNamespaces() (list []string) {
	listOpt := &metav1.ListOptions{}

	nsList, err := (*KubeClient).CoreV1().Namespaces().List(*listOpt)
	if err != nil {
		glog.Errorf("Error fetching namespaces from the API server: %v", err)
	}
	glog.Error("NAMESPACE LIST")
	namespacesNames := []string{}
	glog.Error(nsList)
	for _, n := range nsList.Items {
		namespacesNames = append(namespacesNames, n.Name)
	}

	return namespacesNames
}

func checkMessageSimilarity(objectT *policyv1alpha1.ObjectTemplate, cond *policyv1alpha1.Condition) bool {
	same := true
	lastIndex := len(objectT.Status.Conditions)
	if lastIndex > 0 {
		oldCond := objectT.Status.Conditions[lastIndex-1]
		if !IsSimilarToLastCondition(oldCond, *cond) {
			// objectT.Status.Conditions = AppendCondition(objectT.Status.Conditions, cond, "object", false)
			same = false
		}
	} else {
		// objectT.Status.Conditions = AppendCondition(objectT.Status.Conditions, cond, "object", false)
		same = false
	}
	return same
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
				glog.Errorf("!namespaced, full creation error: %s", err)
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
				glog.Errorf("namespaced, full creation error: %s", err)
				glog.Errorf("Error creating the object `%v`, the error is `%v`", name, errors.ReasonForError(err))
			}
		} else {
			created = true
			glog.V(4).Infof("Resource `%v` created\n", name)

		}
	}
	return created, err
}

func deleteObject(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, dclient dynamic.Interface) (result bool, erro error) {
	deleted := false
	var err error
	if !namespaced {
		res := dclient.Resource(rsrc)
		err = res.Delete(name, &metav1.DeleteOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				deleted = true
				glog.V(6).Infof("response to delete object `%v` from the api-server: %v", name, err)
			}
			glog.Errorf("object `%v` cannot be deleted from the api server, the error is: `%v`\n", name, err)
		} else {
			deleted = true
			glog.V(5).Infof("object `%v` deleted from the api server\n", name)
		}
	} else {
		res := dclient.Resource(rsrc).Namespace(namespace)
		err = res.Delete(name, &metav1.DeleteOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				deleted = true
				glog.V(6).Infof("response to delete object `%v` from the api-server: %v", name, err)
			}
			glog.Errorf("object `%v` cannot be deleted from the api server, the error is: `%v`\n", name, err)
		} else {
			deleted = true
			glog.V(5).Infof("object `%v` deleted from the api server\n", name)
		}
	}
	return deleted, err
}

func mergeSpecs(x1, x2 interface{}) (interface{}, error) {
	data1, err := json.Marshal(x1)
	if err != nil {
		return nil, err
	}
	data2, err := json.Marshal(x2)
	if err != nil {
		return nil, err
	}
	var j1 interface{}
	err = json.Unmarshal(data1, &j1)
	if err != nil {
		return nil, err
	}
	var j2 interface{}
	err = json.Unmarshal(data2, &j2)
	if err != nil {
		return nil, err
	}
	return mergeSpecsHelper(j1, j2), nil
}

func mergeSpecsHelper(x1, x2 interface{}) interface{} {
	switch x1 := x1.(type) {
	case map[string]interface{}:
		x2, ok := x2.(map[string]interface{})
		if !ok {
			return x1
		}
		for k, v2 := range x2 {
			if v1, ok := x1[k]; ok {
				x1[k] = mergeSpecsHelper(v1, v2)
			} else {
				x1[k] = v2
			}
		}
	case []interface{}:
		x2, ok := x2.([]interface{})
		if !ok {
			return x1
		}
		if len(x2) > 0 {
			_, ok := x2[0].(map[string]interface{})
			if ok {
				for idx, v2 := range x2 {
					v1 := x1[idx]
					x1[idx] = mergeSpecsHelper(v1, v2)
				}
			} else {
				return x1
			}
		} else {
			return x1
		}
	case nil:
		x2, ok := x2.(map[string]interface{})
		if ok {
			return x2
		}
	}
	return x1
}

func compareSpecs(newSpec map[string]interface{}, oldSpec map[string]interface{}) (updatedSpec map[string]interface{}, err error) {
	merged, err := mergeSpecs(newSpec, oldSpec)
	if err != nil {
		return merged.(map[string]interface{}), err
	}
	return merged.(map[string]interface{}), nil
}

func updateTemplate(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface, typeStr string) (success bool, message string) {
	updateNeeded := false
	if namespaced {
		res := dclient.Resource(rsrc).Namespace(namespace)
		existingObj, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			newObj := unstruct.Object["spec"]
			oldObj := existingObj.UnstructuredContent()["spec"]
			//merge changes into new spec
			newObj, err = compareSpecs(newObj.(map[string]interface{}), oldObj.(map[string]interface{}))
			if err != nil {
				message := fmt.Sprintf("Error merging changes into spec: %s", err)
				return false, message
			}
			//check if merged spec has changed
			nJSON, err := json.Marshal(newObj)
			if err != nil {
				message := fmt.Sprintf("Error converting updated spec to JSON: %s", err)
				return false, message
			}
			oJSON, err := json.Marshal(oldObj)
			if err != nil {
				message := fmt.Sprintf("Error converting updated spec to JSON: %s", err)
				return false, message
			}
			if !reflect.DeepEqual(nJSON, oJSON) {
				updateNeeded = true
			}
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

func updatePolicy(plc *policyv1alpha1.ConfigurationPolicy, retry int) error {
	setStatus(plc)
	copy := plc.DeepCopy()

	var tmp policyv1alpha1.ConfigurationPolicy
	tmp = *plc

	err := restClient.Get().
		Name(tmp.Name).
		Namespace(tmp.Namespace).
		Resource("configurationpolicies").
		Do().
		Into(&tmp)
	if err != nil {
		glog.Errorf("Error fetching policy %v, from the K8s API server the error is: %v", plc.Name, err)
	}

	if copy.ResourceVersion != tmp.ResourceVersion {
		copy.ResourceVersion = tmp.ResourceVersion
	}

	err = restClient.Put().
		Name(tmp.Name).
		Namespace(tmp.Namespace).
		Resource("configurationpolicies").
		Body(copy).
		Do().
		Into(copy)
	if err != nil {
		glog.Errorf("Error update policy %v, the error is: %v", plc.Name, err)
	}
	glog.V(2).Infof("Updated the policy `%v` in namespace `%v`", plc.Name, plc.Namespace)

	return err
}

func createRoleTemplateCondition(event string, rtValue roleOrigin, err error, conditions []policyv1alpha1.Condition, myMessage string) (condR []policyv1alpha1.Condition) {

	switch event {
	case "accessError":
		message := fmt.Sprintf("Error accessing K8s Api-server, the error is: %v", err)

		cond := &policyv1alpha1.Condition{
			Type:               "failed",
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s access error",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "accessError", false)

	case "createRoleError":
		message := fmt.Sprintf("Error creating a k8s RBAC role, the error is: %v", err)

		cond := &policyv1alpha1.Condition{
			Type:               "failed",
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s create role error",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role", false)
	case "createdRole":
		message := fmt.Sprintf("k8s RBAC role \"%v\" was missing ", rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC role created",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role Created", true)
	case "mismatch":
		message := fmt.Sprintf("Role must not include these permissions: ")

		cond := &policyv1alpha1.Condition{
			Type:               "failed",
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC role has a mismatch",
			Message:            message + myMessage,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role Mismatch", false)
	case "mismatchFixed":
		message := fmt.Sprintf("Role must not include these permissions: ")

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC role updated and mismatch was fixed",
			Message:            message + myMessage,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role MismatchFixed", true)
	case "missingRole":
		cls := ""
		if rtValue.roleTemplate.ObjectMeta.ClusterName != "" {
			cls = fmt.Sprintf(" Cluster %v,", rtValue.roleTemplate.ObjectMeta.ClusterName)
		}
		message := fmt.Sprintf("Following roles must exist:%v %v", cls, rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC role is missing",
			Message:            message + myMessage,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role", false)

	case "failedDeletingExtraRole":
		cls := ""
		if rtValue.roleTemplate.ObjectMeta.ClusterName != "" {
			cls = fmt.Sprintf(" Cluster %v,", rtValue.roleTemplate.ObjectMeta.ClusterName)
		}
		message := fmt.Sprintf("Following roles must not exist:%v %v", cls, rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC extra role exists",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role", false)
	case "deletedExtraRole":
		cls := ""
		if rtValue.roleTemplate.ObjectMeta.ClusterName != "" {
			cls = fmt.Sprintf(" Cluster %v,", rtValue.roleTemplate.ObjectMeta.ClusterName)
		}
		message := fmt.Sprintf("Following roles must not exist:%v %v", cls, rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC extra role",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role", true)

	case "ExtraRole":
		cls := ""
		if rtValue.roleTemplate.ObjectMeta.ClusterName != "" {
			cls = fmt.Sprintf(" Cluster %v,", rtValue.roleTemplate.ObjectMeta.ClusterName)
		}
		message := fmt.Sprintf("Following roles must not exist:%v %v", cls, rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC extra role",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role", true)

	case "match":
		message := fmt.Sprintf("k8s RBAC role \"%v\" exists and matches", rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC role matches",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role Matches", true)

	case "notExists":
		cls := ""
		if rtValue.roleTemplate.ObjectMeta.ClusterName != "" {
			cls = fmt.Sprintf(" Cluster %v,", rtValue.roleTemplate.ObjectMeta.ClusterName)
		}
		message := fmt.Sprintf("Following roles must exist:%v %v", cls, rtValue.roleTemplate.Name)

		cond := &policyv1alpha1.Condition{
			Type:               "completed",
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "K8s RBAC role doesn't exist",
			Message:            message,
		}
		conditions = AppendCondition(conditions, cond, "RBAC Role doesn't exist", true)

	}

	return conditions
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

func getdesiredRules(rtValue policyv1alpha1.RoleTemplate) []rbacv1.PolicyRule {
	pr := []rbacv1.PolicyRule{}
	for _, rule := range rtValue.Rules {
		if strings.ToLower(string(rule.ComplianceType)) == strings.ToLower(string(policyv1alpha1.MustNotHave)) {
			glog.Infof("skipping a mustnothave rule")
			continue
		} else {
			pr = append(pr, rule.PolicyRule)
		}

	}
	return pr
}

func buildRole(ro roleOrigin) *rbacv1.Role {
	role := &rbacv1.Role{}
	role.Name = ro.roleTemplate.Name

	if ro.roleTemplate.Selector != nil {
		if ro.roleTemplate.Selector.MatchLabels != nil {
			role.Labels = ro.roleTemplate.Selector.MatchLabels
		}
	}

	role.Namespace = ro.namespace

	for _, rl := range ro.roleTemplate.Rules {
		if strings.ToLower(string(rl.ComplianceType)) != "mustnothave" {
			role.Rules = append(role.Rules, rl.PolicyRule)
			glog.V(2).Infof("adding rule: %v ", rl.PolicyRule)
		}
	}

	return role
}

func getRoleNames(list []rbacv1.Role) []string {
	roleNames := []string{}
	for _, n := range list {
		roleNames = append(roleNames, n.Name)
	}
	return roleNames
}

func listToRoleMap(rlist []rbacv1.Role) map[string]rbacv1.Role {
	roleMap := make(map[string]rbacv1.Role)
	for _, role := range rlist {
		roleN := []string{role.Name, role.Namespace}
		roleNamespace := strings.Join(roleN, "-")
		roleMap[roleNamespace] = role
	}
	return roleMap
}

func createKeyValuePairs(m map[string]string) string {

	if m == nil {
		return ""
	}
	b := new(bytes.Buffer)
	for key, value := range m {
		fmt.Fprintf(b, "%s=%s,", key, value)
	}
	s := strings.TrimSuffix(b.String(), ",")
	return s
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
	WebHookURL, err := GetCEMWebhookURL(NamespaceWatched, clusterName, config)
	if err != nil {
		return "", err
	}
	glog.Errorf("webhook url: %s", WebHookURL)
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

// NewScheme creates an object for given group/version/kind and set ObjectKind
func NewScheme() *PluralScheme {
	return &PluralScheme{Scheme: runtime.NewScheme(), plurals: make(map[schema.GroupVersionKind]string)}
}

// SetPlural sets the plural for corresponding  group/version/kind
func (p *PluralScheme) SetPlural(gvk schema.GroupVersionKind, plural string) {
	p.plurals[gvk] = plural
}

// GetCEMWebhookURL populate the webhook value from a CRD
func GetCEMWebhookURL(namespace, clusterName string, config *rest.Config) (url string, err error) {
	alertScheme := NewScheme()
	alerttargetcontroller.AddToScheme(alertScheme.Scheme)
	alertScheme.SetPlural(alerttargetcontroller.SchemeGroupVersion.WithKind("AlertTarget"), "alerttargets")

	cfg := *config
	cfg.GroupVersion = &alerttargetcontroller.SchemeGroupVersion
	cfg.APIPath = "/apis"
	cfg.ContentType = runtime.ContentTypeJSON
	cfg.NegotiatedSerializer = PassthruCodecFactory{CodecFactory: serializer.NewCodecFactory(alertScheme.Scheme)}
	alertClient, err := rest.RESTClientFor(&cfg)
	if err != nil {
		glog.Error("Error generating AlertClient")
		return "", err
	}
	at := createAlertTargetInstance(namespace, clusterName)
	atmeta, err := meta.Accessor(&at)
	if err != nil {
		glog.Error("Error generating AlertTargetInstance")
		return "", err
	}
	err = alertClient.Get().
		Name(atmeta.GetName()).
		Namespace(atmeta.GetNamespace()).
		Resource("alerttargets").
		Do().
		Into(&at)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Errorf("The CRD instance `%v` of the alertTarget is not found, therefore no events will be triggered", fmt.Sprintf("%s-%s", namespace, clusterName))
		} else {
			glog.Errorf("The CRD instance `%v` of the alertTarget is not accessible, therefore no events will be triggered", fmt.Sprintf("%s-%s", namespace, clusterName))
		}
		return "", err
	}

	url = extractURL(at)

	glog.Infof("CEM Webhook URL found: %s", url)
	return url, nil
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
	for _, objectT := range policy.Spec.ObjectTemplates {
		if objectT.Status.ComplianceState == policyv1alpha1.NonCompliant {
			compliant = false
		}
	}
	if compliant {
		policy.Status.ComplianceState = policyv1alpha1.Compliant
	} else {
		policy.Status.ComplianceState = policyv1alpha1.NonCompliant
	}

	err := updatePolicy(policy, retry)
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
	for _, objectT := range policy.Spec.ObjectTemplates {
		if objectT.Status.ComplianceState == policyv1alpha1.NonCompliant {
			compliant = false
		}
	}
	for _, roleT := range policy.Spec.RoleTemplates {

		if roleT.Status.ComplianceState == policyv1alpha1.NonCompliant {
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
			APIVersion: "policies.ibm.com/v1alpha1",
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

func prettyPrint(res roleCompareResult, roleTName string) string {
	message := fmt.Sprintf("the role \" %v \" has ", roleTName)
	if len(res.missingKeys) > 0 {
		missingKeys := "missing keys: "
		for key, value := range res.missingKeys {

			missingKeys += (key + "{")
			for k := range value {
				missingKeys += (k + ",")
			}
			missingKeys = strings.TrimSuffix(missingKeys, ",")
			missingKeys += "} "
		}
		message += missingKeys
	}
	if len(res.missingVerbs) > 0 {
		missingVerbs := "missing verbs: "
		for key, value := range res.missingVerbs {

			missingVerbs += (key + "{")
			for k := range value {
				missingVerbs += (k + ",")
			}
			missingVerbs = strings.TrimSuffix(missingVerbs, ",")
			missingVerbs += "} "
		}
		message += missingVerbs
	}
	if len(res.AdditionalKeys) > 0 {
		AdditionalKeys := "additional keys: "
		for key, value := range res.AdditionalKeys {

			AdditionalKeys += (key + "{")
			for k := range value {
				AdditionalKeys += (k + ",")
			}
			AdditionalKeys = strings.TrimSuffix(AdditionalKeys, ",")
			AdditionalKeys += "} "
		}
		message += AdditionalKeys
	}
	if len(res.AddtionalVerbs) > 0 {
		AddtionalVerbs := "additional verbs: "
		for key, value := range res.AddtionalVerbs {

			AddtionalVerbs += (key + "{")
			for k := range value {
				AddtionalVerbs += (k + ",")
			}
			AddtionalVerbs = strings.TrimSuffix(AddtionalVerbs, ",")
			AddtionalVerbs += "} "
		}
		message += AddtionalVerbs
	}

	return message
}

func recoverFlow() {
	if r := recover(); r != nil {
		fmt.Println("ALERT!!!! -> recovered from ", r)
	}
}
