// Note to U.S. Government Users Restricted Rights:
// Use, duplication or disclosure restricted by GSA ADP Schedule
// Contract with IBM Corp.
// Copyright (c) 2020 Red Hat, Inc.

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
	policyv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policies/v1"
	common "github.com/open-cluster-management/config-policy-controller/pkg/common"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

//UpdatePolicyMap used to keep track of policies to be updated
var UpdatePolicyMap = make(map[string]*policyv1.ConfigurationPolicy)

var log = logf.Log.WithName("controller_configurationpolicy")

// Finalizer used to ensure consistency when deleting a CRD
const Finalizer = "finalizer.policies.open-cluster-management.io"

const grcCategory = "system-and-information-integrity"

// availablePolicies is a cach all all available polices
var availablePolicies common.SyncedPolicyMap

// PlcChan a channel used to pass policies ready for update
var PlcChan chan *policyv1.ConfigurationPolicy

var recorder record.EventRecorder

var clientSet *kubernetes.Clientset

var eventNormal = "Normal"
var eventWarning = "Warning"

var config *rest.Config

var restClient *rest.RESTClient

var syncAlertTargets bool

//CemWebhookURL url to send events to
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
	err = c.Watch(&source.Kind{Type: &policyv1.ConfigurationPolicy{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	// Watch for changes to secondary resource Pods and requeue the owner ConfigurationPolicy
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &policyv1.ConfigurationPolicy{},
	})
	if err != nil {
		return err
	}

	return nil
}

