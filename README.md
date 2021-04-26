[comment]: # ( Copyright Contributors to the Open Cluster Management project )

# What is the Configuration Policy Controller?

Open Cluster Management - Configuration Policy Controller

[![Build](https://img.shields.io/badge/build-Prow-informational)](https://prow.ci.openshift.org/?repo=open-cluster-management%2Fconfig-policy-controller)
[![KinD tests](https://github.com/open-cluster-management/config-policy-controller/actions/workflows/kind.yml/badge.svg?branch=main&event=push)](https://github.com/open-cluster-management/config-policy-controller/actions/workflows/kind.yml)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## Description

With the Configuration Policy Controller, you can create `ConfigurationPolicies` to check if the specified objects are present in the cluster. The controller records compliancy details in the `status` of each ConfigurationPolicy, and as Kubernetes Events. If the policy is set to `enforce` the configuration, then the controller will attempt to create, update, or delete objects on the cluster as necessary to match the specified state. The controller can be run as a stand-alone program or as an integrated part of governing risk with the Open Cluster Management project.

Go to the [Contributing guide](CONTRIBUTING.md) to learn how to get involved.

This is an example spec of a `ConfigurationPolicy` object:
```yaml
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: policy-pod-example
spec:
  remediationAction: enforce
  severity: low
  namespaceSelector:
    exclude: ["kube-*"]
    include: ["default"]
  object-templates:
    - complianceType: musthave
      objectDefinition:
        apiVersion: v1
        kind: Pod
        metadata:
          name: sample-nginx-pod
        spec:
          containers:
          - image: nginx:1.18.0
            name: nginx
            ports:
            - containerPort: 80
```

The `ConfigurationPolicy` spec includes the following fields:

| Field | Description |
| ---- | ---- |
| severity | Optional: `low`, `medium`, or `high`. |
| remediationAction | Required:  `inform` or `enforce`. Determines what actions the controller will take if the actual state of the object-templates does not match what is desired. |
| namespaceSelector | Optional: an object with `include` and `exclude` lists, specifying where the controller will look for the actual state of the object-templates, if the object is namespaced and not already specified in the object. |
| object-templates | Required: A list of Kubernetes objects that will be checked on the cluster. |

Additionally, each item in the `object-templates` includes these fields:

| Field | Description |
| ---- | ---- |
| complianceType | Required: `musthave`, `mustnothave` or `mustonlyhave`. Determines how to decide if the cluster is compliant with the policy. |
| objectDefinition | Required: A Kubernetes object which must (or must not) match an object on the cluster in order to comply with this policy. |

## Getting started

To run the controller locally, point your CLI to a running cluster and then run:
```
export WATCH_NAMESPACE=cluster_namespace_on_hub
go run cmd/manager/main.go
```

## References

- The `config-policy-controller` is part of the `open-cluster-management` community. For more information, visit: [open-cluster-management.io](https://open-cluster-management.io).
- Check the [Security guide](SECURITY.md) if you need to report a security issue.
