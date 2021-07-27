// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package configurationpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	gocmp "github.com/google/go-cmp/cmp"
	policyv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
	common "github.com/open-cluster-management/config-policy-controller/pkg/common"
	templates "github.com/open-cluster-management/config-policy-controller/pkg/common/templates"
	corev1 "k8s.io/api/core/v1"
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
var plcFmtStr = "policy: %s"

var reasonWantFoundExists = "Resource found as expected"
var reasonWantFoundNoMatch = "Resource found but does not match"
var reasonWantFoundDNE = "Resource not found but should exist"
var reasonWantNotFoundExists = "Resource found but should not exist"
var reasonWantNotFoundDNE = "Resource not found as expected"

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
	templates.InitializeKubeClient(kubeClient, kubeconfig)
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
		apiresourcelist, apiresourcelistErr := dd.ServerResources()
		skipLoop := false
		if apiresourcelistErr != nil {
			skipLoop = true
			glog.Errorf("Failed to retrieve apiresourcelist with err: %v", apiresourcelistErr)
		}
		apigroups, apigroupsErr := restmapper.GetAPIGroupResources(dd)

		if !skipLoop && apigroupsErr != nil {
			skipLoop = true
			glog.Errorf("Failed to retrieve apigroups with err: %v", apigroupsErr)

		}
		if skipLoop {
			glog.Errorf("Unexpected failure detected. You api server might not be stable. Waiting for next loop...")
		} else {
			//flattenedpolicylist only contains 1 of each policy instance
			for _, policy := range flattenedPolicyList {
				Mx.Lock()
				handleObjectTemplates(*policy, apiresourcelist, apigroups)
				Mx.Unlock()
			}
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
	if plc.Spec.RemediationAction == "" {
		message := "Policy does not have a RemediationAction specified"
		update := createViolation(&plc, 0, "No RemediationAction", message)
		if update {
			recorder.Event(&plc, eventWarning, fmt.Sprintf(plcFmtStr, plc.GetName()), convertPolicyStatusToString(&plc))
			addForUpdate(&plc)
		}
		return
	}
	// initialize the RelatedObjects for this Configuration Policy
	oldRelated := []policyv1.RelatedObject{}
	for i := range plc.Status.RelatedObjects {
		oldRelated = append(oldRelated, plc.Status.RelatedObjects[i])
	}
	relatedObjects := []policyv1.RelatedObject{}
	parentUpdate := false

	// initialize apiresources for template processing before starting objectTemplate processing
	// this is optional but since apiresourcelist is already available,
	// use this rather than re-discovering the list for generic-lookup
	templates.SetAPIResources(apiresourcelist)

	for indx, objectT := range plc.Spec.ObjectTemplates {
		nonCompliantObjects := map[string]map[string]interface{}{}
		compliantObjects := map[string]map[string]interface{}{}
		enforce := strings.ToLower(string(plc.Spec.RemediationAction)) == strings.ToLower(string(policyv1.Enforce))
		relevantNamespaces := plcNamespaces
		kind := ""
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

		// Here appears to be a  good place to hook in template processing
		// This is at the head of objectemplate processing
		// ( just before the perNamespace handling of objectDefinitions)

		// check here to determine if the object definition has a template
		// and execute  template-processing only if  there is a template pattern "{{" in it
		// to avoid unnecessary parsing when there is no template in the definition.

		if templates.HasTemplate(string(ext.Raw)) {
			resolvedblob, tplErr := templates.ResolveTemplate(blob)
			if tplErr != nil {
				update := createViolation(&plc, 0, "Error processing template", tplErr.Error())
				if update {
					recorder.Event(&plc, eventWarning, fmt.Sprintf(plcFmtStr, plc.GetName()), convertPolicyStatusToString(&plc))
					addForUpdate(&plc)
				}
				return
			}

			//marshal it back and set it on the objectTemplate so be used  in processed further down
			resolveddata, jsonErr := json.Marshal(resolvedblob)
			if jsonErr != nil {
				glog.Error(jsonErr)
				return
			}

			//Set the resolved data for use in further processing
			objectT.ObjectDefinition.Raw = resolveddata
			blob = resolvedblob
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
		handled := false
		objNamespaced := false
		for _, ns := range relevantNamespaces {
			names, compliant, reason, objKind, related, update, namespaced := handleObjects(objectT, ns, indx, &plc, config,
				recorder, apiresourcelist, apigroups)
			if update {
				parentUpdate = true
			}
			if objKind != "" {
				kind = objKind
			}
			if namespaced {
				objNamespaced = true
			}
			if names == nil {
				//object template enforced, already handled in handleObjects
				handled = true
			} else {
				enforce = false
				if !compliant {
					numNonCompliant += len(names)
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
			if related != nil {
				for _, object := range related {
					relatedObjects = updateRelatedObjectsStatus(relatedObjects, object)
				}
			}
		}
		if !handled && !enforce {
			objData := map[string]interface{}{
				"indx":        indx,
				"kind":        kind,
				"desiredName": desiredName,
				"namespaced":  objNamespaced,
			}
			statusUpdate := createInformStatus(mustNotHave, numCompliant, numNonCompliant,
				compliantObjects, nonCompliantObjects, &plc, objData)
			if statusUpdate {
				parentUpdate = true
			}
		}
	}
	checkRelatedAndUpdate(parentUpdate, plc, relatedObjects, oldRelated)
}

func checkRelatedAndUpdate(update bool, plc policyv1.ConfigurationPolicy, related,
	oldRelated []policyv1.RelatedObject) {
	sortUpdate := sortRelatedObjectsAndUpdate(&plc, related, oldRelated)
	if update || sortUpdate {
		addForUpdate(&plc)
	}
}

func sortRelatedObjectsAndUpdate(plc *policyv1.ConfigurationPolicy, related,
	oldRelated []policyv1.RelatedObject) (updateNeeded bool) {
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
	if len(oldRelated) == len(related) {
		for i, entry := range oldRelated {
			if gocmp.Equal(entry, related[i]) == false {
				update = true
			}
		}
	} else {
		update = true
	}
	if update {
		(*plc).Status.RelatedObjects = related
	}
	return update
}

func createInformStatus(mustNotHave bool, numCompliant int, numNonCompliant int,
	compliantObjects map[string]map[string]interface{}, nonCompliantObjects map[string]map[string]interface{},
	plc *policyv1.ConfigurationPolicy, objData map[string]interface{}) (updateNeeded bool) {
	update := false
	compliant := false
	desiredName := objData["desiredName"].(string)
	indx := objData["indx"].(int)
	kind := objData["kind"].(string)
	namespaced := objData["namespaced"].(bool)
	if kind == "" {
		return
	}

	if mustNotHave {
		if numNonCompliant > 0 { // We want no resources, but some were found
			//noncompliant; mustnothave and objects exist
			update = createMustNotHaveStatus(kind, nonCompliantObjects, namespaced, plc, indx, compliant)
		} else if numNonCompliant == 0 {
			//compliant; mustnothave and no objects exist
			compliant = true
			update = createMustNotHaveStatus(kind, compliantObjects, namespaced, plc, indx, compliant)
		}
	} else { // !mustNotHave (musthave)
		if numCompliant == 0 && numNonCompliant == 0 { // Special case: No resources found is NonCompliant
			//noncompliant; musthave and objects do not exist
			update = createMustHaveStatus(desiredName, kind, nonCompliantObjects, namespaced,
				plc, indx, compliant)
		} else if numNonCompliant > 0 {
			//noncompliant; musthave and some objects do not exist
			update = createMustHaveStatus(desiredName, kind, nonCompliantObjects, namespaced, plc, indx, compliant)
		} else { // Found only compliant resources (numCompliant > 0 and no NonCompliant)
			//compliant; musthave and objects exist
			compliant = true
			update = createMustHaveStatus("", kind, compliantObjects, namespaced, plc, indx, compliant)
		}
	}

	if update {
		//update parent policy with violation
		eventType := eventNormal
		if !compliant {
			eventType = eventWarning
		}
		if recorder != nil {
			recorder.Event(plc, eventType, fmt.Sprintf(plcFmtStr, plc.GetName()), convertPolicyStatusToString(plc))
		}
	}
	return update
}

func handleObjects(objectT *policyv1.ObjectTemplate, namespace string, index int, policy *policyv1.ConfigurationPolicy,
	config *rest.Config, recorder record.EventRecorder, apiresourcelist []*metav1.APIResourceList,
	apigroups []*restmapper.APIGroupResources) (objNameList []string, compliant bool, reason string,
	rsrcKind string, relatedObjects []policyv1.RelatedObject, pUpdate bool, isNamespaced bool) {
	if namespace != "" {
		fmt.Println(fmt.Sprintf("handling object template [%d] in namespace %s", index, namespace))
	} else {
		fmt.Println(fmt.Sprintf("handling object template [%d] (no namespace specified)", index))
	}
	namespaced := true
	needUpdate := false
	ext := objectT.ObjectDefinition
	mapping, mappingUpdate := getMapping(apigroups, ext, policy, index)
	if mapping == nil {
		return nil, false, "", "", nil, (needUpdate || mappingUpdate), namespaced
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
			needUpdate = true
		}
		return nil, false, "", "", nil, needUpdate, namespaced
	}
	if name != "" {
		exists = objectExists(namespaced, namespace, name, rsrc, unstruct, dclient)
		objNames = append(objNames, name)
	} else if kind != "" {
		objNames = append(objNames, getNamesOfKind(unstruct, rsrc, namespaced,
			namespace, dclient, strings.ToLower(string(objectT.ComplianceType)))...)
		remediation = "inform"
		if len(objNames) == 0 {
			exists = false
		}
	}
	objShouldExist := strings.ToLower(string(objectT.ComplianceType)) != strings.ToLower(string(policyv1.MustNotHave))
	rsrcKind = ""
	reason = ""
	// if the compliance is calculated by the handleSingleObj function, do not override the setting
	complianceCalculated := false
	if len(objNames) == 1 {
		name = objNames[0]
		objNames, compliant, rsrcKind, needUpdate = handleSingleObj(policy, remediation, exists, objShouldExist, rsrc,
			dclient, objectT, map[string]interface{}{
				"name":       name,
				"namespace":  namespace,
				"namespaced": namespaced,
				"index":      index,
				"unstruct":   unstruct,
			})
		complianceCalculated = true
	}

	if complianceCalculated {
		reason = generateSingleObjReason(objShouldExist, compliant, exists)
	} else {
		if !exists && objShouldExist {
			compliant = false
			rsrcKind = rsrc.Resource
			reason = reasonWantFoundDNE
		} else if exists && !objShouldExist {
			compliant = false
			rsrcKind = rsrc.Resource
			reason = reasonWantNotFoundExists
		} else if !exists && !objShouldExist {
			compliant = true
			rsrcKind = rsrc.Resource
			reason = reasonWantNotFoundDNE
		} else if exists && objShouldExist {
			compliant = true
			rsrcKind = rsrc.Resource
			reason = reasonWantFoundExists
		}
	}

	if complianceCalculated {
		// enforce could clear the objNames array so use name instead
		relatedObjects = addRelatedObjects(policy, compliant, rsrc, namespace, namespaced, []string{name}, reason)
	} else {
		relatedObjects = addRelatedObjects(policy, compliant, rsrc, namespace, namespaced, objNames, reason)
	}
	return objNames, compliant, reason, rsrcKind, relatedObjects, needUpdate, namespaced
}

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

func handleSingleObj(policy *policyv1.ConfigurationPolicy, remediation policyv1.RemediationAction, exists bool,
	objShouldExist bool, rsrc schema.GroupVersionResource, dclient dynamic.Interface, objectT *policyv1.ObjectTemplate,
	data map[string]interface{}) (objNameList []string, compliance bool, rsrcKind string, shouldUpdate bool) {
	var err error
	var compliant bool
	updateNeeded := false
	name := data["name"].(string)
	namespace := data["namespace"].(string)
	index := data["index"].(int)
	namespaced := data["namespaced"].(bool)

	compliantObject := map[string]map[string]interface{}{
		namespace: map[string]interface{}{
			"names": []string{name},
		},
	}

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
				glog.Errorf("error handling a existing object `%v` that is a must NOT have according to policy `%v`",
					name, policy.Name)
			}
		} else { //inform
			compliant = false
		}
	}
	if !exists && !objShouldExist {
		//it is a must not have and it does not exist, so it is compliant
		compliant = true
		if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
			glog.V(7).Infof("entering `does not exists` & ` must not have`")
			updateNeeded = createMustNotHaveStatus(rsrc.Resource, compliantObject, namespaced, policy, index, compliant)
		}
	}

	processingErr := false
	specViolation := false

	if exists {
		updated, throwSpecViolation, msg, pErr := updateTemplate(
			strings.ToLower(string(objectT.ComplianceType)),
			data, remediation, rsrc, dclient, unstruct.Object["kind"].(string), nil)
		if !updated && throwSpecViolation {
			specViolation = throwSpecViolation
			compliant = false
		} else if !updated && msg != "" {
			updateNeeded = createViolation(policy, data["index"].(int), "K8s update template error", msg)
		} else if objShouldExist {
			//it is a must have and it does exist, so it is compliant
			compliant = true
			if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Enforce)) {
				glog.V(7).Infof("entering `exists` & ` must have`")
				updateNeeded = createMustHaveStatus("", rsrc.Resource, compliantObject, namespaced, policy, index, compliant)
			}
		}
		processingErr = pErr
	}

	if updateNeeded {
		eventType := eventNormal
		if data["index"].(int) < len(policy.Status.CompliancyDetails) &&
			policy.Status.CompliancyDetails[data["index"].(int)].ComplianceState == policyv1.NonCompliant {
			eventType = eventWarning
			compliant = false
		}
		recorder.Event(policy, eventType, fmt.Sprintf(eventFmtStr, policy.GetName(), name),
			convertPolicyStatusToString(policy))
		return nil, compliant, "", updateNeeded
	}

	if processingErr {
		return nil, false, "", updateNeeded
	}

	if strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Inform)) || specViolation {
		return []string{name}, compliant, rsrc.Resource, updateNeeded
	}

	return nil, compliant, "", false
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
	policy *policyv1.ConfigurationPolicy, index int) (mapping *meta.RESTMapping, update bool) {
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
		return nil, true
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
			recorder.Event(policy, eventWarning, fmt.Sprintf(plcFmtStr, policy.GetName()), errMsg)
		}
		return nil, updateNeeded
	}
	return mapping, updateNeeded
}

