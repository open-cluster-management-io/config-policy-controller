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
	policyv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
	common "github.com/open-cluster-management/config-policy-controller/pkg/common"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

const controllerName string = "configuration-policy-controller"

var log = logf.Log.WithName(controllerName)

// availablePolicies is a cach all all available polices
var availablePolicies common.SyncedPolicyMap

// PlcChan a channel used to pass policies ready for update
var PlcChan chan *policyv1.ConfigurationPolicy

var recorder record.EventRecorder

var clientSet *kubernetes.Clientset

var eventNormal = "Normal"
var eventWarning = "Warning"
var eventFmtStr = "policy: %s/%s"

const getObjError = "object `%v` cannot be retrieved from the api server\n"
const convertJSONError = "Error converting updated %s to JSON: %s"

var config *rest.Config

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

// Add creates a new ConfigurationPolicy Controller and adds it to the Manager.
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileConfigurationPolicy{client: mgr.GetClient(), scheme: mgr.GetScheme(),
		recorder: mgr.GetEventRecorderFor("configurationpolicy-controller")}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource ConfigurationPolicy
	err = c.Watch(&source.Kind{Type: &policyv1.ConfigurationPolicy{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	return nil
}

// Initialize to initialize some controller variables
func Initialize(kubeconfig *rest.Config, clientset *kubernetes.Clientset,
	kubeClient *kubernetes.Interface, mgr manager.Manager, namespace, eventParent string) {
	InitializeClient(kubeClient)
	PlcChan = make(chan *policyv1.ConfigurationPolicy, 100) //buffering up to 100 policies for update
	NamespaceWatched = namespace
	clientSet = clientset
	EventOnParent = strings.ToLower(eventParent)
	recorder, _ = common.CreateRecorder(*KubeClient, controllerName)
	config = kubeconfig
}

//InitializeClient helper function to initialize kubeclient
func InitializeClient(kubeClient *kubernetes.Interface) {
	KubeClient = kubeClient
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
			reqLogger.Info("Configuration policy was deleted, removing it...")
			handleRemovingPolicy(request.NamespacedName.Name)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Info("Failed to retrieve configuration policy", "err", err)
		return reconcile.Result{}, err
	}

	reqLogger.Info("Configuration policy was found, adding it...")
	err = handleAddingPolicy(instance)
	if err != nil {
		reqLogger.Info("Failed to handleAddingPolicy", "err", err)
		return reconcile.Result{}, err
	}
	reqLogger.Info("Reconcile complete.")
	return reconcile.Result{}, nil
}

// PeriodicallyExecConfigPolicies always check status
func PeriodicallyExecConfigPolicies(freq uint, test bool) {
	// var plcToUpdateMap map[string]*policyv1.ConfigurationPolicy
	for {
		start := time.Now()
		printMap(availablePolicies.PolicyMap)
		flattenedPolicyList := map[string]*policyv1.ConfigurationPolicy{}
		for _, policy := range availablePolicies.PolicyMap {
			key := fmt.Sprintf("%s/%s", policy.GetName(), policy.GetResourceVersion())
			if _, ok := flattenedPolicyList[key]; ok {
				continue
			} else {
				flattenedPolicyList[key] = policy
			}
		}

		//get resources once per cycle to avoid hanging
		dd := clientSet.Discovery()
		apiresourcelist, err := dd.ServerResources()
		if err != nil {
			glog.Fatal(err)
		}
		apigroups, err := restmapper.GetAPIGroupResources(dd)
		if err != nil {
			glog.Fatal(err)
		}

		//flattenedpolicylist only contains 1 of each policy instance
		for _, policy := range flattenedPolicyList {
			Mx.Lock()
			handleObjectTemplates(*policy, apiresourcelist, apigroups)
			Mx.Unlock()
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

func addConditionToStatus(plc *policyv1.ConfigurationPolicy, cond *policyv1.Condition, index int,
	complianceState policyv1.ComplianceState) (updateNeeded bool) {
	var update bool
	if len((*plc).Status.CompliancyDetails) <= index {
		(*plc).Status.CompliancyDetails = append((*plc).Status.CompliancyDetails, policyv1.TemplateStatus{
			ComplianceState: complianceState,
			Conditions:      []policyv1.Condition{},
		})
	}
	if (*plc).Status.CompliancyDetails[index].ComplianceState != complianceState {
		update = true
	}
	(*plc).Status.CompliancyDetails[index].ComplianceState = complianceState

	if !checkMessageSimilarity((*plc).Status.CompliancyDetails[index].Conditions, cond) {
		conditions := AppendCondition((*plc).Status.CompliancyDetails[index].Conditions, cond, "", false)
		(*plc).Status.CompliancyDetails[index].Conditions = conditions
		update = true
	}
	return update
}

func createViolation(plc *policyv1.ConfigurationPolicy, index int, reason string, message string) (result bool) {
	var cond *policyv1.Condition
	cond = &policyv1.Condition{
		Type:               "violation",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	return addConditionToStatus(plc, cond, index, policyv1.NonCompliant)
}

func createNotification(plc *policyv1.ConfigurationPolicy, index int, reason string, message string) (result bool) {
	var cond *policyv1.Condition
	cond = &policyv1.Condition{
		Type:               "notification",
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	return addConditionToStatus(plc, cond, index, policyv1.Compliant)
}

func handleObjectTemplates(plc policyv1.ConfigurationPolicy, apiresourcelist []*metav1.APIResourceList,
	apigroups []*restmapper.APIGroupResources) {
	fmt.Println(fmt.Sprintf("processing object templates for policy %s...", plc.GetName()))
	plcNamespaces := getPolicyNamespaces(plc)
	for indx, objectT := range plc.Spec.ObjectTemplates {
		nonCompliantObjects := map[string][]string{}
		compliantObjects := map[string][]string{}
		enforce := strings.ToLower(string(plc.Spec.RemediationAction)) == strings.ToLower(string(policyv1.Enforce))
		relevantNamespaces := plcNamespaces
		kind := "unknown"
		desiredName := ""
		mustNotHave := strings.ToLower(string(objectT.ComplianceType)) == strings.ToLower(string(policyv1.MustNotHave))

		//override policy namespaces if one is present in object template
		var unstruct unstructured.Unstructured
		unstruct.Object = make(map[string]interface{})
		var blob interface{}
		ext := objectT.ObjectDefinition
		if jsonErr := json.Unmarshal(ext.Raw, &blob); jsonErr != nil {
			glog.Error(jsonErr)
			return
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
			names, compliant, objKind := handleObjects(objectT, ns, indx, &plc, config, recorder, apiresourcelist, apigroups)
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
					compliantObjects[ns] = names
				}
			}
		}

		if !enforce {
			objData := map[string]interface{}{
				"indx":        indx,
				"kind":        kind,
				"desiredName": desiredName,
			}
			createInformStatus(mustNotHave, numCompliant, numNonCompliant, compliantObjects, nonCompliantObjects, &plc, objData)
		}
	}
}

func createInformStatus(mustNotHave bool, numCompliant int, numNonCompliant int, compliantObjects map[string][]string,
	nonCompliantObjects map[string][]string, plc *policyv1.ConfigurationPolicy, objData map[string]interface{}) {
	update := false
	compliant := false
	desiredName := objData["desiredName"].(string)
	indx := objData["indx"].(int)
	kind := objData["kind"].(string)
	if !mustNotHave && numCompliant == 0 {
		//noncompliant; musthave and objects do not exist
		message := fmt.Sprintf("No instances of `%v` exist as specified, and one should be created", kind)
		if desiredName != "" {
			message = fmt.Sprintf("%v `%v` does not exist as specified, and should be created", kind, desiredName)
		}
		update = createViolation(plc, indx, "K8s missing a must have object", message)
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
		update = createViolation(plc, indx, "K8s has a must `not` have object", message)
	}
	if !mustNotHave && numCompliant > 0 {
		//compliant; musthave and objects exist
		nameStr := ""
		for ns, names := range compliantObjects {
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
		message := fmt.Sprintf("%v %v exist as specified, therefore this Object template is compliant", kind, nameStr)
		update = createNotification(plc, indx, "K8s `must have` object already exists", message)
		compliant = true
	}
	if mustNotHave && numNonCompliant == 0 {
		//compliant; mustnothave and no objects exist
		message := fmt.Sprintf("no instances of `%v` exist as specified, therefore this Object template is compliant", kind)
		update = createNotification(plc, indx, "K8s must `not` have object already missing", message)
		compliant = true
	}
	if update {
		//update parent policy with violation
		eventType := eventNormal
		if !compliant {
			eventType = eventWarning
		}
		recorder.Event(plc, eventType, fmt.Sprintf("policy: %s", plc.GetName()), convertPolicyStatusToString(plc))
		addForUpdate(plc)
	}
}

func handleObjects(objectT *policyv1.ObjectTemplate, namespace string, index int, policy *policyv1.ConfigurationPolicy,
	config *rest.Config, recorder record.EventRecorder, apiresourcelist []*metav1.APIResourceList,
	apigroups []*restmapper.APIGroupResources) (objNameList []string, compliant bool, rsrcKind string) {
	if namespace != "" {
		fmt.Println(fmt.Sprintf("handling object template [%d] in namespace %s", index, namespace))
	} else {
		fmt.Println(fmt.Sprintf("handling object template [%d] (no namespace specified)", index))
	}
	namespaced := true
	ext := objectT.ObjectDefinition
	mapping := getMapping(apigroups, ext, policy, index)
	if mapping == nil {
		return nil, false, ""
	}
	var unstruct unstructured.Unstructured
	unstruct.Object = make(map[string]interface{})
	var blob interface{}
	if err := json.Unmarshal(ext.Raw, &blob); err != nil {
		glog.Fatal(err)
	}
	unstruct.Object = blob.(map[string]interface{}) //set object to the content of the blob after Unmarshalling
	exists := true
	objNames := []string{}
	remediation := policy.Spec.RemediationAction
	name, kind, metaNamespace := getDetails(unstruct)
	if metaNamespace != "" {
		namespace = metaNamespace
	}
	dclient, rsrc, namespaced := getClientRsrc(mapping, apiresourcelist)
	if namespaced && namespace == "" {
		//namespaced but none specified, generate violation
		updateStatus := createViolation(policy, index, "K8s missing namespace",
			"namespaced object has no namespace specified")
		if updateStatus {
			eventType := eventNormal
			if index < len(policy.Status.CompliancyDetails) &&
				policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
				eventType = eventWarning
			}
			recorder.Event(policy, eventType, fmt.Sprintf(eventFmtStr, policy.GetName(), name),
				convertPolicyStatusToString(policy))
			addForUpdate(policy)
		}
		return nil, false, ""
	}
	if name != "" {
		exists = objectExists(namespaced, namespace, name, rsrc, unstruct, dclient)
		objNames = append(objNames, name)
	} else if kind != "" {
		objNames = append(objNames, getNamesOfKind(rsrc, namespaced, namespace, dclient)...)
		remediation = "inform"
		if len(objNames) == 0 {
			exists = false
		}
	}
	objShouldExist := strings.ToLower(string(objectT.ComplianceType)) != strings.ToLower(string(policyv1.MustNotHave))
	if len(objNames) == 1 {
		name = objNames[0]
		return handleSingleObj(policy, remediation, exists, objShouldExist, rsrc, dclient, objectT, map[string]interface{}{
			"name":       name,
			"namespace":  namespace,
			"namespaced": namespaced,
			"index":      index,
			"unstruct":   unstruct,
		})
	}
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

	return nil, compliant, ""
}

func handleSingleObj(policy *policyv1.ConfigurationPolicy, remediation policyv1.RemediationAction, exists bool,
	objShouldExist bool, rsrc schema.GroupVersionResource, dclient dynamic.Interface, objectT *policyv1.ObjectTemplate,
	data map[string]interface{}) (objNameList []string, compliance bool, rsrcKind string) {
	var err error
	var compliant bool
	updateNeeded := false
	name := data["name"].(string)
	unstruct := data["unstruct"].(unstructured.Unstructured)
	if !exists && objShouldExist {
		//it is a musthave and it does not exist, so it must be created
		if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
			updateNeeded, err = handleMissingMustHave(policy, remediation, rsrc, dclient, data)
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
			updateNeeded, err = handleExistsMustNotHave(policy, remediation, rsrc, dclient, data)
			if err != nil {
				glog.Errorf("error handling a existing object `%v` that is a must NOT have according to policy `%v`", name, policy.Name)
			}
		} else { //inform
			compliant = false
		}
	}
	if !exists && !objShouldExist {
		//it is a must not have and it does not exist, so it is compliant
		if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
			updateNeeded = handleMissingMustNotHave(policy, rsrc, data)
		}
		compliant = true
	}
	if exists && objShouldExist {
		//it is a must have and it does exist, so it is compliant
		if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
			updateNeeded = handleExistsMustHave(policy, rsrc, data)
		}
		compliant = true
	}

	if exists {
		updated, throwSpecViolation, msg := updateTemplate(strings.ToLower(string(objectT.ComplianceType)),
			data, remediation, rsrc, dclient, unstruct.Object["kind"].(string), nil)
		if !updated && throwSpecViolation {
			compliant = false
		} else if !updated && msg != "" {
			updateNeeded = createViolation(policy, data["index"].(int), "K8s update template error", msg)
		}
	}

	if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Inform)) {
		return []string{name}, compliant, rsrc.Resource
	}

	if updateNeeded {
		eventType := eventNormal
		if data["index"].(int) < len(policy.Status.CompliancyDetails) &&
			policy.Status.CompliancyDetails[data["index"].(int)].ComplianceState == policyv1.NonCompliant {
			eventType = eventWarning
		}
		recorder.Event(policy, eventType, fmt.Sprintf(eventFmtStr, policy.GetName(), name),
			convertPolicyStatusToString(policy))
		addForUpdate(policy)
	}
	return nil, compliant, ""
}