// Initialize to initialize some controller variables
func Initialize(kubeconfig *rest.Config, clientset *kubernetes.Clientset, kClient *kubernetes.Interface, mgr manager.Manager, namespace, eventParent string,
	syncAlert bool, clustName string) {
	InitializeClient(kClient)
	PlcChan = make(chan *policyv1.ConfigurationPolicy, 100) //buffering up to 100 policies for update

	NamespaceWatched = namespace
	clientSet = clientset

	EventOnParent = strings.ToLower(eventParent)

	recorder, _ = common.CreateRecorder(*KubeClient, "policy-controller")
	config = kubeconfig
	config.GroupVersion = &schema.GroupVersion{Group: "policies.open-cluster-management.io", Version: "v1"}
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

//InitializeClient helper function to initialize kubeclient
func InitializeClient(kClient *kubernetes.Interface) {
	KubeClient = kClient
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
	instance := &policyv1.ConfigurationPolicy{}
	if reconcilingAgent == nil {
		reconcilingAgent = r
	}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("error 1 *********")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Info("error 2 *********")
		return reconcile.Result{}, err
	}

	// name of our mcm custom finalizer
	myFinalizerName := Finalizer

	//if instance.ObjectMeta.DeletionTimestamp.IsZero()
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
		log.Info(fmt.Sprintf("adding policy %s", instance.GetName()))
		err := handleAddingPolicy(instance)
		if err != nil {
			glog.V(3).Infof("Failed to handleAddingPolicy")
		}
	} else {
		log.Info(fmt.Sprintf("removing policy %s", instance.GetName()))
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

	relevantNamespaces := getPolicyNamespaces(*instance)
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

func handlePolicyPerNamespace(namespace string, plc *policyv1.ConfigurationPolicy, added bool) {
	//for each template we should handle each Kind of objects. I.e. iterate on all the items
	//of a given Kind
	glog.V(6).Infof("Policy: %v, namespace: %v\n", plc.Name, namespace)
}

// PeriodicallyExecSamplePolicies always check status
func PeriodicallyExecSamplePolicies(freq uint, test bool) {
	var plcToUpdateMap map[string]*policyv1.ConfigurationPolicy
	for {
		start := time.Now()
		printMap(availablePolicies.PolicyMap)
		plcToUpdateMap = make(map[string]*policyv1.ConfigurationPolicy)
		for _, policy := range availablePolicies.PolicyMap {
			//For each namespace, fetch all the RoleBindings in that NS according to the policy selector
			//For each RoleBindings get the number of users
			//update the status internal map
			//no difference between enforce and inform here

			// roleBindingList, err := (*common.KubeClient).RbacV1().RoleBindings(namespace).
			// 	List(metav1.ListOptions{LabelSelector: labels.Set(policy.Spec.LabelSelector).String()})
			// if err != nil {
			// 	glog.Errorf("reason: communication error, subject: k8s API server, namespace: %v, according to policy: %v, additional-info: %v\n",
			// 		namespace, policy.Name, err)
			// 	continue
			// }
			// userViolationCount, GroupViolationCount := checkViolationsPerNamespace(roleBindingList, policy)
			if strings.EqualFold(string(policy.Spec.RemediationAction), string(policyv1.Enforce)) {
				glog.V(5).Infof("Enforce is set, but ignored :-)")
			}
			// if addViolationCount(policy, userViolationCount, GroupViolationCount, namespace) {
			// 	plcToUpdateMap[policy.Name] = policy
			// }

			Mx.Lock()
			handleObjectTemplates(*policy)
			Mx.Unlock()

			// checkComplianceBasedOnDetails(policy)
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
		if test == true {
			return
		}
	}
}

func createViolation(plc *policyv1.ConfigurationPolicy, index int, reason string, message string) (result bool) {
	var update bool
	var cond *policyv1.Condition
	cond = &policyv1.Condition{
		Type:               "violation",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	if len((*plc).Status.CompliancyDetails) <= index {
		(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: policyv1.NonCompliant,
			Conditions:      []policyv1.Condition{},
		})
	}
	if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.NonCompliant {
		update = true
	}
	(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.NonCompliant

	if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
		conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
		(*plc).Status.CompliancyDetails[index].Conditions = conditions
		update = true
	}
	return update
}

func createNotification(plc *policyv1.ConfigurationPolicy, index int, reason string, message string) (result bool) {
	var update bool
	var cond *policyv1.Condition
	cond = &policyv1.Condition{
		Type:               "notification",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	if len((*plc).Status.CompliancyDetails) <= index {
		(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: policyv1.Compliant,
			Conditions:      []policyv1.Condition{},
		})
	}
	if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.Compliant {
		update = true
	}
	(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.Compliant

	if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
		conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
		(*plc).Status.CompliancyDetails[index].Conditions = conditions
		update = true
	}
	return update
}

func handleObjectTemplates(plc policyv1.ConfigurationPolicy) {
	if reflect.DeepEqual(plc.Labels["ignore"], "true") {
		plc.Status = policyv1.ConfigurationPolicyStatus{
			ComplianceState: policyv1.UnknownCompliancy,
		}
	}
	plcNamespaces := getPolicyNamespaces(plc)
	for indx, objectT := range plc.Spec.ObjectTemplates {
		nonCompliantObjects := map[string][]string{}
		mustNotHave := strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1.MustNotHave))
		enforce := strings.ToLower(string(plc.Spec.RemediationAction)) == strings.ToLower(string(policyv1.Enforce))
		relevantNamespaces := plcNamespaces
		kind := "unknown"
		desiredName := ""

		//override policy namespaces if one is present in object template
		var unstruct unstructured.Unstructured
		unstruct.Object = make(map[string]interface{})
		var blob interface{}
		ext := objectT.ObjectDefinition
		if jsonErr := json.Unmarshal(ext.Raw, &blob); jsonErr != nil {
			glog.Fatal(jsonErr)
		}
		unstruct.Object = blob.(map[string]interface{})
		if md, ok := unstruct.Object["metadata"]; ok {
			metadata := md.(map[string]interface{})
			if objectns, ok := metadata["namespace"]; ok {
				relevantNamespaces = []string{objectns.(string)}
			}
			if objectname, ok := metadata["name"]; ok {
				desiredName = objectname.(string)
			}
		}

		numCompliant := 0
		numNonCompliant := 0

		for _, ns := range relevantNamespaces {
			names, compliant, objKind := handleObjects(objectT, ns, indx, &plc, clientSet, config, recorder)
			if objKind != "" {
				kind = objKind
			}
			if names == nil {
				//object template enforced, already handled in handleObjects
				continue
			} else {
				enforce = false
				if !compliant {
					numNonCompliant += len(names)
					nonCompliantObjects[ns] = names
				} else {
					numCompliant += len(names)
				}
			}
		}

		if !enforce {
			update := false
			if !mustNotHave && numCompliant == 0 {
				//noncompliant; musthave and objects do not exist
				message := fmt.Sprintf("No instances of `%v` exist as specified, and one should be created", kind)
				if desiredName != "" {
					message = fmt.Sprintf("%v `%v` does not exist as specified, and should be created", kind, desiredName)
				}
				update = createViolation(&plc, indx, "K8s missing a must have object", message)
			}
			if mustNotHave && numNonCompliant > 0 {
				//noncompliant; mustnothave and objects exist
				nameStr := ""
				for ns, names := range nonCompliantObjects {
					nameStr += "["
					for i, name := range names {
						nameStr += name
						if i != len(names)-1 {
							nameStr += ", "
						}
					}
					nameStr += "] in namespace " + ns + "; "
				}
				nameStr = nameStr[:len(nameStr)-2]
				message := fmt.Sprintf("%v exist and should be deleted: %v", kind, nameStr)
				update = createViolation(&plc, indx, "K8s has a must `not` have object", message)
			}
			if !mustNotHave && numCompliant > 0 {
				//compliant; musthave and objects exist
				message := fmt.Sprintf("%d instances of %v exist as specified, therefore this Object template is compliant", numCompliant, kind)
				update = createNotification(&plc, indx, "K8s must `not` have object already missing", message)
			}
			if mustNotHave && numNonCompliant == 0 {
				//compliant; mustnothave and no objects exist
				message := fmt.Sprintf("no instances of `%v` exist as specified, therefore this Object template is compliant", kind)
				update = createNotification(&plc, indx, "K8s `must have` object already exists", message)
			}
			if update {
				//update parent policy with violation
				eventType := eventNormal
				if plc.Status.CompliancyDetails[indx].ComplianceState == policyv1.NonCompliant {
					eventType = eventWarning
				}
				recorder.Event(&plc, eventType, fmt.Sprintf("policy: %s", plc.GetName()), convertPolicyStatusToString(&plc))
				addForUpdate(&plc)
			}
		}
	}
}

