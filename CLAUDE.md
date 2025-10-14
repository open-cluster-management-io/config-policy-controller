# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this
repository.

## Overview

The Configuration Policy Controller is a Kubernetes controller that enforces and evaluates
ConfigurationPolicy resources in Open Cluster Management. It monitors objects on managed clusters,
checks compliance against policy templates, and can automatically remediate non-compliant resources
when set to enforce mode.

## Build, Test, and Run Commands

### Building

```bash
# Build the controller binary
make build

# Build the dryrun CLI tool
make build-cmd

# Build container image (configurable with REGISTRY, IMG, TAG env vars)
make build-images
```

### Testing

```bash
# Run unit tests
make test

# Run unit tests with coverage
make test-coverage

# Run E2E tests (requires KinD cluster)
make e2e-test

# Run specific E2E tests
TESTARGS="--focus=<pattern>" make e2e-test

# Setup KinD cluster for development
make kind-bootstrap-cluster-dev

# Deploy controller to KinD and run E2E tests
make kind-tests
```

### Running Locally

```bash
# Run controller locally (must set WATCH_NAMESPACE)
export WATCH_NAMESPACE=<namespace>
make run

# The controller requires a Kubernetes cluster configured via kubectl
```

### Linting and Formatting

```bash
# Format code
make fmt
```

### Generate Manifests

```bash
# Generate CRDs and RBAC manifests
make manifests

# Generate DeepCopy implementations
make generate
```

## Architecture

### Core Components

**ConfigurationPolicyReconciler** (`controllers/configurationpolicy_controller.go`)

- Main reconciler that evaluates ConfigurationPolicy resources
- Handles both `inform` (report-only) and `enforce` (remediate) modes
- Uses dynamic client to work with any Kubernetes resource type
- Supports templating with Go templates and sprig functions
- Implements watch-based evaluation for efficient resource monitoring

**OperatorPolicyReconciler** (`controllers/operatorpolicy_controller.go`)

- Manages OperatorPolicy resources for OLM operator lifecycle management
- Controls operator subscriptions, CSV status, and upgrade behavior

**Evaluation Flow**:

1. Policy is reconciled based on evaluation interval or watch events
2. Templates are resolved (if present) using go-template-utils library
3. Namespace selector determines target namespaces
4. For each object template:
   - Determine desired objects (resolving selectors if needed)
   - Compare with existing cluster state
   - If enforce mode: create, update, or delete objects as needed
   - If inform mode: report compliance status only
5. Status is updated with compliance details and related objects
6. Events are emitted to parent Policy resource

### Key Packages

**pkg/common** - Shared utilities:

- `namespace_selection.go`: NamespaceSelector reconciler for efficient namespace filtering
- `common.go`: Helper functions for environment detection

**pkg/dryrun** - CLI tool for testing policies without a cluster:

- Simulates policy evaluation using fake clients
- Supports reading cluster resources or using local YAML files
- Provides diff output and compliance message reporting

**pkg/mappings** - API resource mappings for the dryrun CLI

**pkg/triggeruninstall** - Handles controller uninstallation cleanup

### Template Processing

The controller supports Go templating in object definitions with these special features:

- Hub templates: can reference objects from the hub cluster (when configured)
- Context variables: `.Object`, `.ObjectName`, `.ObjectNamespace` for dynamic templating per
  namespace/object
- Template functions: `fromSecret`, `fromConfigMap`, `fromClusterClaim`, `lookup`, plus sprig
  functions
- `skipObject` function: allows conditional object creation based on template logic
- Encryption support: templates can include encrypted values using AES encryption

### Compliance Types

- **musthave**: Object must exist and match the specified fields (partial match)
- **mustnothave**: Object must not exist
- **mustonlyhave**: Object must exist and match exactly (no extra fields)

### Watch vs Polling

The controller supports two evaluation modes:

- **Watch mode** (default): Uses Kubernetes watches for efficient real-time evaluation
- **Polling mode**: Periodically evaluates policies based on evaluationInterval

The dynamic watcher (kubernetes-dependency-watches) automatically manages watches on related
objects.

### Hosted Mode

The controller can run in "hosted mode" where:

- Controller runs on hub cluster
- Evaluates/enforces policies on a separate managed cluster
- Configured via `--target-kubeconfig-path` flag
- Uses separate managers for hub and managed cluster clients

## Important Patterns

### Object Comparison

The comparison logic in `handleSingleKey` and `mergeSpecsHelper` is critical:

- Merges template values into existing object to avoid false negatives
- Handles arrays specially (preserves duplicates, matches by "name" field)
- Dry-run updates verify actual API behavior before enforcement
- `zeroValueEqualsNil` parameter controls empty value handling

### Caching and Evaluation Optimization

- `processedPolicyCache`: Tracks evaluated objects by resourceVersion to avoid redundant comparisons
- `lastEvaluatedCache`: Prevents race conditions with controller-runtime cache staleness
- Evaluation backoff (`--evaluation-backoff`): Throttles frequent policy evaluations

### Pruning Behavior

When `pruneObjectBehavior` is set:

- **DeleteIfCreated**: Removes objects created by the policy
- **DeleteAll**: Removes all objects matching the template
- Tracked via finalizers and object UIDs in status.relatedObjects

### Dry Run CLI

The `dryrun` command provides policy testing without cluster modification:

```bash
# Test policy against local resources
build/_output/bin/dryrun -p policy.yaml resource1.yaml resource2.yaml

# Test policy against live cluster (read-only)
build/_output/bin/dryrun -p policy.yaml --from-cluster

# Compare status against expected
build/_output/bin/dryrun -p policy.yaml resources.yaml --desired-status expected-status.yaml
```

## Testing Guidelines

### E2E Test Structure

- Tests are in `test/e2e/` with descriptive case names
- Use Ginkgo/Gomega framework
- Tests can filter by label: `--label-filter='!hosted-mode'`
- Helper functions in `test/utils/utils.go` for common operations

### Writing Tests

- Use `utils.GetWithTimeout` for eventually-consistent checks
- Clean up resources in AfterEach blocks
- Use unique names to avoid test conflicts
- Test both inform and enforce modes where applicable

## Configuration

### Controller Flags

Key flags when running the controller:

- `--evaluation-concurrency`: Max concurrent policy evaluations (default: 2)
- `--evaluation-backoff`: Seconds before re-evaluation in watch mode (default: 10)
- `--enable-operator-policy`: Enable OperatorPolicy support
- `--target-kubeconfig-path`: Path to managed cluster kubeconfig (hosted mode)
- `--standalone-hub-templates-kubeconfig-path`: Hub cluster for template resolution

### Environment Variables

- `WATCH_NAMESPACE`: Namespace to monitor for policies (required when running locally)
- `POD_NAME`: Used to detect controller pod name
