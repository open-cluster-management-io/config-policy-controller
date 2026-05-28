[comment]: # ( Copyright Contributors to the Open Cluster Management project )

# Configuration Policy Controller

Open Cluster Management - Configuration Policy Controller

[![KinD tests](https://github.com/open-cluster-management-io/config-policy-controller/actions/workflows/kind.yml/badge.svg?branch=main&event=push)](https://github.com/open-cluster-management-io/config-policy-controller/actions/workflows/kind.yml)
[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)

## Policy Controllers Overview

### Configuration Policy Controller

With the Configuration Policy Controller, you can create `ConfigurationPolicies` to check if the specified objects are present in the cluster. The controller records compliancy details in the `status` of each ConfigurationPolicy, and as Kubernetes Events. If the policy is set to `enforce` the configuration, then the controller will attempt to create, update, or delete objects on the cluster as necessary to match the specified state. The controller can be run as a stand-alone program or as an integrated part of governing risk with the Open Cluster Management project.

The `ConfigurationPolicy` spec includes the following fields:

| Field | Description |
| ---- | ---- |
| severity | Optional: `low`, `medium`, `high`, or `critical`. |
| remediationAction | Required:  `inform` or `enforce`. Determines what actions the controller will take if the actual state of the object-templates does not match what is desired. |
| namespaceSelector | Optional: an object with `include` and `exclude` lists, specifying where the controller will look for the actual state of the object-templates, if the object is namespaced and not already specified in the object. |
| object-templates | Recommended: A list of Kubernetes objects that will be checked on the cluster. Keys inside of the objectDefinition may point to values that have Go templates. Only one of `object-templates` and `object-templates-raw` may be set in a configuration policy. |
| object-templates-raw | For advanced use cases: A raw template string for Go templating such as `range` loops and `if` conditionals. Only one of `object-templates` and `object-templates-raw` may be set in a configuration policy. |

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

#### Templating

Configuration Policies supports inclusion of [Golang text templates](https://golang.org/pkg/text/template/) in  ObjectDefinitions. These templates are resolved at runtime on the target cluster using configuration local to that cluster giving the user the ability to define policies customized to the target cluster. Following custom template functions are available to allow referencing kube-resources on the target cluster.

1. `fromSecret` - returns the value of the specified data key in the  Secret resource
2. `fromConfigMap` - returns the values of the specified data key in the ConfigMap resource.
3. `fromClusterClaim` - returns the value of Spec.Value field in the ClusterClaim resource.
4. `lookup` - a generic lookup function to retreive any kube resource.

Following is an example spec of a `ConfigurationPolicy` object with templates:

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

#### Configuration policy status details

Below are two examples of `ConfigurationPolicy` statuses. The first example policy is `Compliant` and the second example is `NonCompliant`.

After the policy evaluates, the `status` field contains information about the current state and compliance history. This example `ConfigurationPolicy` in `enforce` mode creates a namespace `test-namespace` on the cluster `local-cluster`:

```yaml
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: policy-example-namespace
spec:
  remediationAction: enforce
  severity: low
  object-templates:
    - objectDefinition:
        apiVersion: v1
        kind: Namespace
        metadata:
          name: test-namespace
      complianceType: musthave
status:
  compliancyDetails:
    - Compliant: Compliant
      Validity: {}
      conditions:
        - lastTransitionTime: "2026-05-28T16:03:54Z"
          message: namespaces [test-namespace] found as specified
          reason: K8s `must have` object already exists
          status: "True"
          type: notification
  compliant: Compliant
  history:
    - lastTimestamp: "2026-05-28T16:03:54.128152Z"
      message: Compliant; notification - namespaces [test-namespace] found as specified
    - lastTimestamp: "2026-05-28T16:03:49.058084Z"
      message: Compliant; notification - namespaces [test-namespace] was created successfully
    - lastTimestamp: "2026-05-28T15:55:26.553841Z"
      message: NonCompliant; violation - namespaces [test-namespace] not found
  lastEvaluated: "2026-05-28T16:03:54Z"
  lastEvaluatedGeneration: 2
  relatedObjects:
    - compliant: Compliant
      object:
        apiVersion: v1
        kind: Namespace
        metadata:
          name: test-namespace
      properties:
        createdByPolicy: true
        uid: 9bc52fa6-3d7d-45ad-bb3b-137e06bc539b
      reason: Resource found as expected
      cluster: local-cluster
```

This example `ConfigurationPolicy` in `inform` mode detects a mismatch between the desired `purpose: load-test` and actual `purpose: load-testing` data in a `ConfigMap`:

```yaml
apiVersion: policy.open-cluster-management.io/v1
kind: ConfigurationPolicy
metadata:
  name: policy-example-configmap
spec:
  object-templates:
    - objectDefinition:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: example-configmap
          namespace: default
        data:
          env: dev
          purpose: load-test
      complianceType: musthave
      recordDiff: InStatus
  remediationAction: inform
status:
  compliancyDetails:
    - Compliant: NonCompliant
      Validity: {}
      conditions:
        - lastTransitionTime: "2026-05-28T16:18:34Z"
          message: configmaps [example-configmap] found but not as specified in namespace default
          reason: K8s does not have a `must have` object
          status: "True"
          type: violation
  compliant: NonCompliant
  history:
    - lastTimestamp: "2026-05-28T16:23:14.639170Z"
      message: NonCompliant; violation - configmaps [example-configmap] found but not as specified in namespace default
    - lastTimestamp: "2026-05-28T16:18:34.733058Z"
      message: NonCompliant; violation - configmaps [example-configmap] found but not as specified in namespace default
  lastEvaluated: "2026-05-28T16:23:14Z"
  lastEvaluatedGeneration: 2
  relatedObjects:
    - compliant: NonCompliant
      object:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: example-configmap
          namespace: default
      properties:
        createdByPolicy: false
        diff: |
          --- default/example-configmap : existing
          +++ default/example-configmap : updated
          @@ -1,9 +1,9 @@
           apiVersion: v1
           data:
             env: dev
          -  purpose: load-testing
          +  purpose: load-test
           kind: ConfigMap
           metadata:
             creationTimestamp: "2026-05-28T16:17:54Z"
             name: example-configmap
             namespace: default
        uid: f706c4be-5b68-4592-a81e-d9be6d8fd25b
      reason: Resource found but does not match
      cluster: local-cluster
```

##### Status field descriptions

| Field | Description |
| ---- | ---- |
| `compliant` | Overall compliance state: `Compliant` (all templates match), `NonCompliant` (one or more templates do not match), or `Terminating` (policy is being deleted) |
| `lastEvaluated` | ISO 8601 timestamp of the most recent evaluation |
| `lastEvaluatedGeneration` | The generation of the ConfigurationPolicy resource at the last evaluation |
| `compliancyDetails` | Array with one entry per object-template, showing compliance state and violation details |
| `relatedObjects` | Array of all Kubernetes objects matched by the policy templates |
| `history` | Timestamped messages showing the recent compliance state changes |

##### Related object properties

| Field | Description |
| ---- | ---- |
| `createdByPolicy` | When set to `true`, indicates that the controller created this object. This is important for pruning behavior. |
| `uid` | Used internally to track objects. |
| `diff` | Shows the differences between the policy `objectDefinition` and the actual cluster object. |
| `matchesAfterDryRun` | When set to `true`, indicates that an object initially did not match the policy, but a dry-run update produced a compliant result. The dry-run update can treat empty and null values as equivalent, so they are not reported as mismatches. This property may also be `true` if API server webhooks during a dry-run update produced a compliant object. |

> [!NOTE]
> For some sensitive resources, the `diff` is hidden by default. To always display the `diff` in the status, set the `recordDiff` field on the `object-template` to `InStatus`.

### Operator Policy Controller

With the Operator Policy Controller, you can create `OperatorPolicy` resources to manage operators deployed by the Operator Lifecycle Manager (OLM). The controller automates operator lifecycle management, including installation, upgrades, and removal. It monitors operator health by tracking subscription status, cluster service versions, and deployments, then records compliance details in the `status` of each OperatorPolicy and as Kubernetes Events. If the policy is set to `enforce`, the controller automatically approves install and upgrade plans according to your configuration.

The `OperatorPolicy` spec includes the following fields:

| Field | Description |
| ---- | ---- |
| severity | Optional: `low`, `medium`, `high`, or `critical`. Defines the severity level when the policy is noncompliant. |
| remediationAction | Required: `inform` or `enforce`. Determines what actions the controller will take if the operator is not in the desired state. |
| complianceType | Required: `musthave` or `mustnothave`. Determines if the operator must be installed or must not be installed. |
| subscription | Required: An Operator Lifecycle Manager (OLM) `Subscription` resource specification for the operator. `subscription.spec.name` is required. Any other spec fields will use default values if not specified. Do NOT include `subscription.spec.installPlanApproval` - see `upgradeApproval` below.|
| operatorGroup | Optional: An OLM `OperatorGroup` resource specification. If not specified and no OperatorGroup exists in the namespace, the controller creates an `AllNamespaces` type OperatorGroup by default. |
| versions | Optional: A list of templatable ClusterServiceVersion names that specifies which installed ClusterServiceVersion names are compliant when in `inform` mode and which `InstallPlans` are approved when in `enforce` mode. Multiple versions can be provided in one entry by separating them with commas. An empty list approves all versions. |
| upgradeApproval | Required: `None` or `Automatic`. Specifies whether to automatically approve operator upgrade install plans when the policy is enforced and in `musthave` mode. The initial InstallPlan approval is not affected by this setting. |
| removalBehavior | Optional: Defines resource cleanup behavior when the policy is set to `mustnothave`. |
| complianceConfig | Optional: Defines how resource conditions affect overall policy compliance. |

#### Removal behavior

The `removalBehavior` field controls which resources the controller removes when the policy is set to `mustnothave` and `enforce` mode. All sub-fields are optional and have default values applied when not specified.

| Sub-field | Description | Default |
| ---- | ---- | ---- |
| subscriptions | Whether to delete the `Subscription` resource. Valid values: `Keep`, `Delete`. | `Delete` |
| operatorGroups | Whether to delete the `OperatorGroup` resource. Valid values: `Keep`, `DeleteIfUnused`. Only deletes the OperatorGroup if no other resources use it. | `DeleteIfUnused` |
| clusterServiceVersions | Whether to delete the `ClusterServiceVersion` resource. Valid values: `Keep`, `Delete`. | `Delete` |
| customResourceDefinitions | Whether to delete `CustomResourceDefinitions` associated with the operator. Valid values: `Keep`, `Delete`. Defaults to `Keep` because deleting CRDs should be done deliberately. | `Keep` |

#### Compliance configuration

The `complianceConfig` field defines how different resource conditions affect the overall operator policy compliance state. All sub-fields are optional and have default values applied when not specified.

| Sub-field | Description | Default |
| ---- | ---- | ---- |
| catalogSourceUnhealthy | How to report the policy when the `CatalogSource` is unhealthy. Valid values: `Compliant`, `NonCompliant`. | `Compliant` |
| deploymentsUnavailable | How to report the policy when operator deployments are unavailable. Valid values: `Compliant`, `NonCompliant`. | `NonCompliant` |
| upgradesAvailable | How to report the policy when operator upgrades are available through `InstallPlan`. Valid values: `Compliant`, `NonCompliant`. | `Compliant` |
| deprecationsPresent | How to report the policy when deprecated features are detected in the operator. Valid values: `Compliant`, `NonCompliant`. | `Compliant` |
| minorChannelUpgradeAvailable | How to report the policy when a newer minor version channel is available when `upgradeApproval` is `Automatic`. Valid values: `Compliant`, `NonCompliant`. | `Compliant` |

Following is an example spec of an `OperatorPolicy` object to install the external secrets operator at version 0.11.0. The ESO operator was chosen randomly out of the operators available in the OperatorHub catalog.

Notice the `upgradeApproval` is set to `None` and the `versions` list only contains `external-secrets-operator.v0.11.0`. This means the policy will install version 0.11.0 and will not automatically approve any upgrades for the operator.

```yaml
apiVersion: policy.open-cluster-management.io/v1beta1
kind: OperatorPolicy
metadata:
  name: policy-eso
spec:
  remediationAction: enforce
  severity: medium
  complianceType: musthave
  upgradeApproval: None
  operatorGroup:
    namespace: default
    name: external-secrets-operator-group
    targetNamespaces:
      - default
  subscription:
    namespace: default
    name: external-secrets-operator
    channel: alpha
    source: operatorhubio-catalog
    sourceNamespace: olm
    startingCSV: external-secrets-operator.v0.11.0
  versions:
    - external-secrets-operator.v0.11.0
```

#### Operator policy templating

The Operator Policy Controller also supports Golang text templates in the specification. This allows you to customize operator subscriptions based on cluster-specific configuration at runtime.

Following is an example spec of an `OperatorPolicy` object with templates. With `remediationAction` set to `inform`, the policy will detect whether an external secrets operator deployed in the namespace `my-namespace` matches the policy spec. It will read the `operatorGroup.targetNamespaces` and the `subscription.channel` from a ConfigMap `my-operator-configs` located in the namespace `my-namespace`.

```yaml
apiVersion: policy.open-cluster-management.io/v1beta1
kind: OperatorPolicy
metadata:
  name: eso-operator-with-templates
spec:
  remediationAction: inform
  severity: medium
  complianceType: musthave
  upgradeApproval: None
  operatorGroup:
    name: my-operator-group
    namespace: my-namespace
    targetNamespaces: '{{ (fromConfigMap "my-namespace" "my-operator-configs" "namespaces") | toLiteral }}'
  subscription:
    channel: '{{ (lookup "v1" "ConfigMap" "my-namespace" "my-operator-configs").data.channel }}'
    name: external-secrets-operator
    namespace: my-namespace
    source: operatorhubio-catalog
    sourceNamespace: olm
    startingCSV: external-secrets-operator.v0.11.0
  versions:
    - external-secrets-operator.v0.11.0
```

For more information about templating, see the Configuration Policy Controller [Templating](#templating) section above.


#### Operator policy status details

After the policy evaluates, the `status` field contains the current compliance state and supporting details. A compliant `OperatorPolicy` matches the desired Operator configuration, and a noncompliant `OperatorPolicy` indicates one or more requirements are not met. For more info about noncompliant policies, see [Condition types](#condition-types) below.

<details>
<summary>This example shows a compliant <code>status</code> for an <code>OperatorPolicy</code> in <code>enforce</code> mode that installs the External Secrets Operator at version 0.11.0:</summary>

```yaml
status:
  compliant: Compliant
  conditions:
  - lastTransitionTime: "2026-05-28T17:05:37Z"
    message: CatalogSource was found
    reason: CatalogSourcesFound
    status: "False"
    type: CatalogSourcesUnhealthy
  - lastTransitionTime: "2026-05-28T17:11:40Z"
    message: ClusterServiceVersion (external-secrets-operator.v0.11.0) - install strategy
      completed with no errors
    reason: InstallSucceeded
    status: "True"
    type: ClusterServiceVersionCompliant
  - lastTransitionTime: "2026-05-28T17:11:40Z"
    message: Compliant; the policy spec is valid, the OperatorGroup matches what is
      required by the policy, the Subscription matches what is required by the policy,
      no InstallPlans requiring approval were found, ClusterServiceVersion (external-secrets-operator.v0.11.0)
      - install strategy completed with no errors, there are CRDs present for the
      operator, all operator Deployments have their minimum availability, CatalogSource
      was found
    reason: Compliant
    status: "True"
    type: Compliant
  - lastTransitionTime: "2026-05-28T17:11:07Z"
    message: there are CRDs present for the operator
    reason: RelevantCRDFound
    status: "True"
    type: CustomResourceDefinitionCompliant
  - lastTransitionTime: "2026-05-28T17:11:40Z"
    message: all operator Deployments have their minimum availability
    reason: DeploymentsAvailable
    status: "True"
    type: DeploymentCompliant
  - lastTransitionTime: "2026-05-28T17:11:19Z"
    message: no InstallPlans requiring approval were found
    reason: NoInstallPlansRequiringApproval
    status: "True"
    type: InstallPlanCompliant
  - lastTransitionTime: "2026-05-28T17:11:07Z"
    message: The requested package, channel, and bundle are all at the recommended
      versions
    reason: Recommended
    status: "True"
    type: NoDeprecations
  - lastTransitionTime: "2026-05-28T17:10:43Z"
    message: the OperatorGroup matches what is required by the policy
    reason: OperatorGroupMatches
    status: "True"
    type: OperatorGroupCompliant
  - lastTransitionTime: "2026-05-28T17:10:43Z"
    message: the Subscription matches what is required by the policy
    reason: SubscriptionMatches
    status: "True"
    type: SubscriptionCompliant
  - lastTransitionTime: "2026-05-28T17:00:12Z"
    message: the policy spec is valid
    reason: PolicyValidated
    status: "True"
    type: ValidPolicySpec
  observedGeneration: 3
  relatedObjects:
  - compliant: Compliant
    object:
      apiVersion: operators.coreos.com/v1alpha1
      kind: CatalogSource
      metadata:
        name: operatorhubio-catalog
        namespace: olm
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: operators.coreos.com/v1alpha1
      kind: ClusterServiceVersion
      metadata:
        name: external-secrets-operator.v0.11.0
        namespace: default
    properties:
      uid: d020388e-6222-481f-a742-39248d90fc76
    reason: InstallSucceeded
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: acraccesstokens.generators.external-secrets.io
    properties:
      uid: cab9dc08-ae40-4a3b-a0bd-2088a3f764e6
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: clusterexternalsecrets.external-secrets.io
    properties:
      uid: be2f7926-6baa-41c4-b746-01c72094c28a
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: clustergenerators.generators.external-secrets.io
    properties:
      uid: 50678232-4fc5-4bf9-b2e0-8e77c068d2d0
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: clustersecretstores.external-secrets.io
    properties:
      uid: 88b386e7-7068-4482-b2a8-f9227ebd108e
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: ecrauthorizationtokens.generators.external-secrets.io
    properties:
      uid: 80b5c439-6b82-41c3-8233-b3e4df306b4c
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: externalsecrets.external-secrets.io
    properties:
      uid: 3d9b309e-2aed-4fb2-a17e-d8467ef74dcc
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: fakes.generators.external-secrets.io
    properties:
      uid: c151c395-7d25-4aa6-9715-4148598b4efe
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: gcraccesstokens.generators.external-secrets.io
    properties:
      uid: c3b14e91-8799-43c8-ad3d-456a2cd1e945
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: githubaccesstokens.generators.external-secrets.io
    properties:
      uid: 9b459690-b22b-4ab3-9989-c7438cf7e09c
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: operatorconfigs.operator.external-secrets.io
    properties:
      uid: 8a2048a7-1bba-4fbf-90ef-f982650c0ab0
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: passwords.generators.external-secrets.io
    properties:
      uid: bccf67d0-bd62-41a0-8f88-beb97a852a3e
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: pushsecrets.external-secrets.io
    properties:
      uid: 8333184d-4cc0-4eff-b196-7cf20a73d3bc
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: secretstores.external-secrets.io
    properties:
      uid: 4beca284-7b40-4c2d-b84c-ccac4f68f0fa
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: stssessiontokens.generators.external-secrets.io
    properties:
      uid: 5ffe9c32-70dd-4bd9-b246-710a154d6eb1
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: uuids.generators.external-secrets.io
    properties:
      uid: 25428081-ebef-446a-9d18-715f7284594b
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: vaultdynamicsecrets.generators.external-secrets.io
    properties:
      uid: 1b579ffb-5c7f-4c5e-8593-6e004a1ec14e
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apiextensions.k8s.io/v1
      kind: CustomResourceDefinition
      metadata:
        name: webhooks.generators.external-secrets.io
    properties:
      uid: 56681564-c87e-4e15-b70f-23ae7f940afc
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: external-secrets-operator-controller-manager
        namespace: default
    properties:
      uid: 0692327d-e248-431b-8552-ac9bdc84c518
    reason: Deployment Available
  - compliant: Compliant
    object:
      apiVersion: operators.coreos.com/v1alpha1
      kind: InstallPlan
      metadata:
        name: install-8k5n4
        namespace: default
    properties:
      uid: 48688188-15ce-4ba7-8974-1630b53b9378
    reason: The InstallPlan is Complete
  - compliant: Compliant
    object:
      apiVersion: operators.coreos.com/v1
      kind: OperatorGroup
      metadata:
        name: external-secrets-operator-group
        namespace: default
    properties:
      createdByPolicy: true
      uid: c637e20b-fbac-4c35-acdf-2747a9f7f61d
    reason: Resource found as expected
  - compliant: Compliant
    object:
      apiVersion: operators.coreos.com/v1alpha1
      kind: Subscription
      metadata:
        name: external-secrets-operator
        namespace: default
    properties:
      createdByPolicy: true
      uid: ac733bd1-3ffe-496c-8be9-60b2cbc7baa5
    reason: Resource found as expected
  resolvedSubscriptionLabel: external-secrets-operator.default
```

</details>

##### Status field descriptions

| Field | Description |
| ---- | ---- |
| `compliant` | Overall compliance state: `Compliant` (operator meets all requirements), `NonCompliant` (operator does not meet requirements), or `Terminating` (policy is being deleted) |
| `observedGeneration` | The generation of the `OperatorPolicy` resource when it was last observed |
| `conditions` | Detailed status conditions that track different aspects of `OperatorPolicy` compliance. For more information, see [Condition types](#condition-types). |
| `relatedObjects` | Kubernetes resources associated with the evaluated operator, such as `Subscription`, `ClusterServiceVersion`, `OperatorGroup`, and `Deployment` objects. |
| `resolvedSubscriptionLabel` | The resolved `name.namespace` of the `Subscription` resource |
| `overlappingPolicies` | List of other `OperatorPolicy` resources that manage the same subscription. Use this field to identify conflicting policies. |
| `subscriptionInterventionTime` | Timestamp indicating when the policy will intervene on a stuck subscription. A future timestamp means the controller is waiting for resolution. |

##### Condition types

The following condition types appear in a typical compliant status (see the example above). Additional conditions such as `MinorChannelUpgradeAvailable` may appear depending on compliance configuration. For more information, see [Compliance configuration](#compliance-configuration).

- `ValidPolicySpec`: The policy spec is valid.
- `OperatorGroupCompliant`: The `OperatorGroup` matches what is required by the policy.
- `SubscriptionCompliant`: The `Subscription` matches what is required by the policy.
- `InstallPlanCompliant`: Installation and upgrade `InstallPlan` objects are either approved or waiting to be approved.
- `ClusterServiceVersionCompliant`: The installed operator version matches the `versions` list in `inform` mode, or meets policy requirements in `enforce` mode.
- `CustomResourceDefinitionCompliant`: Custom resource definitions (CRDs) required by the operator are present.
- `DeploymentCompliant`: Operator `Deployment` objects are running and available.
- `CatalogSourcesUnhealthy`: Reports `CatalogSource` health. When `status` is `"False"`, the catalog source was found and is healthy.
- `NoDeprecations`: The requested package, channel, and bundle are at recommended versions. When deprecations are detected, this condition reports them; whether that affects overall compliance is controlled by `complianceConfig.deprecationsPresent`.
- `Compliant`: Overall compliance summary aggregating the individual condition messages above.

##### Troubleshooting Tips

When the policy spec is invalid, the condition `ValidPolicySpec` will appear `False` and the `status` will be `NonCompliant`. Below are some common error messages and suggested fixes.

<details>
<summary>Could not build subscription: failed to parse the template JSON string</summary>

At least one Golang text template within the policy spec is invalid. In this example, the policy contains a function called `thisIsAnInvalidTemplateFunction` that does not exist. See [Operator policy templating](#operator-policy-templating) for more information about Golang text templates.

```yaml
apiVersion: policy.open-cluster-management.io/v1beta1
kind: OperatorPolicy
metadata:
  name: policy-eso
spec:
  # (other fields not shown)
  subscription:
    channel: alpha
    name: external-secrets-operator
    namespace: default
    source: operatorhubio-catalog
    sourceNamespace: olm
    startingCSV: '{{ thisIsAnInvalidTemplateFunction "default" "bad-eso-config" "startingCSV" }}'
status:
  compliant: NonCompliant
  conditions:
  # (other conditions not shown)
  - lastTransitionTime: "2026-06-01T22:57:32Z"
    message: 'could not build subscription: failed to parse the template JSON string {"channel":"alpha","installPlanApproval":"None","name":"external-secrets-operator","namespace":"default","source":"operatorhubio-catalog","sourceNamespace":"olm","startingCSV":"{{ thisIsAnInvalidTemplateFunction \"default\" \"bad-eso-config\" \"startingCSV\" }}"}: template: tmpl:7: function "thisIsAnInvalidTemplateFunction" not defined'
    reason: InvalidPolicySpec
    status: "False"
    type: ValidPolicySpec
```
</details>

<details>
<summary>Name is required in <code>spec.subscription</code></summary>

The policy spec is invalid when the `name` field is not present in the `spec.subscription` of an `OperatorPolicy`. Even if `spec.subscription.metadata.name` is present, `spec.subscription.name` must be set.

```yaml
apiVersion: policy.open-cluster-management.io/v1beta1
kind: OperatorPolicy
metadata:
  name: policy-eso
spec:
  # (other fields not shown)
  subscription:
    namespace: default
    # name is missing
    channel: alpha
    source: operatorhubio-catalog
    sourceNamespace: olm
    startingCSV: external-secrets-operator.v0.11.0
status:
  compliant: NonCompliant
  conditions:
  # (other conditions not shown)
  - lastTransitionTime: "2026-06-01T22:28:48Z"
    message: name is required in spec.subscription
    reason: InvalidPolicySpec
    status: "False"
    type: ValidPolicySpec
```
</details>

<details>
<summary>InstallPlanApproval is prohibited in <code>spec.subscription</code></summary>

The policy spec is invalid when the `installPlanApproval` field is present in the `spec.subscription` of an `OperatorPolicy`. Instead, set the `upgradeApproval` field to control the automatic approval of install plans.

```yaml
apiVersion: policy.open-cluster-management.io/v1beta1
kind: OperatorPolicy
metadata:
  name: policy-eso
spec:
  # (other fields not shown)
  upgradeApproval: Automatic # Keep this field.
  subscription:
    namespace: default
    name: external-secrets-operator
    channel: alpha
    source: operatorhubio-catalog
    sourceNamespace: olm
    startingCSV: external-secrets-operator.v0.11.0
    installPlanApproval: Automatic # Remove this field.
status:
  compliant: NonCompliant
  conditions:
  # (other conditions not shown)
  - lastTransitionTime: "2026-06-01T22:28:48Z"
    message: installPlanApproval is prohibited in spec.subscription
    reason: InvalidPolicySpec
    status: "False"
    type: ValidPolicySpec
```
</details>

### Architecture

The Deployment `config-policy-controller` contains two main controllers: Configuration Policy Controller and Operator Policy Controller. Both evaluate policy rules and support Golang text templates.

**Configuration Policy Controller** - Watches for `ConfigurationPolicy` resources and evaluates Kubernetes objects against desired state specifications.

**Operator Policy Controller** - Manages the lifecycle of Operator Lifecycle Manager (OLM) operators through `OperatorPolicy` resources. Handles `Subscriptions`, `OperatorGroups`, and monitors the health of operator deployments and `ClusterServiceVersions`. The controller can manage operator installation, upgrades, and removal with configurable behaviors for resource cleanup.

![Config Policy Controller Architecture](images/config-policy-controller-architecture-diagram.png)

## Getting started

Go to the
[Contributing guide](https://github.com/open-cluster-management-io/community/blob/main/sig-policy/contribution-guidelines.md)
to learn how to get involved.

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

  - (optional) Build container image
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

    The controller is deployed to a namespace defined in `KIND_NAMESPACE` and monitors the namepace defined in `WATCH_NAMESPACE` for `ConfigurationPolicy` resources.

    1. Deploy the controller and related resources
       ```bash
       make deploy
       ```

       The deployment namespaces are configurable with:
       ```bash
       export KIND_NAMESPACE=''  # (defaults to 'open-cluster-management-agent-addon')
       export WATCH_NAMESPACE=''       # (defaults to 'managed')
       ```
    **NOTE:** Please be aware of the community's [deployment images](https://github.com/open-cluster-management-io/community#deployment-images) special note.


### Steps for test

  - Code linting
    ```bash
    make fmt
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
    3. Start the controller locally
       ```bash
       make build
       ```
       ```bash
       export WATCH_NAMESPACE=<namespace>
       make run
       ```
    4. Run E2E tests:
       ```bash
       make e2e-test
       ```

## References

- The `config-policy-controller` is part of the `open-cluster-management` community. For more information, visit: [open-cluster-management.io](https://open-cluster-management.io).
- Check the [Security guide](SECURITY.md) if you need to report a security issue.

<!---
Date: 11/24/2021
-->