func handleObjects(objectT *policyv1.ObjectTemplate, namespace string, index int, policy *policyv1.ConfigurationPolicy, clientset *kubernetes.Clientset, config *rest.Config, recorder record.EventRecorder) (objNameList []string, compliant bool, rsrcKind string) {
	updateNeeded := false
	namespaced := true
	dd := clientset.Discovery()
	apigroups, err := restmapper.GetAPIGroupResources(dd)
	if err != nil {
		glog.Fatal(err)
	}

	restmapper := restmapper.NewDiscoveryRESTMapper(apigroups)
	//ext := runtime.RawExtension{}
	ext := objectT.ObjectDefinition
	glog.V(9).Infof("reading raw object: %v", string(ext.Raw))
	_, gvk, err := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, nil)
	if err != nil {
		decodeErr := fmt.Sprintf("Decoding error, please check your policy file! Aborting handling the object template at index [%v] in policy `%v` with error = `%v`", index, policy.Name, err)
		glog.Errorf(decodeErr)

		if len(policy.Status.CompliancyDetails) <= index {
			policy.Status.CompliancyDetails = append(policy.Status.CompliancyDetails, policyv1.TemplateStatus{
				ComplianceState: policyv1.NonCompliant,
				Conditions:      []policyv1.Condition{},
			})
		}
		policy.Status.CompliancyDetails[index].ComplianceState = policyv1.NonCompliant
		policy.Status.CompliancyDetails[index].Conditions = []policyv1.Condition{
			policyv1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s decode object definition error",
				Message:            decodeErr,
			},
		}
		addForUpdate(policy)
		return nil, false, ""
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
				conditions := AppendCondition(policy.Status.CompliancyDetails[index].Conditions, cond, gvk.GroupKind().Kind, false)
				policy.Status.CompliancyDetails[index].Conditions = conditions
				updateNeeded = true
			}
		}
		if updateNeeded {
			recorder.Event(policy, eventWarning, fmt.Sprintf("policy: %s", policy.GetName()), errMsg)
			addForUpdate(policy)
		}
		return nil, false, ""
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
	kind := ""
	named := false
	if md, ok := unstruct.Object["metadata"]; ok {

		metadata := md.(map[string]interface{})
		if objectName, ok := metadata["name"]; ok {
			name = objectName.(string)
			named = true
		}
		// override the namespace if specified in objectTemplates
		if objectns, ok := metadata["namespace"]; ok {
			glog.V(5).Infof("overriding the namespace as it is specified in objectTemplates...")
			namespace = objectns.(string)
		}

	}

	if objKind, ok := unstruct.Object["kind"]; ok {
		kind = objKind.(string)
	}

	exists := true
	objNames := []string{}
	remediation := policy.Spec.RemediationAction

	if named {
		exists = objectExists(namespaced, namespace, name, rsrc, unstruct, dclient)
		objNames = append(objNames, name)
	} else if kind != "" {
		objNames = append(objNames, getNamesOfKind(rsrc, namespaced, namespace, dclient)...)
		remediation = "inform"
		if len(objNames) == 0 {
			exists = false
		}
	}
	objShouldExist := !(strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1.MustNotHave)))
	if len(objNames) == 1 {
		name = objNames[0]
		if !exists && objShouldExist {
			//it is a musthave and it does not exist, so it must be created
			if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
				updateNeeded, err = handleMissingMustHave(policy, index, remediation, namespaced, namespace, name, rsrc, unstruct, dclient)
				if err != nil {
					// violation created for handling error
					glog.Errorf("error handling a missing object `%v` that is a must have according to policy `%v`", name, policy.Name)
				}
			} else { //inform
				compliant = false
			}
		}
		if exists && !objShouldExist {
			//it is a mustnothave but it exist, so it must be deleted
			if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
				updateNeeded, err = handleExistsMustNotHave(policy, index, remediation, namespaced, namespace, name, rsrc, dclient)
				if err != nil {
					glog.Errorf("error handling a existing object `%v` that is a must NOT have according to policy `%v`", name, policy.Name)
				}
			} else { //inform
				compliant = false
			}
		}
		if !exists && !objShouldExist {
			//it is a must not have and it does not exist, so it is compliant
			updateNeeded = handleMissingMustNotHave(policy, index, name, rsrc)
			compliant = true
		}
		if exists && objShouldExist {
			//it is a must have and it does exist, so it is compliant
			updateNeeded = handleExistsMustHave(policy, index, name, rsrc)
			compliant = true
		}

		if exists {
			updated, throwSpecViolation, msg := updateTemplate(strings.ToLower(string(objectT.ComplianceType)), namespaced, namespace, name, remediation, rsrc, unstruct, dclient, unstruct.Object["kind"].(string), nil)
			if !updated && throwSpecViolation {
				compliant = false
			} else if !updated && msg != "" {
				cond := &policyv1.Condition{
					Type:               "violation",
					Status:             corev1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "K8s update template error",
					Message:            msg,
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
					conditions := AppendCondition(policy.Status.CompliancyDetails[index].Conditions, cond, rsrc.Resource, false)
					policy.Status.CompliancyDetails[index].Conditions = conditions
					updateNeeded = true
				}
				glog.Errorf(msg)
			}
		}

		if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Inform)) {
			return objNames, compliant, rsrc.Resource
		}

		if updateNeeded {
			eventType := eventNormal
			if index < len(policy.Status.CompliancyDetails) && policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
				eventType = eventWarning
			}
			recorder.Event(policy, eventType, fmt.Sprintf("policy: %s/%s", policy.GetName(), name), convertPolicyStatusToString(policy))
			addForUpdate(policy)
		}
	} else {
		if !exists && objShouldExist {
			return objNames, false, rsrc.Resource
		}
		if exists && !objShouldExist {
			return objNames, false, rsrc.Resource
		}
		if !exists && !objShouldExist {
			return objNames, true, rsrc.Resource
		}
		if exists && objShouldExist {
			return objNames, true, rsrc.Resource
		}
	}
	return nil, compliant, ""
}

