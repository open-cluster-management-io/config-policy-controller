# Copyright 2019 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# Copyright Contributors to the Open Cluster Management project

PWD := $(shell pwd)
LOCAL_BIN ?= $(PWD)/bin

# Keep an existing GOPATH, make a private one if it is undefined
GOPATH_DEFAULT := $(PWD)/.go
export GOPATH ?= $(GOPATH_DEFAULT)
GOBIN_DEFAULT := $(GOPATH)/bin
export GOBIN ?= $(GOBIN_DEFAULT)
export PATH := $(LOCAL_BIN):$(GOBIN):$(PATH)
GOARCH = $(shell go env GOARCH)
GOOS = $(shell go env GOOS)
TESTARGS_DEFAULT := -v
export TESTARGS ?= $(TESTARGS_DEFAULT)
VERSION ?= $(shell cat COMPONENT_VERSION 2> /dev/null)
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/$(IMG)
CONTROLLER_NAMESPACE ?= open-cluster-management-agent-addon
# Handle KinD configuration
MANAGED_CLUSTER_SUFFIX ?= 
MANAGED_CLUSTER_NAME ?= managed$(MANAGED_CLUSTER_SUFFIX)
WATCH_NAMESPACE ?= $(MANAGED_CLUSTER_NAME)
KIND_NAME ?= test-$(MANAGED_CLUSTER_NAME)
KIND_CLUSTER_NAME ?= kind-$(KIND_NAME)
KIND_NAMESPACE ?= $(CONTROLLER_NAMESPACE)
KIND_VERSION ?= latest
# Set the Kind version tag
ifeq ($(KIND_VERSION), minimum)
	KIND_ARGS = --image kindest/node:v1.19.16
else ifneq ($(KIND_VERSION), latest)
	KIND_ARGS = --image kindest/node:$(KIND_VERSION)
else
	KIND_ARGS =
endif
# Test coverage threshold
export COVERAGE_MIN ?= 75
COVERAGE_E2E_OUT ?= coverage_e2e.out

# Image URL to use all building/pushing image targets;
# Use your own docker registry and image name for dev/test by overridding the IMG and REGISTRY environment variable.
IMG ?= $(shell cat COMPONENT_NAME 2> /dev/null)
REGISTRY ?= quay.io/open-cluster-management
TAG ?= latest

# Handle base64 OS differences
OS = $(shell uname -s | tr '[:upper:]' '[:lower:]')
BASE64 = base64 -w 0
ifeq ($(OS), darwin)
    BASE64 = base64
endif

# go-get-tool will 'go install' any package $1 and install it to LOCAL_BIN.
define go-get-tool
@set -e ;\
echo "Checking installation of $(1)" ;\
GOBIN=$(LOCAL_BIN) go install $(1)
endef

include build/common/Makefile.common.mk

############################################################
# work section
############################################################

$(GOBIN):
	@mkdir -p $(GOBIN)

$(LOCAL_BIN):
	@mkdir -p $(LOCAL_BIN)

############################################################
# format section
############################################################

.PHONY: fmt-dependencies
fmt-dependencies:
	$(call go-get-tool,github.com/daixiang0/gci@v0.2.9)
	$(call go-get-tool,mvdan.cc/gofumpt@v0.2.0)

.PHONY: fmt
fmt: fmt-dependencies
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gofmt -s -w
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gci -w -local "$(shell cat go.mod | head -1 | cut -d " " -f 2)"
	find . -not \( -path "./.go" -prune \) -name "*.go" | xargs gofumpt -l -w

############################################################
# check section
############################################################

.PHONY: lint-dependencies
lint-dependencies:
	$(call go-get-tool,github.com/golangci/golangci-lint/cmd/golangci-lint@v1.46.2)

.PHONY: check
check: lint

# All available linters: lint-dockerfiles lint-scripts lint-yaml lint-copyright-banner lint-go lint-python lint-helm lint-markdown lint-sass lint-typescript lint-protos
# Default value will run all linters, override these make target with your requirements:
#    eg: lint: lint-go lint-yaml
.PHONY: lint
lint: lint-dependencies lint-all

############################################################
# test section
############################################################
GOSEC = $(LOCAL_BIN)/gosec
KUBEBUILDER = $(LOCAL_BIN)/kubebuilder
KBVERSION = 3.2.0
K8S_VERSION = 1.21.2

.PHONY: test
test: test-dependencies
	KUBEBUILDER_ASSETS=$(LOCAL_BIN) go test $(TESTARGS) `go list ./... | grep -v test/e2e`

