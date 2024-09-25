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
export PATH := $(LOCAL_BIN):$(PATH)
GOARCH = $(shell go env GOARCH)
GOOS = $(shell go env GOOS)
TESTARGS_DEFAULT := -v
TESTARGS ?= $(TESTARGS_DEFAULT)
CONTROLLER_NAME = $(shell cat COMPONENT_NAME 2> /dev/null)
# Handle KinD configuration
MANAGED_CLUSTER_SUFFIX ?= 
MANAGED_CLUSTER_NAME ?= managed$(MANAGED_CLUSTER_SUFFIX)
WATCH_NAMESPACE ?= $(MANAGED_CLUSTER_NAME)
KIND_NAME ?= test-$(MANAGED_CLUSTER_NAME)
KIND_CLUSTER_NAME ?= kind-$(KIND_NAME)
KIND_NAMESPACE ?= open-cluster-management-agent-addon
# Test coverage threshold
export COVERAGE_MIN ?= 75
COVERAGE_E2E_OUT ?= coverage_e2e.out

# Image URL to use all building/pushing image targets;
# Use your own docker registry and image name for dev/test by overridding the IMG and REGISTRY environment variable.
IMG ?= $(CONTROLLER_NAME)
REGISTRY ?= quay.io/open-cluster-management
TAG ?= latest
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/$(IMG)

# Fix sed issues on mac by using GSED
SED="sed"
ifeq ($(GOOS), darwin)
  SED="gsed"
endif

include build/common/Makefile.common.mk

############################################################
# Lint
############################################################

.PHONY: lint
lint:

.PHONY: fmt
fmt:

############################################################
# test section
############################################################

.PHONY: test
test: envtest kubebuilder
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test $(TESTARGS) `go list ./... | grep -v test/e2e`

.PHONY: test-coverage
test-coverage: TESTARGS = -json -cover -covermode=atomic -coverprofile=coverage_unit.out
test-coverage: test

.PHONY: gosec-scan
gosec-scan: GOSEC_ARGS = -exclude-generated

############################################################
# build section
############################################################

.PHONY: build
build:
	CGO_ENABLED=1 go build -o build/_output/bin/$(IMG) ./

.PHONY: build-cmd
build-cmd: manifests
	go build -o build/_output/bin/dryrun ./cmd/

############################################################
# images section
############################################################

.PHONY: build-images
build-images:
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .
	@docker tag ${IMAGE_NAME_AND_VERSION} $(REGISTRY)/$(IMG):$(TAG)

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
.PHONY: deploy
deploy: generate-operator-yaml create-ns
	$(SED) -i 's/\(namespace: \)open-cluster-management-agent-addon/\1$(CONTROLLER_NAMESPACE)/' -i deploy/operator.yaml 
	kubectl apply -f deploy/operator.yaml -n $(CONTROLLER_NAMESPACE)
	$(SED) -i 's/\(namespace: \)$(CONTROLLER_NAMESPACE)/\1open-cluster-management-agent-addon/' -i deploy/operator.yaml 
	kubectl apply -f deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml -n $(CONTROLLER_NAMESPACE)
	kubectl set env deployment/$(IMG) -n $(CONTROLLER_NAMESPACE) WATCH_NAMESPACE=$(WATCH_NAMESPACE)

.PHONY: create-ns
create-ns:
	-@kubectl create namespace $(KIND_NAMESPACE)
	-@kubectl create namespace $(WATCH_NAMESPACE)

# Run against the current locally configured Kubernetes cluster
.PHONY: run
run:
	WATCH_NAMESPACE=$(WATCH_NAMESPACE) go run ./main.go controller --leader-elect=false --enable-operator-policy=true

############################################################
# clean section
############################################################

