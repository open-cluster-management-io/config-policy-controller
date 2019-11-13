<!-- <p align="center"><a href="http://35.227.205.240/?job=build_go-repo-template_postsubmit">
 prow build badge, godoc, and go report card
<img alt="Build Status" src="http://35.227.205.240/badge.svg?jobs=build_go-repo-template_postsubmit">
</a> <a href="https://godoc.org/github.com/IBM/go-repo-template"><img src="https://godoc.org/github.com/IBM/go-repo-template?status.svg"></a> <a href="https://goreportcard.com/report/github.com/IBM/go-repo-template"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/IBM/go-repo-template" /></a> <a href="https://codecov.io/github/IBM/go-repo-template?branch=master"><img alt="Code Coverage" src="https://codecov.io/gh/IBM/go-repo-template/branch/master/graphs/badge.svg?branch=master" /></a></p> -->

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
<!-- **Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)* -->
**Table of Contents** 
- [Policy Controller](#policy-controller)
    - [What is the Policy Controller](#what-is-the-policy-controller)
    - [Community, discussion, contribution, and support](#community-discussion-contribution-and-support)
    - [Getting Started](#getting-started)
        - [Prerequisites](#prerequisites)
        - [Trouble shooting](#trouble-shooting)
    - [Developing your policy controller](#developing-your-policy-controller)    
    - [References](#references)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Policy Controller

## What is Policy Controller

The policy controllers are Kubernetes CustomResourceDefinition (CRD) instance that can integrate with Governance Risk and Compliance (GRC) framework on IBM Multicloud management. Policy controller can monitor and report whether the Kubernetes cluster is compliant with the policy. It can also enforce the policy to bring the cluster state to compliance. This repo includes the policy controller framework with a sample policy controller.

## Securing the Policy Controller

The policy controller needs to interact with the Kubernetes API server to (1) get updates on the policy CR creation/deletion/update and (2) analyze the existing Kubernetes cluster config (in this sample controller we analyze RBAC role/clusterrole bindings).

The policy controller is authenticated/authorized by the Kubernetes API based on the information defined in the service-account it uses. The `default` service account in the namespace is used by the controller when it is deployed as a pod (unless the `spec.serviceAccountName` specifies otherwise). For finer-grain control, we create a dedicated [service-account](./deploy/service_account.yaml) for the controller and start the pod with the dedicated service-account.

It is important the limit the privileges on the controller using the principle of least privilege, in this context it means to limit (1) the access of the controller to only the resources (e.g. its CR instances) it needs to know about and (2)limit the actions to only the ones needed by the controller (e.g. read-only for certain resources).

The controller priveledges are bounded using (1) an [RBAC role](./deploy/role.yaml) that only grants the service account of the controller the minimum needed permissions to perform its functionality, and (2) an [RBAC rolebinding](deploy/role_binding.yaml) that binds the RBAC role to the controller's service account.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

------

## Getting Started

### Prerequisites

Check the [Development doc](docs/development.md) for how to contribute to the repo.

### Trouble shooting

Please refer to [Trouble shooting documentation](docs/trouble_shooting.md) for further information.

## Developing your policy controller
Please refer to [Adoption guide](docs/adoption_guide.md) for further information.

## References

If you have any further question about the policy controller, please refer to
[help documentation](docs/help.md) for further information.