func getNamesOfKind(rsrc schema.GroupVersionResource, namespaced bool, ns string, dclient dynamic.Interface) (kindNameList []string) {
	if namespaced {
		res := dclient.Resource(rsrc).Namespace(ns)
		resList, err := res.List(metav1.ListOptions{})
		if err != nil {
			glog.Error(err)
			return []string{}
		}
		kindNameList = []string{}
		for _, uObj := range resList.Items {
			kindNameList = append(kindNameList, uObj.Object["metadata"].(map[string]interface{})["name"].(string))
		}
		return kindNameList
	}
	res := dclient.Resource(rsrc)
	resList, err := res.List(metav1.ListOptions{})
	if err != nil {
		glog.Error(err)
		return []string{}
	}
	kindNameList = []string{}
	for _, uObj := range resList.Items {
		kindNameList = append(kindNameList, uObj.Object["metadata"].(map[string]interface{})["name"].(string))
	}
	return kindNameList
}

func handleMissingMustNotHave(plc *policyv1.ConfigurationPolicy, index int, name string, rsrc schema.GroupVersionResource) bool {
	glog.V(7).Infof("entering `does not exists` & ` must not have`")
	var cond *policyv1.Condition
	var update bool
	message := fmt.Sprintf("%v `%v` is missing as it should be, therefore this Object template is compliant", rsrc.Resource, name)
	cond = &policyv1.Condition{
		Type:               "succeeded",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "K8s must `not` have object already missing",
		Message:            message,
	}
	if len((*plc).Status.CompliancyDetails) <= index {
		(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: policyv1.Compliant,
			Conditions:      []policyv1.Condition{},
		})
	}
	if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.Compliant {
		update = true
	}
	(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.Compliant

	if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
		conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, rsrc.Resource, true)
		(*plc).Status.CompliancyDetails[index].Conditions = conditions
		update = true
	}
	return update
}

func handleExistsMustHave(plc *policyv1.ConfigurationPolicy, index int, name string, rsrc schema.GroupVersionResource) (updateNeeded bool) {
	var cond *policyv1.Condition
	var update bool
	message := fmt.Sprintf("%v `%v` exists as it should be, therefore this Object template is compliant", rsrc.Resource, name)
	cond = &policyv1.Condition{
		Type:               "notification",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "K8s `must have` object already exists",
		Message:            message,
	}
	if len((*plc).Status.CompliancyDetails) <= index {
		(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: policyv1.Compliant,
			Conditions:      []policyv1.Condition{},
		})
	}
	if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.Compliant {
		update = true
	}
	(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.Compliant

	if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
		conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, rsrc.Resource, true)
		(*plc).Status.CompliancyDetails[index].Conditions = conditions
		update = true
	}
	return update
}

func handleExistsMustNotHave(plc *policyv1.ConfigurationPolicy, index int, action policyv1.RemediationAction, namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, dclient dynamic.Interface) (result bool, erro error) {
	glog.V(7).Infof("entering `exists` & ` must not have`")
	var cond *policyv1.Condition
	var update, deleted bool
	var err error

	if strings.ToLower(string(action)) == strings.ToLower(string(policyv1.Enforce)) {
		if deleted, err = deleteObject(namespaced, namespace, name, rsrc, dclient); !deleted {
			message := fmt.Sprintf("%v `%v` exists, and cannot be deleted, reason: `%v`", rsrc.Resource, name, err)
			cond = &policyv1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s deletion error",
				Message:            message,
			}
			if len((*plc).Status.CompliancyDetails) <= index {
				(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
					ComplianceState: policyv1.NonCompliant,
					Conditions:      []policyv1.Condition{},
				})
			}
			if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.NonCompliant {
				update = true
			}
			(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.NonCompliant

			if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
				conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
				(*plc).Status.CompliancyDetails[index].Conditions = conditions
				update = true
			}
		} else { //deleted successfully
			message := fmt.Sprintf("%v `%v` existed, and was deleted successfully", rsrc.Resource, name)
			cond = &policyv1.Condition{
				Type:               "notification",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s deletion success",
				Message:            message,
			}
			if len((*plc).Status.CompliancyDetails) <= index {
				(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
					ComplianceState: policyv1.Compliant,
					Conditions:      []policyv1.Condition{},
				})
			}
			if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.Compliant {
				update = true
			}
			(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.Compliant

			if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
				conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
				(*plc).Status.CompliancyDetails[index].Conditions = conditions
				update = true
			}
		}
	}
	return update, err
}

