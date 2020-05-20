SONAR_GO_TEST_ARGS ?= ./...


.PHONY: sonar/go
## Run SonarCloud analysis for Go on Travis CI. This will not be run during local development.
sonar/go: go/gosec-install
	go test -coverprofile=coverage.out -json ${SONAR_GO_TEST_ARGS} > report.json
	gosec -fmt sonarqube -out gosec.json -no-fail ./...
	unset SONARQUBE_SCANNER_PARAMS
	sonar-scanner --debug


.PHONY: sonar/go/openshiftci
## Run SonarCloud analysis for Go on OpenShift CI. This will not be run during local development.
sonar/go/openshiftci: export SONAR_TOKEN="$(shell cat /etc/sonarcloud/token 2> /dev/null)"
sonar/go/openshiftci:
	@echo "Verify sonar token from kube secret has been mounted"
	@if [[ -z "${SONAR_TOKEN}" ]] ; then echo "---> ERROR: SONAR_TOKEN is not set" ; exit 1 ; fi
	
	@echo "Running go test"
	go test -coverprofile=coverage.out -json ${SONAR_GO_TEST_ARGS} | tee report.json "${ARTIFACT_DIR}/sonar_gotest.json"
	
	@echo "Running gosec" 
	gosec -fmt sonarqube -no-fail ./... | tee gosec.json "${ARTIFACT_DIR}/sonar_gosec.json"
	
	@echo "Starting Sonar Scanner"
	@echo "JOB_TYPE=${JOB_TYPE}"
	@echo "PULL_BASE_REF=${PULL_BASE_REF}"
	@echo "PULL_NUMBER=${PULL_NUMBER}"
	@echo "BRANCH=$(shell curl -s "https://api.github.com/repos/${REPO_OWNER}/${REPO_OWNER}/pulls/${PULL_NUMBER}" | jq ".head.ref")"
	@if [[ "${JOB_TYPE}" == "presubmit" ]]; then \
		echo "sonar.pullrequest.base=${PULL_BASE_REF}" >> sonar-project.properties ; \
		echo "sonar.pullrequest.key=${PULL_NUMBER}" >> sonar-project.properties ; \
		echo "sonar.pullrequest.branch=$(shell curl -s "https://api.github.com/repos/${REPO_OWNER}/${REPO_OWNER}/pulls/${PULL_NUMBER}" | jq ".head.ref")" >> sonar-project.properties ; \
	fi

	@sonar-scanner -Dsonar.login=${SONAR_TOKEN} --debug | tee "${ARTIFACT_DIR}/sonar.log"


# This expects that your code uses Jest to execute tests.
# Add this field to your jest.config.js file to generate the report:
#     testResultProcessor: 'jest-sonar-reporter',
# It must be run before make component/test/unit.
.PHONY: sonar/js/jest-init
## Install npm module to make Sonar test reports in Jest on Travis. This will not be run during local development.
sonar/js/jest-init:
	npm install -D jest-sonar-reporter


# Test reports and code coverage must be generated before running the scanner.
# It must be run after make component/test/unit.
.PHONY: sonar/js
## Runs the SonarCloud analysis for JavaScript on Travis. This will not be run during local development.
sonar/js:
	unset SONARQUBE_SCANNER_PARAMS
	sonar-scanner --debug
