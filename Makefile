###############################################################################
# Licensed Materials - Property of IBM Copyright IBM Corporation 2017, 2019. All Rights Reserved.
# U.S. Government Users Restricted Rights - Use, duplication or disclosure restricted by GSA ADP
# Schedule Contract with IBM Corp.
#
# Contributors:
#  IBM Corporation - initial API and implementation
###############################################################################

include Configfile

ifneq ($(ARCH), x86_64)
DOCKER_FILE = Dockerfile.$(ARCH)
else
DOCKER_FILE = Dockerfile
endif
@echo "using DOCKER_FILE: $(DOCKER_FILE)"

.PHONY: init\:
init::
-include $(shell curl -fso .build-harness -H "Authorization: token ${GITHUB_TOKEN}" -H "Accept: application/vnd.github.v3.raw" "https://raw.github.ibm.com/ICP-DevOps/build-harness/master/templates/Makefile.build-harness"; echo .build-harness)

.PHONY: deps
deps::
	go get -d github.com/coreos/etcd
	go install github.com/coreos/etcd
	go get golang.org/x/lint/golint
	go get -u github.com/apg/patter
	go get -u github.com/wadey/gocovmerge
	curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
ifeq ($(ARCH), x86_64)
	curl -O https://storage.googleapis.com/kubernetes-release/release/v1.10.3/bin/linux/amd64/kube-apiserver &&\
	chmod a+x kube-apiserver &&\
	mv kube-apiserver ${GOPATH}/bin
else
	wget https://dl.k8s.io/v1.10.3/kubernetes-server-linux-$(ARCH).tar.gz
	tar -xf kubernetes-server-linux-$(ARCH).tar.gz 
	chmod a+x kubernetes/server/bin/kube-apiserver
	mv kubernetes/server/bin/kube-apiserver ${GOPATH}/bin
	rm -f kubernetes-server-linux-$(ARCH).tar.gz
	rm -rf kubernetes
endif

.PHONY: gocompile
gocompile:
	build-tools/build-all.sh

PHONY: propagator
propagator:
	build-tools/build-propagator.sh

.PHONY: clean
clean::
	rm -rf _build/

clean-test:
	-@rm -rf test-output
	-@rm -f cover.*

lint: 
	golint -set_exit_status=true pkg/

fmt:
	gofmt -l ${GOFILES}

.PHONY: lintall
lintall: fmt lint vet

.PHONY: test
test: 
	@./build-tools/test.sh

coverage:
	go tool cover -html=cover.out -o=cover.html
	@./build-tools/calculate-coverage.sh

#Check default DOCKER_REGISTRY/DOCKER_BUILD_TAG/SCRATCH_TAG/DOCKER_TAG values in Configfile
#Only new value other than default need to be set here
.PHONY: docker-logins
docker-logins:
	make docker:login DOCKER_REGISTRY=$(DOCKER_EDGE_REGISTRY)
	make docker:login DOCKER_REGISTRY=$(DOCKER_SCRATCH_REGISTRY)
	make docker:login

.PHONY: image
image:: docker-logins
	make docker:info
	make docker:build
	docker image ls -a

.PHONY: run
run: 
	make docker:info
	make docker:run

.PHONY: push
push:
	make docker:login DOCKER_REGISTRY=$(DOCKER_SCRATCH_REGISTRY)
	make docker:tag-arch DOCKER_REGISTRY=$(DOCKER_SCRATCH_REGISTRY) DOCKER_TAG=$(SCRATCH_TAG)
	make docker:push-arch DOCKER_REGISTRY=$(DOCKER_SCRATCH_REGISTRY) DOCKER_TAG=$(SCRATCH_TAG)

.PHONY: release
release:
	make docker:login
	make docker:tag-arch
	make docker:push-arch
	make docker:tag-arch DOCKER_TAG=$(SEMVERSION)
	make docker:push-arch DOCKER_TAG=$(SEMVERSION)
ifeq ($(ARCH), x86_64)
	make docker:tag-arch DOCKER_TAG=$(RELEASE_TAG_RED_HAT)
	make docker:push-arch DOCKER_TAG=$(RELEASE_TAG_RED_HAT)
	make docker:tag-arch DOCKER_TAG=$(SEMVERSION_RED_HAT)
	make docker:push-arch DOCKER_TAG=$(SEMVERSION_RED_HAT)
endif

.PHONY: multi-arch
multi-arch:
	make docker:manifest-tool
	make docker:multi-arch
	make docker:multi-arch DOCKER_TAG=$(SEMVERSION)
