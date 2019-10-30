# multicloud-operators-policy-controller
An operator based policy framework sample controller. 

## Launch Dev mode

To build and run it locally, install operator SDK CLI from https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md. 

Make sure to export GO111MODULE=on as it uses go mod as dependency manager.

```bash
GO111MODULE=on
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
Check the sample policy resource using `kubectl describe SamplePolicy example-samplepolicy -n default`. Its status indicates the compliant status, for example-
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