func handleMissingMustHave(plc *policyv1.ConfigurationPolicy, index int, action policyv1.RemediationAction, namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface) (result bool, erro error) {
	glog.V(7).Infof("entering `does not exists` & ` must have`")

	var update, created bool
	var err error
	var cond *policyv1.Condition
	if strings.ToLower(string(action)) == strings.ToLower(string(policyv1.Enforce)) {
		if created, err = createObject(namespaced, namespace, name, rsrc, unstruct, dclient, nil); !created {
			message := fmt.Sprintf("%v `%v` is missing, and cannot be created, reason: `%v`", rsrc.Resource, name, err)
			cond = &policyv1.Condition{
				Type:               "violation",
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation error",
				Message:            message,
			}
			if len((*plc).Status.CompliancyDetails) <= index {
				(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
					ComplianceState: policyv1.NonCompliant,
					Conditions:      []policyv1.Condition{},
				})
			}
			if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.NonCompliant {
				update = true
			}
			(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.NonCompliant

			if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
				conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
				(*plc).Status.CompliancyDetails[index].Conditions = conditions
				update = true
			}
		} else { //created successfully
			glog.V(8).Infof("entering `%v` created successfully", name)
			message := fmt.Sprintf("%v `%v` was missing, and was created successfully", rsrc.Resource, name)
			cond = &policyv1.Condition{
				Type:               "notification",
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "K8s creation success",
				Message:            message,
			}
			if len((*plc).Status.CompliancyDetails) <= index {
				(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
					ComplianceState: policyv1.Compliant,
					Conditions:      []policyv1.Condition{},
				})
			}
			if (*plc).Status.CompliancyDetails[index].ComplianceState != policyv1.Compliant {
				update = true
			}
			(*plc).Status.CompliancyDetails[index].ComplianceState = policyv1.Compliant

			if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
				conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
				(*plc).Status.CompliancyDetails[index].Conditions = conditions
				update = true
			}
		}
	}
	return update, err
}

func getPolicyNamespaces(policy policyv1.ConfigurationPolicy) []string {
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
	namespacesNames := []string{}
	for _, n := range nsList.Items {
		namespacesNames = append(namespacesNames, n.Name)
	}

	return namespacesNames
}