func getDetails(unstruct unstructured.Unstructured) (name string, kind string, namespace string) {
	name = ""
	kind = ""
	namespace = ""
	if md, ok := unstruct.Object["metadata"]; ok {

		metadata := md.(map[string]interface{})
		if objectName, ok := metadata["name"]; ok {
			name = strings.TrimSpace(objectName.(string))
		}
		// override the namespace if specified in objectTemplates
		if objectns, ok := metadata["namespace"]; ok {
			glog.V(5).Infof("overriding the namespace as it is specified in objectTemplates...")
			namespace = strings.TrimSpace(objectns.(string))
		}
	}

	if objKind, ok := unstruct.Object["kind"]; ok {
		kind = strings.TrimSpace(objKind.(string))
	}
	return name, kind, namespace
}

func buildNameList(unstruct unstructured.Unstructured, complianceType string,
	resList *unstructured.UnstructuredList) (kindNameList []string) {
	for i := range resList.Items {
		uObj := resList.Items[i]
		match := true
		for key := range unstruct.Object {
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
func getNamesOfKind(unstruct unstructured.Unstructured, rsrc schema.GroupVersionResource,
	namespaced bool, ns string, dclient dynamic.Interface, complianceType string) (kindNameList []string) {
	if namespaced {
		res := dclient.Resource(rsrc).Namespace(ns)
		resList, err := res.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			glog.Error(err)
			return kindNameList
		}
		return buildNameList(unstruct, complianceType, resList)
	}
	res := dclient.Resource(rsrc)
	resList, err := res.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		glog.Error(err)
		return kindNameList
	}
	return buildNameList(unstruct, complianceType, resList)
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
		nameStr := createResourceNameStr([]string{name}, namespace, namespaced)
		if deleted, err = deleteObject(namespaced, namespace, name, rsrc, dclient); !deleted {
			message := fmt.Sprintf("%v %v exists, and cannot be deleted, reason: `%v`", rsrc.Resource, nameStr, err)
			update = createViolation(plc, index, "K8s deletion error", message)
		} else { //deleted successfully
			message := fmt.Sprintf("%v %v existed, and was deleted successfully", rsrc.Resource, nameStr)
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
		nameStr := createResourceNameStr([]string{name}, namespace, namespaced)
		if created, err = createObject(namespaced, namespace, name, rsrc, unstruct, dclient); !created {
			message := fmt.Sprintf("%v %v is missing, and cannot be created, reason: `%v`", rsrc.Resource, nameStr, err)
			update = createViolation(plc, index, "K8s creation error", message)
		} else { //created successfully
			glog.V(8).Infof("entering [%v] created successfully", name)
			message := fmt.Sprintf("%v %v was missing, and was created successfully", rsrc.Resource, nameStr)
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

	nsList, err := (*KubeClient).CoreV1().Namespaces().List(context.TODO(), *listOpt)
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

// objectExists returns true if the object is found
func objectExists(namespaced bool, namespace string, name string, rsrc schema.GroupVersionResource,
	unstruct unstructured.Unstructured, dclient dynamic.Interface) (result bool) {
	exists := false
	if !namespaced {
		res := dclient.Resource(rsrc)
		_, err := res.Get(context.TODO(), name, metav1.GetOptions{})
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
		_, err := res.Get(context.TODO(), name, metav1.GetOptions{})
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

		_, err = res.Create(context.TODO(), &unstruct, metav1.CreateOptions{})
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
		_, err = res.Create(context.TODO(), &unstruct, metav1.CreateOptions{})
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
		err = res.Delete(context.TODO(), name, metav1.DeleteOptions{})
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
		err = res.Delete(context.TODO(), name, metav1.DeleteOptions{})
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
		if !isSorted(x1) {
			sort.Slice(x1, func(i, j int) bool {
				return fmt.Sprintf("%v", x1[i]) < fmt.Sprintf("%v", x1[j])
			})
		}
		x2, ok := x2.([]interface{})
		if !ok {
			return x1
		}
		if len(x2) > len(x1) {
			if ctype != "mustonlyhave" {
				return mergeArrays(x1, x2, ctype)
			}
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
				if ctype != "mustonlyhave" {
					return mergeArrays(x1, x2, ctype)
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
	_, ok := x1.(string)
	if !ok {
		return x1
	}
	return strings.TrimSpace(x1.(string))
}

func isSorted(arr []interface{}) (result bool) {
	arrCopy := append([]interface{}{}, arr...)
	sort.Slice(arr, func(i, j int) bool {
		return fmt.Sprintf("%v", arr[i]) < fmt.Sprintf("%v", arr[j])
	})
	if fmt.Sprint(arrCopy) != fmt.Sprint(arr) {
		return false
	}
	return true
}

func mergeArrays(new []interface{}, old []interface{}, ctype string) (result []interface{}) {
	if ctype == "mustonlyhave" {
		return new
	}
	newCopy := append([]interface{}{}, new...)
	idxWritten := map[int]bool{}
	for i := range newCopy {
		idxWritten[i] = false
	}
	oldItemSet := map[string]map[string]interface{}{}
	for _, val2 := range old {
		if entry, ok := oldItemSet[fmt.Sprint(val2)]; ok {
			oldItemSet[fmt.Sprint(val2)]["count"] = entry["count"].(int) + 1
		} else {
			oldItemSet[fmt.Sprint(val2)] = map[string]interface{}{
				"count": 1,
				"value": val2,
			}
		}
	}

	for _, data := range oldItemSet {
		count := 0
		reqCount := data["count"]
		val2 := data["value"]
		for newIdx, val1 := range newCopy {
			if idxWritten[newIdx] {
				continue
			}
			var mergedObj interface{}
			switch val2 := val2.(type) {
			case map[string]interface{}:
				mergedObj, _ = compareSpecs(val1.(map[string]interface{}), val2, ctype)
			default:
				mergedObj = val1
			}
			if equalObjWithSort(mergedObj, val2) {
				count = count + 1
				new[newIdx] = mergedObj
				idxWritten[newIdx] = true
			}
		}
		if count < reqCount.(int) {
			for i := 0; i < (reqCount.(int) - count); i++ {
				new = append(new, val2)
			}
		}
	}
	return new
}

func compareLists(newList []interface{}, oldList []interface{}, ctype string) (updatedList []interface{}, err error) {
	if ctype != "mustonlyhave" {
		return mergeArrays(newList, oldList, ctype), nil
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

func handleSingleKey(key string, unstruct unstructured.Unstructured, existingObj *unstructured.Unstructured,
	complianceType string) (errormsg string, update bool, merged interface{}, skip bool) {
	var err error
	updateNeeded := false
	if !isDenylisted(key) {
		newObj := formatTemplate(unstruct, key)
		oldObj := existingObj.UnstructuredContent()[key]
		typeErr := ""
		//merge changes into new spec
		var mergedObj interface{}
		switch newObj := newObj.(type) {
		case []interface{}:
			switch oldObj := oldObj.(type) {
			case []interface{}:
				mergedObj, err = compareLists(newObj, oldObj, complianceType)
			case nil:
				mergedObj = newObj
			default:
				typeErr = fmt.Sprintf("Error merging changes into key \"%s\": object type of template and existing do not match",
					key)
			}
		case map[string]interface{}:
			switch oldObj := oldObj.(type) {
			case (map[string]interface{}):
				mergedObj, err = compareSpecs(newObj, oldObj, complianceType)
			case nil:
				mergedObj = newObj
			default:
				typeErr = fmt.Sprintf("Error merging changes into key \"%s\": object type of template and existing do not match",
					key)
			}
		default:
			mergedObj = newObj
		}
		if typeErr != "" {
			return typeErr, false, mergedObj, false
		}
		if err != nil {
			message := fmt.Sprintf("Error merging changes into %s: %s", key, err)
			return message, false, mergedObj, false
		}
		if key == "metadata" {
			oldObj = formatMetadata(oldObj.(map[string]interface{}))
			mergedObj = formatMetadata(mergedObj.(map[string]interface{}))
		}
		if !equalObjWithSort(mergedObj, oldObj) {
			updateNeeded = true
		}
		return "", updateNeeded, mergedObj, false
	}
	return "", false, nil, true
}

func handleKeys(unstruct unstructured.Unstructured, existingObj *unstructured.Unstructured,
	remediation policyv1.RemediationAction, complianceType string, typeStr string, name string,
	res dynamic.ResourceInterface) (success bool, throwSpecViolation bool, message string,
	processingErr bool) {
	var err error
	for key := range unstruct.Object {
		isStatus := key == "status"
		errorMsg, updateNeeded, mergedObj, skipped := handleSingleKey(key, unstruct, existingObj, complianceType)
		if errorMsg != "" {
			return false, false, errorMsg, true
		}
		if mergedObj == nil && skipped {
			continue
		}
		mapMtx := sync.RWMutex{}
		mapMtx.Lock()
		if key == "metadata" {
			existingObj.UnstructuredContent()["metadata"].(map[string]interface{})["annotations"] =
				mergedObj.(map[string]interface{})["annotations"]
			existingObj.UnstructuredContent()["metadata"].(map[string]interface{})["labels"] =
				mergedObj.(map[string]interface{})["labels"]
		} else {
			existingObj.UnstructuredContent()[key] = mergedObj
		}
		mapMtx.Unlock()
		if updateNeeded {
			if (strings.ToLower(string(remediation)) == strings.ToLower(string(policyv1.Inform))) || isStatus {
				return false, true, "", false
			}
			//enforce
			glog.V(4).Infof("Updating %v template `%v`...", typeStr, name)
			_, err = res.Update(context.TODO(), existingObj, metav1.UpdateOptions{})
			if errors.IsNotFound(err) {
				message := fmt.Sprintf("`%v` is not present and must be created", typeStr)
				return false, false, message, true
			}
			if err != nil {
				message := fmt.Sprintf("Error updating the object `%v`, the error is `%v`", name, err)
				return false, false, message, true
			}
			glog.V(4).Infof("Resource `%v` updated\n", name)
		}
	}
	return false, false, "", false
}

func updateTemplate(
	complianceType string, metadata map[string]interface{}, remediation policyv1.RemediationAction,
	rsrc schema.GroupVersionResource, dclient dynamic.Interface,
	typeStr string, parent *policyv1.ConfigurationPolicy) (success bool, throwSpecViolation bool,
	message string, processingErr bool) {
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
	existingObj, err := res.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf(getObjError, name)
	} else {
		return handleKeys(unstruct, existingObj, remediation, complianceType, typeStr, name, res)
	}
	return false, false, "", false
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
	if len(policy.Status.CompliancyDetails) == 0 {
		policy.Status.ComplianceState = "Undetermined"
	} else if compliant {
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
		if EventOnParent != "no" && instance.Status.ComplianceState != "Undetermined" {
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
