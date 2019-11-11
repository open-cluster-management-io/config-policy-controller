**Table of Contents**
- [Run sample policy controller locally](#run-sample-policy-controller-locally)
- [Build a local container image](#build-a-local-container)
- [Develop new policy controller](#develop-new-policy-controller)
    - [Make sure your file names are correct](#make-sure-your-file-names-are-correct)
    - [Change your CRD](#change-your-crd)
    - [Change your type](#change-your-type)
    - [Generate the code based on your changes](#generate-the-code-based-on-your-changes)
    - [Change the logic of the execution](#change-the-logic-of-the-execution)
    - [Change the test files](#change-the-test-files)
    - [Do integration testing](#do-integration-testing)

## Run sample policy controller locally

To build and run it locally, install operator SDK CLI from https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md.

Make sure to export GO111MODULE=on as it uses go mod as dependency manager.

```bash
export GO111MODULE=on
kubectl apply -f deploy/crds/policies.ibm.com_samplepolicies_crd.yaml
operator-sdk up local --verbose
```
It takes seconds for the sample policy controller to fully start. You will get the message `Waiting for policies to be available for processing...` once it's fully started and watching for policies.

To test a sample policy, open another command prompt to deploy the sample policy file
```
kubectl apply -f deploy/crds/policies.ibm.com_v1alpha1_samplepolicy_cr.yaml -n default
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
```bash
operator-sdk build ibm/multicloud-operators-policy-controller:latest
```

## Develop new policy controller

### Make sure your file names are correct

```
for file in $(find . -name "*.go" -type f); do  sed -i "" "s/multicloud-operators-policy-controller/<your-git-repo-name>/g" $file; done
```
### Change your CRD

your CRD definition is located in:

config/crds/

you can change the `Kind`.
Note if you wish to change the API goup `mcm.ibm.com` you would also need to change it in: `pkg/apis/mcm/v1alpha1/register.go`


you can double check where you need to make changes by searching for the old `Kind`

```
find . -type f -exec grep -H 'grcpolicy' {} \;
```

### Change your type

You may change your type `Specs` and `Status` if needed here: `pkg/apis/policies/v1alpha1/samplepolicy_types.go`

make sure you don't leave traces of the old `type`

### Generate the code based on your changes

```
make install

make run
```

### Change the logic of the execution


in `pkg/controller/samplepolicy/samplepolicy_controller.go` you need to change the logic in the function `PeriodicallyExecSamplePolicies`

### Change the test files

create different objects based on your new CRD in the test files

### Do integration testing
Do integration testing to make sure everything is running

`make run`

Use the files in the `config/samples` to test the controller behavior with respect to the CR (Custom Resource) instance creation