.PHONY: clean
clean:
	-rm bin/*
	-rm build/_output/bin/*
	-rm coverage*.out
	-rm report*.json
	-rm kubeconfig_*
	-rm -r vendor/

############################################################
# Generate manifests
############################################################

.PHONY: manifests
manifests: controller-gen kustomize
	$(CONTROLLER_GEN) crd rbac:roleName=config-policy-controller paths="./..." output:crd:artifacts:config=deploy/crds output:rbac:artifacts:config=deploy/rbac
	mv deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml deploy/crds/kustomize_configurationpolicy/policy.open-cluster-management.io_configurationpolicies.yaml
	mv deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml deploy/crds/kustomize_operatorpolicy/policy.open-cluster-management.io_operatorpolicies.yaml
	@printf -- "---\n" > deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml
	@printf -- "---\n" > deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml
	$(KUSTOMIZE) build deploy/crds/kustomize_configurationpolicy >> deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml
	$(KUSTOMIZE) build deploy/crds/kustomize_operatorpolicy >> deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml
	cp deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml ./cmd/dryrun/

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-operator-yaml
generate-operator-yaml: manifests
	$(KUSTOMIZE) build deploy/manager > deploy/operator.yaml

############################################################
# e2e test section
############################################################
GINKGO = $(LOCAL_BIN)/ginkgo

.PHONY: kind-bootstrap-cluster
kind-bootstrap-cluster: kind-bootstrap-cluster-dev kind-deploy-controller

.PHONY: kind-bootstrap-cluster-dev
kind-bootstrap-cluster-dev: CLUSTER_NAME = $(MANAGED_CLUSTER_NAME)
kind-bootstrap-cluster-dev: kind-create-cluster install-crds kind-controller-kubeconfig

.PHONY: kind-deploy-controller
kind-deploy-controller: generate-operator-yaml install-resources deploy

.PHONY: deploy-controller
deploy-controller: kind-deploy-controller

HOSTED ?= none

.PHONY: kind-deploy-controller-dev
kind-deploy-controller-dev: 
	if [ "$(HOSTED)" = "hosted" ]; then\
		$(MAKE) kind-deploy-controller-dev-addon ;\
	else\
		$(MAKE) kind-deploy-controller-dev-normal ;\
	fi

.PHONY: kind-deploy-controller-dev-normal
kind-deploy-controller-dev-normal: kind-deploy-controller
	@echo Pushing image to KinD cluster
	kind load docker-image $(REGISTRY)/$(IMG):$(TAG) --name $(KIND_NAME)
	@echo "Patch deployment image"
	kubectl patch deployment $(IMG) -n $(KIND_NAMESPACE) -p "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"$(IMG)\",\"imagePullPolicy\":\"Never\"}]}}}}"
	kubectl patch deployment $(IMG) -n $(KIND_NAMESPACE) -p "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"$(IMG)\",\"image\":\"$(REGISTRY)/$(IMG):$(TAG)\"}]}}}}"
	kubectl rollout status -n $(KIND_NAMESPACE) deployment $(IMG) --timeout=180s

.PHONY: kind-deploy-controller-dev-addon
kind-deploy-controller-dev-addon:
	kind load docker-image $(REGISTRY)/$(IMG):$(TAG) --name $(KIND_NAME)
	kubectl annotate -n $(subst -hosted,,$(KIND_NAMESPACE)) --overwrite managedclusteraddon config-policy-controller\
		addon.open-cluster-management.io/values='{"global":{"imagePullPolicy": "Never", "imageOverrides":{"config_policy_controller": "$(REGISTRY)/$(IMG):$(TAG)"}}}'

# Specify KIND_VERSION to indicate the version tag of the KinD image
.PHONY: kind-create-cluster
kind-create-cluster:

.PHONY: kind-additional-cluster
kind-additional-cluster: MANAGED_CLUSTER_SUFFIX = 2
kind-additional-cluster: CLUSTER_NAME = $(MANAGED_CLUSTER_NAME)
kind-additional-cluster: kind-create-cluster kind-controller-kubeconfig install-crds-hosted

.PHONY: kind-delete-cluster
kind-delete-cluster:
	-kind delete cluster --name $(KIND_NAME)
	-kind delete cluster --name $(KIND_NAME)2

.PHONY: kind-tests
kind-tests: kind-delete-cluster kind-bootstrap-cluster-dev kind-deploy-controller-dev e2e-test

OLM_VERSION := v0.26.0
OLM_INSTALLER = $(LOCAL_BIN)/install.sh

.PHONY: install-crds
install-crds:  $(OLM_INSTALLER)
	chmod +x $(LOCAL_BIN)/install.sh
	$(LOCAL_BIN)/install.sh $(OLM_VERSION)
	@echo installing crds
	kubectl apply -f test/crds/clusterversions.config.openshift.io.yaml
	kubectl apply -f test/crds/securitycontextconstraints.security.openshift.io_crd.yaml
	kubectl apply -f test/crds/apiservers.config.openshift.io_crd.yaml
	kubectl apply -f test/crds/clusterclaims.cluster.open-cluster-management.io.yaml
	kubectl apply -f test/crds/oauths.config.openshift.io_crd.yaml
	kubectl apply -f https://raw.githubusercontent.com/open-cluster-management-io/governance-policy-propagator/main/deploy/crds/policy.open-cluster-management.io_policies.yaml
	kubectl apply -f deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml
	kubectl apply -f deploy/crds/policy.open-cluster-management.io_operatorpolicies.yaml

$(OLM_INSTALLER):
	@echo getting OLM installer
	mkdir -p $(LOCAL_BIN)
	curl -L https://github.com/operator-framework/operator-lifecycle-manager/releases/download/$(OLM_VERSION)/install.sh -o $(LOCAL_BIN)/install.sh

install-crds-hosted: export KUBECONFIG=./kubeconfig_managed$(MANAGED_CLUSTER_SUFFIX)_e2e
install-crds-hosted: $(OLM_INSTALLER)
	chmod +x $(LOCAL_BIN)/install.sh
	$(LOCAL_BIN)/install.sh $(OLM_VERSION)

.PHONY: install-resources
install-resources:
	# creating namespaces
	-kubectl create ns $(WATCH_NAMESPACE)
	-kubectl create ns $(KIND_NAMESPACE)
	# deploying roles and service account
	kubectl apply -k deploy/rbac
	kubectl apply -f deploy/manager/service-account.yaml -n $(KIND_NAMESPACE)

IS_HOSTED ?= false
E2E_PROCS = 20

.PHONY: e2e-test
e2e-test: e2e-dependencies
	$(GINKGO) -v --procs=$(E2E_PROCS) $(E2E_TEST_ARGS) test/e2e -- -is_hosted=$(IS_HOSTED)

.PHONY: e2e-test-coverage
e2e-test-coverage: E2E_TEST_ARGS = --json-report=report_e2e.json --label-filter='!hosted-mode && !running-in-cluster' --output-dir=.
e2e-test-coverage: e2e-run-instrumented e2e-test e2e-stop-instrumented

.PHONY: e2e-test-hosted-mode-coverage
e2e-test-hosted-mode-coverage: E2E_TEST_ARGS = --json-report=report_e2e_hosted_mode.json --label-filter="hosted-mode || supports-hosted && !running-in-cluster" --output-dir=.
e2e-test-hosted-mode-coverage: COVERAGE_E2E_OUT = coverage_e2e_hosted_mode.out
e2e-test-hosted-mode-coverage: IS_HOSTED=true
e2e-test-hosted-mode-coverage: E2E_PROCS=2
e2e-test-hosted-mode-coverage: export TARGET_KUBECONFIG_PATH = $(PWD)/kubeconfig_managed2
e2e-test-hosted-mode-coverage: e2e-run-instrumented e2e-test e2e-stop-instrumented

.PHONY: e2e-test-running-in-cluster
e2e-test-running-in-cluster: E2E_TEST_ARGS = --label-filter="running-in-cluster" --covermode=atomic --coverprofile=coverage_e2e_uninstall.out --coverpkg=open-cluster-management.io/config-policy-controller/pkg/triggeruninstall
e2e-test-running-in-cluster: e2e-test

.PHONY: e2e-build-instrumented
e2e-build-instrumented:
	go test -covermode=atomic -coverpkg=$(shell cat go.mod | head -1 | cut -d ' ' -f 2)/... -c -tags e2e ./ -o build/_output/bin/$(IMG)-instrumented

.PHONY: e2e-run-instrumented
e2e-run-instrumented: e2e-build-instrumented
	WATCH_NAMESPACE="$(WATCH_NAMESPACE)" ./build/_output/bin/$(IMG)-instrumented -test.run "^TestRunMain$$" -test.coverprofile=$(COVERAGE_E2E_OUT) 2>&1 | tee ./build/_output/controller.log &

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
COVERAGE_FILE = coverage.out

.PHONY: coverage-merge
coverage-merge: coverage-dependencies
	@echo Merging the coverage reports into $(COVERAGE_FILE)
	$(GOCOVMERGE) $(PWD)/coverage_* > $(COVERAGE_FILE)

.PHONY: coverage-verify
coverage-verify:
	./build/common/scripts/coverage_calc.sh