func checkMessageSimilarity(conditions []policyv1.Condition, cond *policyv1.Condition) bool {
	same := true
	lastIndex := len(conditions)
	if lastIndex > 0 {
		oldCond := conditions[lastIndex-1]
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

func createObject(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface, parent *policyv1.ConfigurationPolicy) (result bool, erro error) {
	var err error
	created := false
	// set ownerReference for mutaionPolicy and override remediationAction
	if parent != nil {
		plcOwnerReferences := *metav1.NewControllerRef(parent, schema.GroupVersionKind{
			Group:   policyv1.SchemeGroupVersion.Group,
			Version: policyv1.SchemeGroupVersion.Version,
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

func mergeSpecs(x1, x2 interface{}, ctype string) (interface{}, error) {
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
	return mergeSpecsHelper(j1, j2, ctype), nil
}

func mergeSpecsHelper(x1, x2 interface{}, ctype string) interface{} {
	switch x1 := x1.(type) {
	case map[string]interface{}:
		x2, ok := x2.(map[string]interface{})
		if !ok {
			return x1
		}
		for k, v2 := range x2 {
			if v1, ok := x1[k]; ok {
				x1[k] = mergeSpecsHelper(v1, v2, ctype)
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
					x1[idx] = mergeSpecsHelper(v1, v2, ctype)
				}
			} else {
				if ctype == "musthave" {
					return mergeArrays(x1, x2)
				}
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

func mergeArrays(new []interface{}, old []interface{}) (result []interface{}) {
	for _, val1 := range new {
		found := false
		for _, val2 := range old {
			if reflect.DeepEqual(val1, val2) {
				found = true
			}
		}
		if !found {
			new = append(new, val1)
		}
	}
	return new
}

func compareLists(newList []interface{}, oldList []interface{}, ctype string) (updatedList []interface{}, err error) {
	if ctype == "musthave" {
		return mergeArrays(newList, oldList), nil
	}
	//mustonlyhave
	mergedList := []interface{}{}
	for idx, item := range newList {
		newItem, err := mergeSpecs(item, oldList[idx], ctype)
		if err != nil {
			return nil, err
		}
		mergedList = append(mergedList, newItem)
	}
	return mergedList, nil
}

func compareSpecs(newSpec map[string]interface{}, oldSpec map[string]interface{}, ctype string) (updatedSpec map[string]interface{}, err error) {
	merged, err := mergeSpecs(newSpec, oldSpec, ctype)
	if err != nil {
		return merged.(map[string]interface{}), err
	}
	return merged.(map[string]interface{}), nil
}

func isBlacklisted(key string) (result bool) {
	blacklist := []string{"apiVersion", "metadata", "kind", "status"}
	for _, val := range blacklist {
		if key == val {
			return true
		}
	}
	return false
}

func updateTemplate(
	complianceType string, namespaced bool, namespace string, name string, remediation policyv1.RemediationAction,
	rsrc schema.GroupVersionResource, unstruct unstructured.Unstructured, dclient dynamic.Interface,
	typeStr string, parent *policyv1.ConfigurationPolicy) (success bool, throwSpecViolation bool, message string) {
	updateNeeded := false
	if namespaced {
		res := dclient.Resource(rsrc).Namespace(namespace)
		existingObj, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			for key := range unstruct.Object {
				if !isBlacklisted(key) {
					newObj := unstruct.Object[key]
					oldObj := existingObj.UnstructuredContent()[key]
					if newObj == nil || oldObj == nil {
						return false, false, ""
					}

					//merge changes into new spec
					switch newObj := newObj.(type) {
					case []interface{}:
						newObj, err = compareLists(newObj, oldObj.([]interface{}), complianceType)
					case map[string]interface{}:
						newObj, err = compareSpecs(newObj, oldObj.(map[string]interface{}), complianceType)
					}
					if err != nil {
						message := fmt.Sprintf("Error merging changes into %s: %s", key, err)
						return false, false, message
					}
					//check if merged spec has changed
					nJSON, err := json.Marshal(newObj)
					if err != nil {
						message := fmt.Sprintf("Error converting updated %s to JSON: %s", key, err)
						return false, false, message
					}
					oJSON, err := json.Marshal(oldObj)
					if err != nil {
						message := fmt.Sprintf("Error converting updated %s to JSON: %s", key, err)
						return false, false, message
					}
					if !reflect.DeepEqual(nJSON, oJSON) {
						updateNeeded = true
					}
					mapMtx := sync.RWMutex{}
					mapMtx.Lock()
					existingObj.UnstructuredContent()[key] = newObj
					mapMtx.Unlock()
					if updateNeeded {
						if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Inform)) {
							return false, true, ""
						}
						//enforce
						glog.V(4).Infof("Updating %v template `%v`...", typeStr, name)
						_, err = res.Update(existingObj, metav1.UpdateOptions{})
						if errors.IsNotFound(err) {
							message := fmt.Sprintf("`%v` is not present and must be created", typeStr)
							return false, false, message
						}
						if err != nil {
							message := fmt.Sprintf("Error updating the object `%v`, the error is `%v`", name, err)
							return false, false, message
						}
						glog.V(4).Infof("Resource `%v` updated\n", name)
					}
				}
			}
			return false, false, ""
		}
	} else {
		res := dclient.Resource(rsrc)
		existingObj, err := res.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			for key := range unstruct.Object {
				if !isBlacklisted(key) {
					newObj := unstruct.Object[key]
					oldObj := existingObj.UnstructuredContent()[key]
					if newObj == nil || oldObj == nil {
						return false, false, ""
					}
					updateNeeded := !(reflect.DeepEqual(newObj, oldObj))
					oldMap := existingObj.UnstructuredContent()["metadata"].(map[string]interface{})
					resVer := oldMap["resourceVersion"]
					mapMtx := sync.RWMutex{}
					mapMtx.Lock()
					unstruct.Object["metadata"].(map[string]interface{})["resourceVersion"] = resVer
					mapMtx.Unlock()
					if updateNeeded {
						if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Inform)) {
							return false, true, ""
						}
						//enforce
						glog.V(4).Infof("Updating %v template `%v`...", typeStr, name)
						_, err = res.Update(&unstruct, metav1.UpdateOptions{})
						if errors.IsNotFound(err) {
							message := fmt.Sprintf("`%v` is not present and must be created", typeStr)
							return false, false, message
						}
						if err != nil {
							message := fmt.Sprintf("Error updating the object `%v`, the error is `%v`", name, err)
							return false, false, message
						}
						glog.V(4).Infof("Resource `%v` updated\n", name)
					}
				}
			}
			return false, false, ""
		}
	}
	return false, false, ""
}

// AppendCondition check and appends conditions
func AppendCondition(conditions []policyv1.Condition, newCond *policyv1.Condition, resourceType string, resolved ...bool) (conditionsRes []policyv1.Condition) {
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
func IsSimilarToLastCondition(oldCond policyv1.Condition, newCond policyv1.Condition) bool {
	if reflect.DeepEqual(oldCond.Status, newCond.Status) &&
		reflect.DeepEqual(oldCond.Reason, newCond.Reason) &&
		reflect.DeepEqual(oldCond.Message, newCond.Message) &&
		reflect.DeepEqual(oldCond.Type, newCond.Type) {
		return true
	}
	return false
}

func triggerEvent(cond policyv1.Condition, resourceType string, resolved []bool) (res string, err error) {

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
	WebHookURL, err := GetCEMWebhookURL()
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
func GetCEMWebhookURL() (url string, err error) {

	if CemWebhookURL == "" {
		return "", fmt.Errorf("undefined CEM webhook: %s", CemWebhookURL)
	}
	return CemWebhookURL, nil
}

func addForUpdate(policy *policyv1.ConfigurationPolicy) {
	compliant := true
	for index := range policy.Spec.ObjectTemplates {
		if policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
			compliant = false
		}
	}
	if compliant {
		policy.Status.ComplianceState = policyv1.Compliant
	} else {
		policy.Status.ComplianceState = policyv1.NonCompliant
	}
	_, err := updatePolicyStatus(map[string]*policyv1.ConfigurationPolicy{
		(*policy).GetName(): policy,
	})
	if err != nil {
		log.Error(err, err.Error())
		time.Sleep(100) //giving enough time to sync
	}
}

func ensureDefaultLabel(instance *policyv1.ConfigurationPolicy) (updateNeeded bool) {
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

func checkUnNamespacedPolicies(plcToUpdateMap map[string]*policyv1.ConfigurationPolicy) error {
	// plcMap := convertMaptoPolicyNameKey()
	// // group the policies with cluster users and the ones with groups
	// // take the plc with min users and groups and make it your baseline
	// ClusteRoleBindingList, err := (*common.KubeClient).RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
	// if err != nil {
	// 	glog.Errorf("reason: communication error, subject: k8s API server, namespace: all, according to policy: none, additional-info: %v\n", err)
	// 	return err
	// }

	// clusterLevelUsers, clusterLevelGroups := checkAllClusterLevel(ClusteRoleBindingList)

	// for _, policy := range plcMap {
	// 	var userViolationCount, groupViolationCount int
	// 	if policy.Spec.MaxClusterRoleBindingUsers < clusterLevelUsers && policy.Spec.MaxClusterRoleBindingUsers >= 0 {
	// 		userViolationCount = clusterLevelUsers - policy.Spec.MaxClusterRoleBindingUsers
	// 	}
	// 	if policy.Spec.MaxClusterRoleBindingGroups < clusterLevelGroups && policy.Spec.MaxClusterRoleBindingGroups >= 0 {
	// 		groupViolationCount = clusterLevelGroups - policy.Spec.MaxClusterRoleBindingGroups
	// 	}
	// 	if addViolationCount(policy, userViolationCount, groupViolationCount, "cluster-wide") {
	// 		plcToUpdateMap[policy.Name] = policy
	// 	}
	// 	checkComplianceBasedOnDetails(policy)
	// }

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

func convertMaptoPolicyNameKey() map[string]*policyv1.ConfigurationPolicy {
	plcMap := make(map[string]*policyv1.ConfigurationPolicy)
	for _, policy := range availablePolicies.PolicyMap {
		plcMap[policy.Name] = policy
	}
	return plcMap
}

func checkViolationsPerNamespace(roleBindingList *v1.RoleBindingList, plc *policyv1.ConfigurationPolicy) (userV, groupV int) {
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

// func addViolationCount(plc *policyv1.ConfigurationPolicy, userCount int, groupCount int, namespace string) bool {
// 	changed := false
// 	msg := fmt.Sprintf("%s violations detected in namespace `%s`, there are %v users violations and %v groups violations",
// 		fmt.Sprint(userCount+groupCount),
// 		namespace,
// 		userCount,
// 		groupCount)
// 	if plc.Status.CompliancyDetails == nil {
// 		plc.Status.CompliancyDetails = make(map[string]map[string][]string)
// 	}
// 	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
// 		plc.Status.CompliancyDetails[plc.Name] = make(map[string][]string)
// 	}
// 	if plc.Status.CompliancyDetails[plc.Name][namespace] == nil {
// 		plc.Status.CompliancyDetails[plc.Name][namespace] = []string{}
// 	}
// 	if len(plc.Status.CompliancyDetails[plc.Name][namespace]) == 0 {
// 		plc.Status.CompliancyDetails[plc.Name][namespace] = []string{msg}
// 		changed = true
// 		return changed
// 	}
// 	firstNum := strings.Split(plc.Status.CompliancyDetails[plc.Name][namespace][0], " ")
// 	if len(firstNum) > 0 {
// 		if firstNum[0] == fmt.Sprint(userCount+groupCount) {
// 			return false
// 		}
// 	}
// 	plc.Status.CompliancyDetails[plc.Name][namespace][0] = msg
// 	changed = true
// 	return changed
// }

// func checkComplianceBasedOnDetails(plc *policyv1.ConfigurationPolicy) {
// 	plc.Status.ComplianceState = policyv1.Compliant
// 	if plc.Status.CompliancyDetails == nil {
// 		return
// 	}
// 	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
// 		return
// 	}
// 	if len(plc.Status.CompliancyDetails[plc.Name]) == 0 {
// 		return
// 	}
// 	for namespace, msgList := range plc.Status.CompliancyDetails[plc.Name] {
// 		if len(msgList) > 0 {
// 			violationNum := strings.Split(plc.Status.CompliancyDetails[plc.Name][namespace][0], " ")
// 			if len(violationNum) > 0 {
// 				if violationNum[0] != fmt.Sprint(0) {
// 					plc.Status.ComplianceState = policyv1.NonCompliant
// 				}
// 			}
// 		} else {
// 			return
// 		}
// 	}
// }

// func checkComplianceChangeBasedOnDetails(plc *policyv1.ConfigurationPolicy) (complianceChanged bool) {
// 	//used in case we also want to know not just the compliance state, but also whether the compliance changed or not.
// 	previous := plc.Status.ComplianceState
// 	if plc.Status.CompliancyDetails == nil {
// 		plc.Status.ComplianceState = policyv1.UnknownCompliancy
// 		return reflect.DeepEqual(previous, plc.Status.ComplianceState)
// 	}
// 	if _, ok := plc.Status.CompliancyDetails[plc.Name]; !ok {
// 		plc.Status.ComplianceState = policyv1.UnknownCompliancy
// 		return reflect.DeepEqual(previous, plc.Status.ComplianceState)
// 	}
// 	if len(plc.Status.CompliancyDetails[plc.Name]) == 0 {
// 		plc.Status.ComplianceState = policyv1.UnknownCompliancy
// 		return reflect.DeepEqual(previous, plc.Status.ComplianceState)
// 	}
// 	plc.Status.ComplianceState = policyv1.Compliant
// 	for namespace, msgList := range plc.Status.CompliancyDetails[plc.Name] {
// 		if len(msgList) > 0 {
// 			violationNum := strings.Split(plc.Status.CompliancyDetails[plc.Name][namespace][0], " ")
// 			if len(violationNum) > 0 {
// 				if violationNum[0] != fmt.Sprint(0) {
// 					plc.Status.ComplianceState = policyv1.NonCompliant
// 				}
// 			}
// 		} else {
// 			return reflect.DeepEqual(previous, plc.Status.ComplianceState)
// 		}
// 	}
// 	if plc.Status.ComplianceState != policyv1.NonCompliant {
// 		plc.Status.ComplianceState = policyv1.Compliant
// 	}
// 	return reflect.DeepEqual(previous, plc.Status.ComplianceState)
// }

func updatePolicyStatus(policies map[string]*policyv1.ConfigurationPolicy) (*policyv1.ConfigurationPolicy, error) {
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

func setStatus(policy *policyv1.ConfigurationPolicy) {
	compliant := true
	for index := range policy.Spec.ObjectTemplates {
		if policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
			compliant = false
		}
	}
	if compliant {
		policy.Status.ComplianceState = policyv1.Compliant
	} else {
		policy.Status.ComplianceState = policyv1.NonCompliant
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

func handleRemovingPolicy(plc *policyv1.ConfigurationPolicy) {
	for k, v := range availablePolicies.PolicyMap {
		if v.Name == plc.Name {
			availablePolicies.RemoveObject(k)
		}
	}
}

func handleAddingPolicy(plc *policyv1.ConfigurationPolicy) error {
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

func newConfigurationPolicy(plc *policyv1.ConfigurationPolicy) *unstructured.Unstructured {
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
func (r *ReconcileConfigurationPolicy) deleteExternalDependency(instance *policyv1.ConfigurationPolicy) error {
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
func printMap(myMap map[string]*policyv1.ConfigurationPolicy) {
	if len(myMap) == 0 {
		fmt.Println("Waiting for policies to be available for processing... ")
		return
	}
	fmt.Println("Available policies in namespaces: ")

	for k, v := range myMap {
		fmt.Printf("namespace = %v; policy = %v \n", k, v.Name)
	}
}

func createParentPolicyEvent(instance *policyv1.ConfigurationPolicy) {
	if len(instance.OwnerReferences) == 0 {
		return //there is nothing to do, since no owner is set
	}
	// we are making an assumption that the GRC policy has a single owner, or we chose the first owner in the list
	if string(instance.OwnerReferences[0].UID) == "" {
		return //there is nothing to do, since no owner UID is set
	}

	parentPlc := createParentPolicy(instance)

	if reconcilingAgent.recorder != nil {
		reconcilingAgent.recorder.Event(&parentPlc,
			corev1.EventTypeNormal,
			fmt.Sprintf("policy: %s/%s", instance.Namespace, instance.Name),
			convertPolicyStatusToString(instance))
	}
}

func createParentPolicy(instance *policyv1.ConfigurationPolicy) policyv1.ConfigurationPolicy {
	ns := common.ExtractNamespaceLabel(instance)
	if ns == "" {
		ns = NamespaceWatched
	}
	plc := policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.OwnerReferences[0].Name,
			Namespace: ns, // we are making an assumption here that the parent policy is in the watched-namespace passed as flag
			UID:       instance.OwnerReferences[0].UID,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigurationPolicy",
			APIVersion: "policies.open-cluster-management.io/v1",
		},
	}
	return plc
}

//=================================================================
// convertPolicyStatusToString to be able to pass the status as event
func convertPolicyStatusToString(plc *policyv1.ConfigurationPolicy) (results string) {
	result := "ComplianceState is still undetermined"
	if plc.Status.ComplianceState == "" {
		return result
	}
	result = string(plc.Status.ComplianceState)

	if plc.Status.CompliancyDetails == nil {
		return result
	}
	if len(plc.Status.CompliancyDetails) == 0 {
		return result
	}
	for _, v := range plc.Status.CompliancyDetails {
		result += "; "
		for _, cond := range v.Conditions {
			result += cond.Type + " - " + cond.Message + ", "
		}
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
