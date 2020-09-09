# Configuration Policy Controller
Red Hat Advanced Cluster Management Governance - Configuration Policy Controller

## How it works

This operator watches for the following changes to trigger reconcile:

1. configurationpolicy changes in all watched namespaces on hub

Every reconcile does following things:

1. Create/update/delete replicated policy on managed cluster in cluster namespace
2. Handles the object template specified in the configurationpolicy and creates an object / status update depending on the details of the object template

## Run
```
export WATCH_NAMESPACE=cluster_namespace_on_hub
operator-sdk run --local
```
<!---
Date: 9/09/2020
-->