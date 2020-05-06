CURL := $(shell which curl 2> /dev/null)
YQ_VERSION ?= 3.2.1
YQ_URL ?= https://github.com/mikefarah/yq/releases/download/$(YQ_VERSION)/yq_$(BUILD_HARNESS_OS)_$(BUILD_HARNESS_ARCH)
YQ ?= $(BUILD_HARNESS_PATH)/vendor/yq

.PHONY: yq/install
## Install yq
yq/install: %install:
	@[ -x $(YQ) ] || ( \
		echo "Installing YQ $(YQ_VERSION) ($(YQ_PLATFORM)_$(YQ_ARCH)) from $(YQ_URL)" && \
		curl '-#' -fL -o $(YQ) $(YQ_URL) && \
		chmod +x $(YQ) \
		)
	$(YQ) --version
