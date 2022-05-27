#! /bin/bash

set -e

COVERAGE_MIN=${COVERAGE_MIN:-60}

COVERAGE_FILE_UNIT="coverage_unit.out"
COVERAGE_FILE_E2E="coverage_e2e.out"
COVERAGE_FILE="coverage.out"

get_coverage () {
  go tool cover -func=${1} 2>/dev/null | awk '/total:/ {print $3}'
}

print_coverage () {
  printf "%s\t%s\n" "${1}" "${2}"
}

# Merge coverage files from Unit and E2E tests
make coverage-merge

echo "* Coverage files found:"
ls -1 *.out

# Output total coverages
TOTAL_COVERAGE=$(get_coverage ${COVERAGE_FILE})

echo "================================"
print_coverage "Unit Test Coverage:" "$(get_coverage ${COVERAGE_FILE_UNIT})"
print_coverage "E2E Test Coverage:" "$(get_coverage ${COVERAGE_FILE_E2E})"
echo "--------------------------------"
print_coverage "TOTAL Test Coverage:" "${TOTAL_COVERAGE}"
echo "================================"

# Validate coverage
if (( ${TOTAL_COVERAGE%.*} < ${COVERAGE_MIN} )); then
  printf "\n%s\n" "ERROR: Total test coverage threshold of ${COVERAGE_MIN}% not met."
  exit 1
else
  printf "\n%s\n" "PASS: Total test coverage threshold of ${COVERAGE_MIN}% has been met."
fi
