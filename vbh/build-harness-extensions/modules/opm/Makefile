CURL := $(shell which curl 2> /dev/null)
OPM_VERSION ?= v1.6.1
OPM_URL ?= https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$(BUILD_HARNESS_OS)-$(BUILD_HARNESS_ARCH)-opm
OPM ?= $(BUILD_HARNESS_PATH)/vendor/opm

.PHONY: opm/install
## Install opm
opm/install: %install:
	@[ -x $(OPM) ] || ( \
		echo "Version: $(OPM_VERSION) Platform: $(OPM_PLATFORM) Arch: $(OPM_ARCH)" && \
		echo "Installing OPM $(OPM_VERSION) ($(BUILD_HARNESS_OS)-$(BUILD_HARNESS_ARCH)-opm) from $(OPM_URL)" && \
		curl '-#' -fL -o $(OPM) $(OPM_URL) && \
		chmod +x $(OPM) \
		)