func getClientRsrc(mapping *meta.RESTMapping, apiresourcelist []*metav1.APIResourceList) (dclient dynamic.Interface,
	rsrc schema.GroupVersionResource, namespaced bool) {
	namespaced = false
	restconfig := config
	restconfig.GroupVersion = &schema.GroupVersion{
		Group:   mapping.GroupVersionKind.Group,
		Version: mapping.GroupVersionKind.Version,
	}
	dclient, err := dynamic.NewForConfig(restconfig)
	if err != nil {
		glog.Fatal(err)
	}

	rsrc = mapping.Resource
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
	return dclient, rsrc, namespaced
}

func getMapping(apigroups []*restmapper.APIGroupResources, ext runtime.RawExtension,
	policy *policyv1.ConfigurationPolicy, index int) (mapping *meta.RESTMapping) {
	updateNeeded := false
	restmapper := restmapper.NewDiscoveryRESTMapper(apigroups)
	glog.V(9).Infof("reading raw object: %v", string(ext.Raw))
	_, gvk, err := unstructured.UnstructuredJSONScheme.Decode(ext.Raw, nil, nil)
	if err != nil {
		decodeErr := fmt.Sprintf("Decoding error, please check your policy file!"+
			" Aborting handling the object template at index [%v] in policy `%v` with error = `%v`",
			index, policy.Name, err)
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
		return nil
	}
	mapping, err = restmapper.RESTMapping(gvk.GroupKind(), gvk.Version)
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
		return nil
	}
	return mapping
}

