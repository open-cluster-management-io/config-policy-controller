[comment]: # ( Copyright Contributors to the Open Cluster Management project )

**Table of Contents**
- [Run configuration policy controller locally](#run-configuration-policy-controller-locally)
- [Build a local container image](#build-a-local-container)
- [Make your own policy controller](#make-your-own-policy-controller)
  - [Change kind](#change-kind)
  - [Change CRD](#change-crd)
  - [Change CR](#change-cr)
  - [Test new CRD and CR](#test-new-crd-and-cr)
  - [Change the logic of the execution](#change-the-logic-of-the-execution)
  - [Change the test files](#change-the-test-files)
  - [Do integration testing](#do-integration-testing)

## Run configuration policy controller locally

To build and run it locally, install operator SDK CLI from https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md.

Make sure to export GO111MODULE=on as it uses go mod as dependency manager.

```bash
export GO111MODULE=on
kubectl apply -f deploy/crds/policy.open-cluster-management.io_configurationpolicies_crd.yaml
operator-sdk run --local --verbose
```
It takes seconds for the configuration policy controller to fully start. You will get the message `Waiting for policies to be available for processing...` once it's fully started and watching for policies.

To test a configuration policy, open another command prompt to deploy the configuration policy file
```
kubectl apply -f deploy/crds/policy.open-cluster-management.io_v1_configurationpolicy_cr.yaml -n default
```
The local process outputs the following messages
```
{"level":"info","ts":1572447165.453119,"logger":"controller_configurationpolicy","msg":"Reconciling configurationPolicy","Request.Namespace":"default","Request.Name":"example-configurationpolicy"}
Available policies in namespaces:
namespace = kube-public; policy = example-configurationpolicy
namespace = default; policy = example-configurationpolicy
```
Check the configuration policy resource using `kubectl describe configurationPolicy example-configurationpolicy -n default`. The policy controller checks the cluster and reports the compliancy status in the policy.  The status field in the policy is updated with  the compliant status, for example-
```
status:
  compliancyDetails:
  - Compliant: NonCompliant
    Validity: {}
    conditions:
    - lastTransitionTime: "2020-05-08T15:53:28Z"
      message: roles `pod-reader-thur` does not exist as specified, and should be
        created
      reason: K8s missing a must have object
      status: "True"
      type: violation
  compliant: NonCompliant
```