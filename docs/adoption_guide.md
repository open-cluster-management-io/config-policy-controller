**Table of Contents**
- [Run sample policy controller locally](#run-sample-policy-controller-locally)
- [Build a local container image](#build-a-local-container)
- [Make your own policy controller](#make-your-own-policy-controller)
  - [Change kind](#change-kind)
  - [Change CRD](#change-crd)
  - [Change CR](#change-cr)
  - [Test new CRD and CR](#test-new-crd-and-cr)
  - [Change the logic of the execution](#change-the-logic-of-the-execution)
  - [Change the test files](#change-the-test-files)
  - [Do integration testing](#do-integration-testing)

## Run sample policy controller locally

To build and run it locally, install operator SDK CLI from https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md.

Make sure to export GO111MODULE=on as it uses go mod as dependency manager.

```bash
export GO111MODULE=on
kubectl apply -f deploy/crds/policies.open-cluster-management.io_configurationpolicies_crd.yaml
operator-sdk run --local --verbose
```
It takes seconds for the sample policy controller to fully start. You will get the message `Waiting for policies to be available for processing...` once it's fully started and watching for policies.

To test a sample policy, open another command prompt to deploy the sample policy file
```
kubectl apply -f deploy/crds/policies.open-cluster-management.io_v1alpha1_configurationpolicy_cr.yaml -n default
```
The local process outputs the following messages
```
{"level":"info","ts":1572447165.453119,"logger":"controller_samplepolicy","msg":"Reconciling SamplePolicy","Request.Namespace":"default","Request.Name":"example-samplepolicy"}
Available policies in namespaces:
namespace = kube-public; policy = example-samplepolicy
namespace = default; policy = example-samplepolicy
```
Check the sample policy resource using `kubectl describe SamplePolicy example-samplepolicy -n default`. The policy controller checks the cluster and reports the compliancy status in the policy.  The status field in the policy is updated with  the compliant status, for example-
```
Status:
  Compliancy Details:
    Example - Samplepolicy:
      Cluster - Wide:
        3 violations detected in namespace `cluster-wide`, there are 0 users violations and 3 groups violations
      Default:
        2 violations detected in namespace `default`, there are 0 users violations and 2 groups violations
      Kube - Public:
        0 violations detected in namespace `kube-public`, there are 0 users violations and 0 groups violations
  Compliant:  NonCompliant
```


## Build a local container image
### Using operator-sdk command
```bash
operator-sdk build ibm/config-policy-ctrl:latest
```

## Make your own policy controller

### Change kind

```bash
# replace `TestPolicy` with the name you want
for file in $(find . -name "*.go" -type f); do  sed -i "" "s/SamplePolicy/TestPolicy/g" $file; done
```
### Change CRD

CRD definition file is located at: [deploy/crds/policies.open-cluster-management.io_samplepolicies_crd.yaml](../deploy/crds/policies.open-cluster-management.io_samplepolicies_crd.yaml)

Change below section to match the kind you specified in previous step.

```yaml
names:
  kind: SamplePolicy
  listKind: SamplePolicyList
  plural: samplepolicies
  singular: samplepolicy
```

### Change CR

A sample CR is located at: [deploy/crds/policies.open-cluster-management.io_v1alpha1_samplepolicy_cr.yaml](../deploy/crds/policies.open-cluster-management.io_v1alpha1_samplepolicy_cr.yaml)

Change below section to match the kind you specified in previous step.

```yaml
kind: SamplePolicy
```

### Test new CRD and CR

Now you have created a new CRD and CR, you can repeat the step [Run sample policy controller locally](#run-sample-policy-controller-locally) to see if the controller is now working with the CRD you have defined.

### Change the logic of the execution

in [samplepolicy_controller.go](../pkg/controller/samplepolicy/samplepolicy_controller.go) you need to change the logic in the function `PeriodicallyExecSamplePolicies`

### Update the test files

Update the test files to test against on your new CRD

### Test again

Now you should have a custom policy controller which checks policy compliancy using desired logic. You can follow the step [Run sample policy controller locally](#run-sample-policy-controller-locally) again to make sure if works.

