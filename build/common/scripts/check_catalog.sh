#!/bin/bash

# Max number of retries
MAX_RETRIES=7
# Retry counter
retry_count=0

# Function to check the output and continue if the condition is met
check_and_continue() {
  GREEN=$'\e[0;32m'
  NC=$'\e[0m'
  RED=$'\e[0;31m'

  # Run the command and count matching lines
  result_count=$(kubectl get packagemanifest -A | grep -c GRC)

  if [ "$result_count" -gt 0 ]; then
    echo "${GREEN}Found these grc fake operators..${NC}"
    kubectl get packagemanifest -A | grep GRC
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
    kubectl get catalogsource -n olm grc-mock-source  -o yaml
    kubectl get pod -n olm
    kubectl describe pods -l olm.catalogSource=grc-mock-source -n olm
    echo "${RED}Max retries reached. Exiting.${NC}"
    exit 1
  fi

  # Wait before retrying
  sleep 10
done