.PHONY: test-coverage
test-coverage: TESTARGS = -json -cover -covermode=atomic -coverprofile=coverage_unit.out
test-coverage: test

.PHONY: test-dependencies
test-dependencies: kubebuilder-dependencies kubebuilder

.PHONY: kubebuilder
kubebuilder:
	@if [ "$$($(KUBEBUILDER) version 2>/dev/null | grep -o KubeBuilderVersion:\"[0-9]*\.[0-9]\.[0-9]*\")" != "KubeBuilderVersion:\"$(KBVERSION)\"" ]; then \
		echo "Installing Kubebuilder"; \
		curl -L https://github.com/kubernetes-sigs/kubebuilder/releases/download/v$(KBVERSION)/kubebuilder_$(GOOS)_$(GOARCH) -o $(KUBEBUILDER); \
		chmod +x $(KUBEBUILDER); \
	fi

.PHONY: kubebuilder-dependencies
kubebuilder-dependencies: $(LOCAL_BIN)
	@if [ ! -f $(LOCAL_BIN)/etcd ] || [ ! -f $(LOCAL_BIN)/kube-apiserver ] || [ ! -f $(LOCAL_BIN)/kubectl ] || \
	[ "$$($(KUBEBUILDER) version 2>/dev/null | grep -o KubeBuilderVersion:\"[0-9]*\.[0-9]\.[0-9]*\")" != "KubeBuilderVersion:\"$(KBVERSION)\"" ]; then \
		echo "Installing envtest Kubebuilder assets"; \
		curl -L "https://go.kubebuilder.io/test-tools/$(K8S_VERSION)/$(GOOS)/$(GOARCH)" | tar xz --strip-components=2 -C $(LOCAL_BIN); \
	fi

.PHONY: gosec
gosec:
	$(call go-get-tool,github.com/securego/gosec/v2/cmd/gosec@v2.9.6)

.PHONY: gosec-scan
gosec-scan: gosec
	$(GOSEC) -fmt sonarqube -out gosec.json -no-fail -exclude-dir=.go ./...

############################################################
# build section
############################################################

.PHONY: build
build:
	@build/common/scripts/gobuild.sh build/_output/bin/$(IMG) ./

.PHONY: local
local:
	@GOOS=darwin build/common/scripts/gobuild.sh build/_output/bin/$(IMG) ./cmd/manager

############################################################
# images section
############################################################

.PHONY: build-images
build-images:
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .
	@docker tag ${IMAGE_NAME_AND_VERSION} $(REGISTRY)/$(IMG):$(TAG)


# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
.PHONY: deploy
deploy: generate-operator-yaml
	kubectl apply -f deploy/operator.yaml -n $(CONTROLLER_NAMESPACE)
	kubectl apply -f deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml -n $(CONTROLLER_NAMESPACE)
	kubectl set env deployment/$(IMG) -n $(CONTROLLER_NAMESPACE) WATCH_NAMESPACE=$(WATCH_NAMESPACE)

.PHONY: create-ns
create-ns:
	-@kubectl create namespace $(CONTROLLER_NAMESPACE)
	-@kubectl create namespace $(WATCH_NAMESPACE)

# Run against the current locally configured Kubernetes cluster
.PHONY: run
run:
	WATCH_NAMESPACE=$(WATCH_NAMESPACE) go run ./main.go --leader-elect=false

############################################################
# clean section
############################################################

