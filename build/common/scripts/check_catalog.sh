#!/bin/bash

# Be strict about pipelines; failures are handled via retries.
set -o pipefail

# Max number of retries
MAX_RETRIES=7
# Retry counter
retry_count=0

# Listing packagemanifests cluster-wide (-A) is expensive and can overload the API server during kind bootstrap.
# PackageManifests are typically available in the "default" namespace (see e2e usage), so scope queries there.
PACKAGEMANIFEST_NAMESPACE="${PACKAGEMANIFEST_NAMESPACE:-olm}"

# Give kubectl more time; kind+podman can be slow during OLM bootstrap.
KUBECTL_TIMEOUT="${KUBECTL_TIMEOUT:-60s}"

# Function to check the output and continue if the condition is met
check_and_continue() {
  GREEN=$'\e[0;32m'
  NC=$'\e[0m'
  RED=$'\e[0;31m'

  # Fail fast if the API server isn't responding (avoids hammering it and causing TLS handshake timeouts).
  kubectl --request-timeout="${KUBECTL_TIMEOUT}" get --raw=/readyz >/dev/null 2>&1 || {
    echo "${RED}API server not ready (readyz failed). Retrying...${NC}"
    return 1
  }

  # PackageManifest names are just operator names (e.g. "example-operator") and won't include the catalog display
  # name. Use the standard table output (scoped to one namespace) so the "GRC mock operators" catalog display name
  # appears in the output for matching.
  output="$(kubectl --request-timeout="${KUBECTL_TIMEOUT}" get packagemanifest -n "${PACKAGEMANIFEST_NAMESPACE}" --no-headers 2>&1)"
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "${RED}kubectl failed (exit ${rc}); will retry:${NC}"
    echo "${RED}${output}${NC}"
    return 1
  fi

  # Count matching lines
  result_count="$(echo "${output}" | grep -c GRC || true)"

  if [ "$result_count" -gt 0 ]; then
    echo "${GREEN}Found these grc fake operators..${NC}"
    echo "${output}" | grep GRC
    echo "${GREEN}GRC found ($result_count lines). Continuing...${NC}"
    return 0
  else
    echo "${RED}GRC not found. Retrying...${NC}"
    return 1
  fi
}

# Retry loop
while ! check_and_continue; do
  retry_count=$((retry_count + 1))

  if [ "$retry_count" -ge "$MAX_RETRIES" ]; then
    kubectl --request-timeout="${KUBECTL_TIMEOUT}" get catalogsource -n olm grc-mock-source -o yaml || true
    kubectl --request-timeout="${KUBECTL_TIMEOUT}" get pod -n olm || true
    kubectl --request-timeout="${KUBECTL_TIMEOUT}" get packagemanifest -n "${PACKAGEMANIFEST_NAMESPACE}" || true
    echo "${RED}Max retries reached. Exiting.${NC}"
    exit 1
  fi

  # Wait before retrying (simple backoff)
  sleep $((10 + retry_count * 5))
done
