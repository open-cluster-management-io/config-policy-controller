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
package samplepolicy

import (
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	policiesv1alpha1 "github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/apis/policies/v1alpha1"
	"github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/common"
	"golang.org/x/net/context"
	coretypes "k8s.io/api/core/v1"
	sub "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	testclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
	"time"
)

var c client.Client
var mgr manager.Manager
var err error

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo", Namespace: "default"}}
var depKey = types.NamespacedName{Name: "foo", Namespace: "default"}

const timeout = time.Second * 5

func TestReconcile(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	instance := &policiesv1alpha1.SamplePolicy{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"}}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err = manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()
	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	t.Logf("requests %v", requests)
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())
	stopMgr, mgrStopped := StartTestManager(mgr, g)
	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), instance)
	// The instance object may not be a valid object because it might be missing some required fields.
	// Please modify the instance object by adding required fields and then remove the following if statement.
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Eventually(func() error { return c.Get(context.TODO(), depKey, instance) }, timeout).
		Should(gomega.Succeed())
}


func TestCheckUnNamespacedPolicies(t *testing.T) {
	var simpleClient kubernetes.Interface
	simpleClient = testclient.NewSimpleClientset()
	common.Initialize(&simpleClient, nil)
	var samplePolicy = policiesv1alpha1.SamplePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		}}

	var policies = map[string]*policiesv1alpha1.SamplePolicy{}
	policies["policy1"] = &samplePolicy

	err := checkUnNamespacedPolicies(policies)
	assert.Nil(t, err)
}

func TestEnsureDefaultLabel(t *testing.T) {
	updateNeeded := ensureDefaultLabel(&samplePolicy)
	assert.True(t, updateNeeded)

	var labels1 = map[string]string{}
	labels1["category"] = grcCategory
	samplePolicy.Labels = labels1
	updateNeeded = ensureDefaultLabel(&samplePolicy)
	assert.False(t, updateNeeded)

	var labels2 = map[string]string{}
	labels2["category"] = "foo"
	samplePolicy.Labels = labels2
	updateNeeded = ensureDefaultLabel(&samplePolicy)
	assert.True(t, updateNeeded)

	var labels3 = map[string]string{}
	labels3["foo"] = grcCategory
	samplePolicy.Labels = labels3
	updateNeeded = ensureDefaultLabel(&samplePolicy)
	assert.True(t, updateNeeded)
}

func TestCheckAllClusterLevel(t *testing.T) {
	var subject =  sub.Subject {
		APIGroup: "",
		Kind: "User",
		Name: "user1",
		Namespace: "default",
	}
	var subjects = []sub.Subject{}
	subjects = append(subjects, subject)
	var clusterRoleBinding = sub.ClusterRoleBinding{
		Subjects: subjects,
	}
	var items = []sub.ClusterRoleBinding{}
	items = append(items, clusterRoleBinding)
	var clusterRoleBindingList = sub.ClusterRoleBindingList{
		Items: items,
	}
	var users, groups = checkAllClusterLevel(&clusterRoleBindingList)
	assert.Equal(t, 1, users)
	assert.Equal(t, 0, groups)
}

func TestCheckViolationsPerNamespace(t *testing.T) {
	var subject =  sub.Subject {
		APIGroup: "",
		Kind: "User",
		Name: "user1",
		Namespace: "default",
	}
	var subjects = []sub.Subject{}
	subjects = append(subjects, subject)
	var roleBinding = sub.RoleBinding{
		Subjects: subjects,
	}
	var items = []sub.RoleBinding{}
	items = append(items, roleBinding)
	var roleBindingList = sub.RoleBindingList{
		Items: items,
	}
	var samplePolicySpec = policiesv1alpha1.SamplePolicySpec{
		MaxRoleBindingUsersPerNamespace:  1,
		MaxRoleBindingGroupsPerNamespace: 1,
		MaxClusterRoleBindingUsers:       1,
		MaxClusterRoleBindingGroups:      1,
	}
	samplePolicy.Spec = samplePolicySpec
	checkViolationsPerNamespace(&roleBindingList, &samplePolicy, "default")
}

func TestCreateParentPolicy(t *testing.T) {
	var ownerReference = metav1.OwnerReference{
		Name: "foo",
	}
	var ownerReferences = []metav1.OwnerReference{}
	ownerReferences = append(ownerReferences, ownerReference)
	samplePolicy.OwnerReferences = ownerReferences

	policy := createParentPolicy(&samplePolicy)
	assert.NotNil(t, policy)
	createParentPolicyEvent(&samplePolicy)
}

func TestConvertPolicyStatusToString(t *testing.T) {
	var compliantDetail = map[string][]string{}
	var compliantDetails = map[string]map[string][]string{}
	details := []string{}

	details = append(details, "detail1")
	details = append(details, "detail2")

	compliantDetail["w"] = details
	compliantDetails["a"] = compliantDetail
	compliantDetails["b"] = compliantDetail
	compliantDetails["c"] = compliantDetail
	samplePolicyStatus := policiesv1alpha1.SamplePolicyStatus {
		ComplianceState: "Compliant",
		CompliancyDetails: compliantDetails,
	}
	samplePolicy.Status = samplePolicyStatus
	var policyInString = convertPolicyStatusToString(&samplePolicy)
	assert.NotNil(t, policyInString)
	checkComplianceChangeBasedOnDetails(&samplePolicy)
	checkComplianceBasedOnDetails(&samplePolicy)
	addViolationCount(&samplePolicy, 1, 1, "default")
}

func TestDeleteExternalDependency(t *testing.T) {
	mgr, err = manager.New(cfg, manager.Options{})
	reconcileSamplePolicy := ReconcileSamplePolicy{client: mgr.GetClient(), scheme: mgr.GetScheme(), recorder: mgr.GetEventRecorderFor("samplepolicy-controller")}
	reconcileSamplePolicy.deleteExternalDependency(&samplePolicy)
}

func TestHandleAddingPolicy(t *testing.T) {
	var simpleClient kubernetes.Interface
	simpleClient = testclient.NewSimpleClientset()
	common.Initialize(&simpleClient, nil)
	err := handleAddingPolicy(&samplePolicy)
	assert.Nil(t, err)
	handleRemovingPolicy(&samplePolicy)
}

func TestGetContainerID(t *testing.T) {
	var containerStateWaiting = coretypes.ContainerStateWaiting{
		Reason: "unknown",
	}
	var containerState = coretypes.ContainerState{
		Waiting: &containerStateWaiting,
	}
	var containerStatus = coretypes.ContainerStatus{
		State: containerState,
		ContainerID: "id",
	}
	var containerStatuses []coretypes.ContainerStatus
	containerStatuses = append(containerStatuses, containerStatus)
	var podStatus = coretypes.PodStatus{
		ContainerStatuses: containerStatuses,
	}
	var pod = coretypes.Pod{
		Status: podStatus,
	}
	getContainerID(pod, "foo")
}