.PHONY: clean
clean:
	-rm bin/*
	-rm build/_output/bin/*
	-rm coverage*.out
	-rm report*.json
	-rm kubeconfig_managed*
	-rm -r vendor/

############################################################
# Generate manifests
############################################################
CONTROLLER_GEN = $(LOCAL_BIN)/controller-gen
KUSTOMIZE = $(LOCAL_BIN)/kustomize
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

.PHONY: manifests
manifests: controller-gen kustomize
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=config-policy-controller paths="./..." output:crd:artifacts:config=deploy/crds output:rbac:artifacts:config=deploy/rbac
	mv deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml deploy/crds/kustomize/policy.open-cluster-management.io_configurationpolicies.yaml
	# Add a newline so that the format matches what kubebuilder generates
	@printf "\n---\n" > deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml
	$(KUSTOMIZE) build deploy/crds/kustomize >> deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-operator-yaml
generate-operator-yaml: manifests
	$(KUSTOMIZE) build deploy/manager > deploy/operator.yaml

.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,sigs.k8s.io/controller-tools/cmd/controller-gen@v0.6.1)

.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,sigs.k8s.io/kustomize/kustomize/v4@v4.5.4)

############################################################
# e2e test section
############################################################
GINKGO = $(LOCAL_BIN)/ginkgo

.PHONY: kind-bootstrap-cluster
kind-bootstrap-cluster: kind-bootstrap-cluster-dev kind-deploy-controller

.PHONY: kind-bootstrap-cluster-dev
kind-bootstrap-cluster-dev: kind-create-cluster install-crds kind-controller-kubeconfig

.PHONY: kind-deploy-controller
kind-deploy-controller: generate-operator-yaml install-resources deploy

.PHONY: deploy-controller
deploy-controller: kind-deploy-controller

.PHONY: kind-deploy-controller-dev
kind-deploy-controller-dev: kind-deploy-controller
	@echo Pushing image to KinD cluster
	kind load docker-image $(REGISTRY)/$(IMG):$(TAG) --name $(KIND_NAME)
	@echo "Patch deployment image"
	kubectl patch deployment $(IMG) -n $(KIND_NAMESPACE) -p "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"$(IMG)\",\"imagePullPolicy\":\"Never\"}]}}}}"
	kubectl patch deployment $(IMG) -n $(KIND_NAMESPACE) -p "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"$(IMG)\",\"image\":\"$(REGISTRY)/$(IMG):$(TAG)\"}]}}}}"
	kubectl rollout status -n $(KIND_NAMESPACE) deployment $(IMG) --timeout=180s

# Specify KIND_VERSION to indicate the version tag of the KinD image
.PHONY: kind-create-cluster
kind-create-cluster:
	# ensuring cluster $(KIND_NAME)
	-kind create cluster --name $(KIND_NAME) $(KIND_ARGS)
	kubectl config use-context $(KIND_CLUSTER_NAME)
	kind get kubeconfig --name $(KIND_NAME) > kubeconfig_$(MANAGED_CLUSTER_NAME)_e2e

.PHONY: kind-controller-kubeconfig
kind-controller-kubeconfig: install-resources
	kubectl -n $(KIND_NAMESPACE) apply -f test/resources/e2e_controller_secret.yaml
	-rm kubeconfig_$(MANAGED_CLUSTER_NAME)
	@kubectl config set-cluster $(KIND_CLUSTER_NAME) --kubeconfig=$(PWD)/kubeconfig_$(MANAGED_CLUSTER_NAME) \
		--server=$(shell kubectl config view --minify -o jsonpath='{.clusters[].cluster.server}' --kubeconfig=kubeconfig_$(MANAGED_CLUSTER_NAME)_e2e) \
		--insecure-skip-tls-verify=true
	@kubectl config set-credentials $(KIND_CLUSTER_NAME) --kubeconfig=$(PWD)/kubeconfig_$(MANAGED_CLUSTER_NAME) \
		--token=$$(kubectl get secret -n $(KIND_NAMESPACE) config-policy-controller -o jsonpath='{.data.token}' --kubeconfig=$(PWD)/kubeconfig_$(MANAGED_CLUSTER_NAME)_e2e | $(BASE64) --decode)
	@kubectl config set-context $(KIND_CLUSTER_NAME) --kubeconfig=$(PWD)/kubeconfig_$(MANAGED_CLUSTER_NAME) \
		--user=$(KIND_CLUSTER_NAME) --cluster=$(KIND_CLUSTER_NAME)
	@kubectl config use-context $(KIND_CLUSTER_NAME) --kubeconfig=$(PWD)/kubeconfig_$(MANAGED_CLUSTER_NAME)

.PHONY: kind-additional-cluster
kind-additional-cluster: MANAGED_CLUSTER_SUFFIX = 2
kind-additional-cluster: kind-create-cluster kind-controller-kubeconfig

.PHONY: kind-delete-cluster
kind-delete-cluster:
	-kind delete cluster --name $(KIND_NAME)
	-kind delete cluster --name $(KIND_NAME)2

.PHONY: kind-tests
kind-tests: kind-delete-cluster kind-bootstrap-cluster-dev build-images kind-deploy-controller-dev e2e-test

.PHONY: install-crds
install-crds:
	@echo installing crds
	kubectl apply -f test/crds/clusterversions.config.openshift.io.yaml
	kubectl apply -f test/crds/securitycontextconstraints.security.openshift.io_crd.yaml
	kubectl apply -f test/crds/apiservers.config.openshift.io_crd.yaml
	kubectl apply -f test/crds/clusterclaims.cluster.open-cluster-management.io.yaml
	kubectl apply -f test/crds/oauths.config.openshift.io_crd.yaml
	kubectl apply -f https://raw.githubusercontent.com/open-cluster-management-io/governance-policy-propagator/main/deploy/crds/policy.open-cluster-management.io_policies.yaml
	kubectl apply -f deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml

.PHONY: install-resources
install-resources:
	# creating namespaces
	-kubectl create ns $(WATCH_NAMESPACE)
	-kubectl create ns $(KIND_NAMESPACE)
	# deploying roles and service account
	kubectl apply -k deploy/rbac
	kubectl apply -f deploy/manager/service-account.yaml -n $(KIND_NAMESPACE)

.PHONY: kind-ensure-sa
kind-ensure-sa:
	@KUBECONFIG_TOKEN="$$(kubectl config view --raw -o jsonpath='{.users[].user.token}')"; \
	KUBECONFIG_USER="$$(echo "$${KUBECONFIG_TOKEN}" | jq -rR 'split(".") | .[1] | select(. != null) | @base64d | fromjson | .sub')"; \
	echo "Kubeconfig user detected from token: $${KUBECONFIG_USER}"; \
	[ "$${KUBECONFIG_USER}" = "system:serviceaccount:$(KIND_NAMESPACE):config-policy-controller" ]

.PHONY: e2e-dependencies
e2e-dependencies:
	$(call go-get-tool,github.com/onsi/ginkgo/v2/ginkgo@$(shell awk '/github.com\/onsi\/ginkgo\/v2/ {print $$2}' go.mod))

.PHONY: e2e-test
e2e-test: e2e-dependencies
	$(GINKGO) -v --fail-fast --slow-spec-threshold=10s $(E2E_TEST_ARGS) test/e2e

.PHONY: e2e-test-coverage
e2e-test-coverage: E2E_TEST_ARGS = --json-report=report_e2e.json --label-filter='!hosted-mode' --output-dir=.
e2e-test-coverage: e2e-run-instrumented e2e-test e2e-stop-instrumented

.PHONY: e2e-test-hosted-mode-coverage
e2e-test-hosted-mode-coverage: E2E_TEST_ARGS = --json-report=report_e2e_hosted_mode.json --label-filter="hosted-mode" --output-dir=.
e2e-test-hosted-mode-coverage: COVERAGE_E2E_OUT = coverage_e2e_hosted_mode.out
e2e-test-hosted-mode-coverage: export TARGET_KUBECONFIG_PATH = $(PWD)/kubeconfig_managed2
e2e-test-hosted-mode-coverage: e2e-run-instrumented e2e-test e2e-stop-instrumented

.PHONY: e2e-build-instrumented
e2e-build-instrumented:
	go test -covermode=atomic -coverpkg=$(shell cat go.mod | head -1 | cut -d ' ' -f 2)/... -c -tags e2e ./ -o build/_output/bin/$(IMG)-instrumented

.PHONY: e2e-run-instrumented
e2e-run-instrumented: e2e-build-instrumented
	WATCH_NAMESPACE="$(WATCH_NAMESPACE)" ./build/_output/bin/$(IMG)-instrumented -test.run "^TestRunMain$$" -test.coverprofile=$(COVERAGE_E2E_OUT) &>build/_output/controller.log &

.PHONY: e2e-stop-instrumented
e2e-stop-instrumented:
	ps -ef | grep '$(IMG)' | grep -v grep | awk '{print $$2}' | xargs kill

.PHONY: e2e-debug
e2e-debug:
	@echo local controller log:
	-cat build/_output/controller.log
	@echo remote controller log:
	-kubectl logs $$(kubectl get pods -n $(KIND_NAMESPACE) -o name | grep $(IMG)) -n $(KIND_NAMESPACE)

############################################################
# test coverage
############################################################
GOCOVMERGE = $(LOCAL_BIN)/gocovmerge
.PHONY: coverage-dependencies
coverage-dependencies:
	$(call go-get-tool,github.com/wadey/gocovmerge@v0.0.0-20160331181800-b5bfa59ec0ad)

COVERAGE_FILE = coverage.out
.PHONY: coverage-merge
coverage-merge: coverage-dependencies
	@echo Merging the coverage reports into $(COVERAGE_FILE)
	$(GOCOVMERGE) $(PWD)/coverage_* > $(COVERAGE_FILE)

.PHONY: coverage-verify
coverage-verify:
	./build/common/scripts/coverage_calc.sh
