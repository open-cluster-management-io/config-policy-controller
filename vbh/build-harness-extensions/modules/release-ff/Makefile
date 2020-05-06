RELEASE_FF_BRANCH ?= $(shell cat RELEASE_FF_BRANCH)

.PHONY: release-ff
## Fast-forward from master branch to branch defined in RELEASE_FF_BRANCH file in the dev repo
release-ff:
	${BUILD_HARNESS_EXTENSIONS_PATH}/modules/release-ff/bin/release_ff.sh $(RELEASE_FF_BRANCH)
