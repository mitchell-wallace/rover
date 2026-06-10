#!/usr/bin/env bash
# Unit tests for scripts/azure/common.sh helper functions.
#
# Run: bash scripts/azure/test_common.sh
#
# Real-backend verification notes (update when re-verifying against live Azure):
#
# Last verified: 2026-06-10 against Azure subscription in australiaeast.
#
#   az vm show --query hardwareProfile.vmSize:
#     → "Standard_B2als_v2" (burstable/xsmall)
#     → "Standard_B2as_v2" (burstable/small)
#
#   az vm get-instance-view --query "instanceView.statuses[?starts_with(code,'PowerState/')].displayStatus|[0]":
#     → "VM running"     (when VM is on)
#     → "VM deallocated" (when deallocated by rover down)
#     → "VM stopped"     (when stopped without deallocating)
#
#   sku_for must stay in sync with infra/bicep/main.bicep and internal/sizes/sizes.go.
set -euo pipefail

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/azure/common.sh
source "${COMMON_DIR}/common.sh"

PASS=0
FAIL=0

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  if [ "${expected}" = "${actual}" ]; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    echo "FAIL: ${desc}: expected '${expected}', got '${actual}'"
  fi
}

assert_true() {
  local desc="$1" actual="$2"
  if [ "${actual}" = "true" ]; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    echo "FAIL: ${desc}: expected true, got '${actual}'"
  fi
}

assert_false() {
  local desc="$1" actual="$2"
  if [ "${actual}" = "false" ]; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    echo "FAIL: ${desc}: expected false, got '${actual}'"
  fi
}

# --- sku_for tests (verified 2026-06-10 against main.bicep vmSkus matrix) ---

echo "Testing sku_for..."

# Burstable family
assert_eq "burstable/xsmall" "Standard_B2als_v2" "$(sku_for burstable xsmall)"
assert_eq "burstable/small"  "Standard_B2as_v2"  "$(sku_for burstable small)"
assert_eq "burstable/medium" "Standard_B4als_v2" "$(sku_for burstable medium)"
assert_eq "burstable/large"  "Standard_B4as_v2"  "$(sku_for burstable large)"

# Balanced family
assert_eq "balanced/small"  "Standard_D2as_v7" "$(sku_for balanced small)"
assert_eq "balanced/medium" "Standard_D4as_v7" "$(sku_for balanced medium)"
assert_eq "balanced/large"  "Standard_D8as_v7" "$(sku_for balanced large)"

# Ram-heavy family
assert_eq "ramheavy/small"  "Standard_E2as_v7" "$(sku_for ramheavy small)"
assert_eq "ramheavy/medium" "Standard_E4as_v7" "$(sku_for ramheavy medium)"
assert_eq "ramheavy/large"  "Standard_E8as_v7" "$(sku_for ramheavy large)"

# Invalid combo should fail (run in subshell so die/exit doesn't kill the test runner)
if (sku_for invalid size) 2>/dev/null; then
  FAIL=$((FAIL + 1))
  echo "FAIL: sku_for should reject invalid family:size combo"
else
  PASS=$((PASS + 1))
fi

# --- is_running tests (verified 2026-06-10 against real Azure displayStatus values) ---

echo "Testing is_running..."

# Real Azure displayStatus values
assert_true  "VM running"     "$(is_running "VM running"     && echo true || echo false)"
assert_false "VM deallocated" "$(is_running "VM deallocated" && echo true || echo false)"
assert_false "VM stopped"     "$(is_running "VM stopped"     && echo true || echo false)"
assert_false "absent"         "$(is_running "absent"         && echo true || echo false)"
assert_true  "vm RUNNING"     "$(is_running "vm RUNNING"     && echo true || echo false)"
assert_true  "Running"        "$(is_running "Running"        && echo true || echo false)"

# --- Results ---

echo ""
if [ "${FAIL}" -gt 0 ]; then
  echo "FAILED: ${PASS} passed, ${FAIL} failed"
  exit 1
fi
echo "PASSED: ${PASS} tests passed"
