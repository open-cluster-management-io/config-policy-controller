[comment]: # ( Copyright Contributors to the Open Cluster Management project )

# Configuration Policy Controller

Open Cluster Management - Configuration Policy Controller

[![Build](https://img.shields.io/badge/build-Prow-informational)](https://prow.ci.openshift.org/?repo=open-cluster-management%2Fconfig-policy-controller)
[![KinD tests](https://github.com/open-cluster-management/config-policy-controller/actions/workflows/kind.yml/badge.svg?branch=main&event=push)](https://github.com/open-cluster-management/config-policy-controller/actions/workflows/kind.yml)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## Description

With the Configuration Policy Controller, you can create `ConfigurationPolicies` to check if the specified objects are present in the cluster. The controller records compliancy details in the `status` of each ConfigurationPolicy, and as Kubernetes Events. If the policy is set to `enforce` the configuration, then the controller will attempt to create, update, or delete objects on the cluster as necessary to match the specified state. The controller can be run as a stand-alone program or as an integrated part of governing risk with the Open Cluster Management project.

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

Following is an example spec of a `ConfigurationPolicy` object:
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

### Templating

Configuration Policies supports inclusion of [Golang text templates](https://golang.org/pkg/text/template/) in  ObjectDefinitions. These templates are resolved at runtime on the target cluster using configuration local to that cluster giving the user the ability to define policies customized to the target cluster. Following custom template functions are available to allow referencing kube-resources on the target cluster.

1. `fromSecret` - returns the value of the specified data key in the  Secret resource
2. `fromConfigMap` - returns the values of the specified data key in the ConfigMap resource.
3. `fromClusterClaim` - returns the value of Spec.Value field in the ClusterClaim resource.
4. `lookup` - a generic lookup function to retreive any kube resource.

Following is an example spec of a `ConfigurationPolicy` object with templates :

```yaml

apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: demo-templates
  namespace: test-templates
spec:
  namespaceSelector:
    exclude:
    - kube-*
    include:
    - default
  object-templates:
  - complianceType: musthave
    objectDefinition:
      kind: ConfigMap
      apiVersion: v1
      metadata:
        name: demo-templates
        namespace: test
      data:
        # Configuration values can be set as key-value properties
        app-name: sampleApp
        app-description: "this is a sample app"
        app-key: '{{ fromSecret "test" "testappkeys" "app-key"  | base64dec }}'
        log-file: '{{ fromConfigMap "test" "logs-config" "log-file" }}'
        appmetrics-url: |
          http://{{ (lookup "v1" "Service" "test" "appmetrics").spec.clusterIP }}:8080
        app-version: version: '{{ fromClusterClaim "version.openshift.io" }}'
  remediationAction: enforce
  severity: low

```


Go to the [Contributing guide](CONTRIBUTING.md) to learn how to get involved.

## Getting started

### Steps for development

  - Build code
    ```bash
    make build
    ```
  - Run controller locally against the Kubernetes cluster currently configured with `kubectl`
    ```bash
    export WATCH_NAMESPACE=<namespace>
    make run
    ```
    (`WATCH_NAMESPACE` can be any namespace on the cluster that you want the controller to monitor for policies.)


### Steps for deployment

  - Build container image
    ```bash
    make build-images
    ```
    - The image registry, name, and tag used in the image build, are configurable with:
      ```bash
      export REGISTRY=''  # (defaults to 'quay.io/open-cluster-management')
      export IMG=''       # (defaults to the repository name)
      export TAG=''       # (defaults to 'latest')
      ```
  - Deploy controller to a cluster

    The controller is deployed to a namespace defined in `CONTROLLER_NAMESPACE` and monitors the namepace defined in `WATCH_NAMESPACE` for `ConfigurationPolicy` resources.

    1. Create the deployment namespaces
       ```bash
       make create-ns
       ```
       The deployment namespaces are configurable with:
       ```bash
       export CONTROLLER_NAMESPACE=''  # (defaults to 'open-cluster-management-agent-addon')
       export WATCH_NAMESPACE=''       # (defaults to 'managed')
       ```
    2. Deploy the controller and related resources
       ```bash
       make deploy
       ```
    **NOTE:** Please be aware of the community's [deployment images](https://github.com/open-cluster-management/community#deployment-images) special note.


### Steps for test

  - Code linting
    ```bash
    make lint
    ```
  - Unit tests
    - Install prerequisites
      ```bash
      make test-dependencies
      ```
    - Run unit tests
      ```bash
      make test
      ```
  - E2E tests
    1. Prerequisites:
       - [docker](https://docs.docker.com/get-docker/)
       - [kind](https://kind.sigs.k8s.io/docs/user/quick-start/)
    2. Start KinD cluster (make sure Docker is running first)
       ```bash
       make kind-bootstrap-cluster-dev
       ```
    3. Start the controller locally (see [Steps for development](#steps-for-development))
    4. Run E2E tests:
       ```bash
       export WATCH_NAMESPACE=managed
       make e2e-test
       ```

## References

- The `config-policy-controller` is part of the `open-cluster-management` community. For more information, visit: [open-cluster-management.io](https://open-cluster-management.io).
- Check the [Security guide](SECURITY.md) if you need to report a security issue.

<!---
Date: 8/18/2021
-->