func getDetails(unstruct unstructured.Unstructured) (name string, kind string, namespace string) {
	name = ""
	kind = ""
	namespace = ""
	if md, ok := unstruct.Object["metadata"]; ok {

		metadata := md.(map[string]interface{})
		if objectName, ok := metadata["name"]; ok {
			name = objectName.(string)
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
	return name, kind, namespace
}

func getNamesOfKind(rsrc schema.GroupVersionResource, namespaced bool, ns string,
	dclient dynamic.Interface) (kindNameList []string) {
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

func handleMissingMustNotHave(plc *policyv1.ConfigurationPolicy, rsrc schema.GroupVersionResource,
	metadata map[string]interface{}) bool {
	glog.V(7).Infof("entering `does not exists` & ` must not have`")
	name := metadata["name"].(string)
	index := metadata["index"].(int)

	message := fmt.Sprintf("%v `%v` is missing as it should be, therefore this Object template is compliant",
		rsrc.Resource, name)
	return createNotification(plc, index, "K8s must `not` have object already missing", message)
}

func handleExistsMustHave(plc *policyv1.ConfigurationPolicy, rsrc schema.GroupVersionResource,
	metadata map[string]interface{}) (updateNeeded bool) {
	name := metadata["name"].(string)
	index := metadata["index"].(int)

	message := fmt.Sprintf("%v `%v` exists as it should be, therefore this Object template is compliant",
		rsrc.Resource, name)
	return createNotification(plc, index, "K8s must have object already missing", message)
}

func handleExistsMustNotHave(plc *policyv1.ConfigurationPolicy, action policyv1.RemediationAction,
	rsrc schema.GroupVersionResource, dclient dynamic.Interface,
	metadata map[string]interface{}) (result bool, erro error) {
	glog.V(7).Infof("entering `exists` & ` must not have`")

	name := metadata["name"].(string)
	namespace := metadata["namespace"].(string)
	index := metadata["index"].(int)
	namespaced := metadata["namespaced"].(bool)

	var update, deleted bool
	var err error

	if strings.ToLower(string(action)) == strings.ToLower(string(policyv1.Enforce)) {
		if deleted, err = deleteObject(namespaced, namespace, name, rsrc, dclient); !deleted {
			message := fmt.Sprintf("%v `%v` exists, and cannot be deleted, reason: `%v`", rsrc.Resource, name, err)
			update = createViolation(plc, index, "K8s deletion error", message)
		} else { //deleted successfully
			message := fmt.Sprintf("%v `%v` existed, and was deleted successfully", rsrc.Resource, name)
			update = createNotification(plc, index, "K8s deletion success", message)
		}
	}
	return update, err
}

func handleMissingMustHave(plc *policyv1.ConfigurationPolicy, action policyv1.RemediationAction,
	rsrc schema.GroupVersionResource, dclient dynamic.Interface,
	metadata map[string]interface{}) (result bool, erro error) {
	glog.V(7).Infof("entering `does not exists` & ` must have`")

	name := metadata["name"].(string)
	namespace := metadata["namespace"].(string)
	index := metadata["index"].(int)
	namespaced := metadata["namespaced"].(bool)
	unstruct := metadata["unstruct"].(unstructured.Unstructured)

	var update, created bool
	var err error
	if strings.ToLower(string(action)) == strings.ToLower(string(policyv1.Enforce)) {
		if created, err = createObject(namespaced, namespace, name, rsrc, unstruct, dclient); !created {
			message := fmt.Sprintf("%v `%v` is missing, and cannot be created, reason: `%v`", rsrc.Resource, name, err)
			update = createViolation(plc, index, "K8s creation error", message)
		} else { //created successfully
			glog.V(8).Infof("entering `%v` created successfully", name)
			message := fmt.Sprintf("%v `%v` was missing, and was created successfully", rsrc.Resource, name)
			update = createNotification(plc, index, "K8s creation success", message)
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
		finalList = append(finalList, "")
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

func objectExists(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource,
	unstruct unstructured.Unstructured, dclient dynamic.Interface) (result bool) {
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
			glog.Errorf(getObjError, name)

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
			glog.Errorf(getObjError, name)
		} else {
			exists = true
			glog.V(6).Infof("object `%v` retrieved from the api server\n", name)
		}
	}
	return exists
}

func createObject(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource,
	unstruct unstructured.Unstructured, dclient dynamic.Interface) (result bool, erro error) {
	var err error
	created := false
	// set ownerReference for mutaionPolicy and override remediationAction

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

func deleteObject(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource,
	dclient dynamic.Interface) (result bool, erro error) {
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
	for _, val2 := range old {
		found := false
		for _, val1 := range new {
			if reflect.DeepEqual(val1, val2) {
				found = true
			}
		}
		if !found {
			new = append(new, val2)
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

func compareSpecs(newSpec map[string]interface{}, oldSpec map[string]interface{},
	ctype string) (updatedSpec map[string]interface{}, err error) {
	if ctype == "mustonlyhave" {
		return newSpec, nil
	}
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

func handleKeys(unstruct unstructured.Unstructured, existingObj *unstructured.Unstructured,
	remediation policyv1.RemediationAction, complianceType string, typeStr string, name string,
	res dynamic.ResourceInterface) (a bool, b bool, c string) {
	var err error
	updateNeeded := false
	for key := range unstruct.Object {
		if !isBlacklisted(key) {
			newObj := unstruct.Object[key]
			oldObj := existingObj.UnstructuredContent()[key]
			if newObj == nil || oldObj == nil {
				return false, false, ""
			}

			//merge changes into new spec
			var mergedObj interface{}
			switch newObj := newObj.(type) {
			case []interface{}:
				mergedObj, err = compareLists(newObj, oldObj.([]interface{}), complianceType)
			case map[string]interface{}:
				mergedObj, err = compareSpecs(newObj, oldObj.(map[string]interface{}), complianceType)
			}
			if err != nil {
				message := fmt.Sprintf("Error merging changes into %s: %s", key, err)
				return false, false, message
			}
			//check if merged spec has changed
			nJSON, err := json.Marshal(mergedObj)
			if err != nil {
				message := fmt.Sprintf(convertJSONError, key, err)
				return false, false, message
			}
			oJSON, err := json.Marshal(oldObj)
			if err != nil {
				message := fmt.Sprintf(convertJSONError, key, err)
				return false, false, message
			}
			if !reflect.DeepEqual(nJSON, oJSON) {
				updateNeeded = true
			}
			mapMtx := sync.RWMutex{}
			mapMtx.Lock()
			existingObj.UnstructuredContent()[key] = mergedObj
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

func updateTemplate(
	complianceType string, metadata map[string]interface{}, remediation policyv1.RemediationAction,
	rsrc schema.GroupVersionResource, dclient dynamic.Interface,
	typeStr string, parent *policyv1.ConfigurationPolicy) (success bool, throwSpecViolation bool, message string) {
	name := metadata["name"].(string)
	namespace := metadata["namespace"].(string)
	namespaced := metadata["namespaced"].(bool)
	unstruct := metadata["unstruct"].(unstructured.Unstructured)

	var res dynamic.ResourceInterface
	if namespaced {
		res = dclient.Resource(rsrc).Namespace(namespace)
	} else {
		res = dclient.Resource(rsrc)
	}
	existingObj, err := res.Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf(getObjError, name)
	} else {
		return handleKeys(unstruct, existingObj, remediation, complianceType, typeStr, name, res)
	}
	return false, false, ""
}

// AppendCondition check and appends conditions
func AppendCondition(conditions []policyv1.Condition, newCond *policyv1.Condition, resourceType string,
	resolved ...bool) (conditionsRes []policyv1.Condition) {
	defer recoverFlow()
	lastIndex := len(conditions)
	if lastIndex > 0 {
		oldCond := conditions[lastIndex-1]
		if IsSimilarToLastCondition(oldCond, *newCond) {
			conditions[lastIndex-1] = *newCond
			return conditions
		}

	} else {
		//first condition => trigger event
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

func addForUpdate(policy *policyv1.ConfigurationPolicy) {
	compliant := true
	for index := range policy.Spec.ObjectTemplates {
		if index < len(policy.Status.CompliancyDetails) {
			if policy.Status.CompliancyDetails[index].ComplianceState == policyv1.NonCompliant {
				compliant = false
			}
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
		// time.Sleep(100) //giving enough time to sync
	}
}

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
			reconcilingAgent.recorder.Event(instance, "Normal", "Policy updated", fmt.Sprintf("Policy status is: %v",
				instance.Status.ComplianceState))
		}
	}
	return nil, nil
}

func handleRemovingPolicy(name string) {
	for k, v := range availablePolicies.PolicyMap {
		if v.Name == name {
			availablePolicies.RemoveObject(k)
		}
	}
}

func handleAddingPolicy(plc *policyv1.ConfigurationPolicy) error {
	allNamespaces, err := common.GetAllNamespaces()
	if err != nil {
		glog.Errorf("reason: error fetching the list of available namespaces,"+
			" subject: K8s API server, namespace: all, according to policy: %v, additional-info: %v", plc.Name, err)
		return err
	}
	//clean up that policy from the existing namepsaces, in case the modification is in the namespace selector
	for _, ns := range allNamespaces {
		key := fmt.Sprintf("%s/%s", ns, plc.Name)
		if policy, found := availablePolicies.GetObject(key); found {
			if policy.Name == plc.Name {
				availablePolicies.RemoveObject(key)
			}
		}
	}
	selectedNamespaces := common.GetSelectedNamespaces(plc.Spec.NamespaceSelector.Include,
		plc.Spec.NamespaceSelector.Exclude, allNamespaces)
	for _, ns := range selectedNamespaces {
		key := fmt.Sprintf("%s/%s", ns, plc.Name)
		availablePolicies.AddObject(key, plc)
	}
	if len(selectedNamespaces) == 0 {
		key := fmt.Sprintf("%s/%s", "NA", plc.Name)
		availablePolicies.AddObject(key, plc)
	}
	return err
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
// Helper functions that pretty prints a map
func printMap(myMap map[string]*policyv1.ConfigurationPolicy) {
	if len(myMap) == 0 {
		fmt.Println("Waiting for policies to be available for processing... ")
		return
	}
	fmt.Println("Available policies in namespaces: ")

	mapToPrint := map[string][]string{}
	for k, v := range myMap {
		if _, ok := mapToPrint[v.Name]; ok {
			mapToPrint[v.Name] = append(mapToPrint[v.Name], strings.Split(k, "/")[0])
		} else {
			mapToPrint[v.Name] = []string{strings.Split(k, "/")[0]}
		}
	}

	for k, v := range mapToPrint {
		nsString := "["
		for idx, ns := range v {
			nsString += ns
			if idx != len(v)-1 {
				nsString += ", "
			}
		}
		nsString += "]"
		fmt.Println(fmt.Sprintf("configpolicy %s in namespace(s) %s", k, nsString))
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
		eventType := "Normal"
		if instance.Status.ComplianceState == policyv1.NonCompliant {
			eventType = "Warning"
		}
		reconcilingAgent.recorder.Event(&parentPlc,
			eventType,
			fmt.Sprintf(eventFmtStr, instance.Namespace, instance.Name),
			convertPolicyStatusToString(instance))
	}
}

func createParentPolicy(instance *policyv1.ConfigurationPolicy) policyv1.Policy {
	ns := common.ExtractNamespaceLabel(instance)
	if ns == "" {
		ns = NamespaceWatched
	}
	plc := policyv1.Policy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.OwnerReferences[0].Name,
			Namespace: ns, // we are making an assumption here that the parent policy is in the watched-namespace passed as flag
			UID:       instance.OwnerReferences[0].UID,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Policy",
			APIVersion: "policy.open-cluster-management.io/v1",
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
		for idx, cond := range v.Conditions {
			result += cond.Type + " - " + cond.Message
			if idx != len(v.Conditions)-1 {
				result += ", "
			}
		}
	}
	return result
}

func recoverFlow() {
	if r := recover(); r != nil {
		fmt.Println("ALERT!!!! -> recovered from ", r)
	}
}
