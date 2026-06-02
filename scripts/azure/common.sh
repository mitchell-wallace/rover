#!/usr/bin/env bash
# Shared helpers + configuration for Rover's Azure scripts.
#
# These scripts are usable on their own (export ROVER_* vars or rely on the
# defaults below) and are also driven by the Rover Go CLI, which exports the
# same ROVER_* variables from its state file before invoking them.
#
# Config resolution for every value: environment variable > default.
set -euo pipefail

# --- paths ------------------------------------------------------------------
# COMMON_DIR = scripts/azure ; ASSET_ROOT = repo/asset root (two levels up).
COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSET_ROOT="$(cd "${COMMON_DIR}/../.." && pwd)"

# --- configuration (override via env) ---------------------------------------
ROVER_RESOURCE_GROUP="${ROVER_RESOURCE_GROUP:-rover-rg}"
ROVER_LOCATION="${ROVER_LOCATION:-australiaeast}"
ROVER_VM_NAME="${ROVER_VM_NAME:-rover-vm}"
ROVER_ADMIN_USER="${ROVER_ADMIN_USER:-${USER:-rover}}"
ROVER_SSH_PUBKEY="${ROVER_SSH_PUBKEY:-$HOME/.ssh/id_rsa.pub}"
# Private key is derived from the public key path unless set explicitly.
ROVER_SSH_KEY="${ROVER_SSH_KEY:-${ROVER_SSH_PUBKEY%.pub}}"
ROVER_DEPLOYMENT_NAME="${ROVER_DEPLOYMENT_NAME:-rover-deploy}"
ROVER_BICEP="${ROVER_BICEP:-${ASSET_ROOT}/infra/bicep/main.bicep}"
ROVER_SUBSCRIPTION="${ROVER_SUBSCRIPTION:-}"

# --- logging ----------------------------------------------------------------
log()  { printf '\033[1;34m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
err()  { printf '\033[1;31m[error]\033[0m %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

# --- azure helpers ----------------------------------------------------------
az_args=()
az_init() {
  require_cmd az
  if [ -n "${ROVER_SUBSCRIPTION}" ]; then
    az_args=(--subscription "${ROVER_SUBSCRIPTION}")
  fi
}

# azx: az with the optional subscription argument applied.
azx() {
  az "$@" "${az_args[@]}"
}

# Ensure the resource providers Rover needs are registered (one-time per
# subscription). Registration is async; we kick it off and continue.
ensure_providers() {
  local p state
  for p in Microsoft.Compute Microsoft.Network; do
    state="$(azx provider show -n "$p" --query registrationState -o tsv 2>/dev/null || echo Unknown)"
    if [ "$state" != "Registered" ]; then
      log "Registering resource provider $p (one-time)..."
      azx provider register -n "$p" --wait -o none 2>/dev/null || azx provider register -n "$p" -o none || true
    fi
  done
}

rg_exists() {
  [ "$(azx group exists --name "${ROVER_RESOURCE_GROUP}" -o tsv)" = "true" ]
}

vm_exists() {
  azx vm show -g "${ROVER_RESOURCE_GROUP}" -n "${ROVER_VM_NAME}" -o none 2>/dev/null
}

# Echoes the power state, e.g. "VM running" / "VM deallocated", or "absent".
vm_power_state() {
  if ! vm_exists; then
    echo "absent"
    return 0
  fi
  azx vm get-instance-view -g "${ROVER_RESOURCE_GROUP}" -n "${ROVER_VM_NAME}" \
    --query "instanceView.statuses[?starts_with(code, 'PowerState/')].displayStatus | [0]" \
    -o tsv 2>/dev/null || echo "unknown"
}

# Emits connection info as JSON to stdout. Reads from live Azure resources so it
# works even if the local state file is missing.
emit_connection_info() {
  local power pubip fqdn privip vmsize
  power="$(vm_power_state)"
  if [ "${power}" = "absent" ]; then
    printf '{"exists":false,"powerState":"absent","vmName":"%s","resourceGroup":"%s","location":"%s"}\n' \
      "${ROVER_VM_NAME}" "${ROVER_RESOURCE_GROUP}" "${ROVER_LOCATION}"
    return 0
  fi
  vmsize="$(azx vm show -g "${ROVER_RESOURCE_GROUP}" -n "${ROVER_VM_NAME}" --query 'hardwareProfile.vmSize' -o tsv 2>/dev/null || echo '')"
  pubip="$(azx vm list-ip-addresses -g "${ROVER_RESOURCE_GROUP}" -n "${ROVER_VM_NAME}" \
    --query '[0].virtualMachine.network.publicIpAddresses[0].ipAddress' -o tsv 2>/dev/null || echo '')"
  # FQDN lives on the public-IP resource (named <vm>-pip by our Bicep), not on
  # the VM's ip-addresses view.
  fqdn="$(azx network public-ip show -g "${ROVER_RESOURCE_GROUP}" -n "${ROVER_VM_NAME}-pip" \
    --query 'dnsSettings.fqdn' -o tsv 2>/dev/null || echo '')"
  privip="$(azx vm list-ip-addresses -g "${ROVER_RESOURCE_GROUP}" -n "${ROVER_VM_NAME}" \
    --query '[0].virtualMachine.network.privateIpAddresses[0]' -o tsv 2>/dev/null || echo '')"

  local host="${fqdn:-$pubip}"
  cat <<JSON
{"exists":true,"powerState":"${power}","vmName":"${ROVER_VM_NAME}","resourceGroup":"${ROVER_RESOURCE_GROUP}","location":"${ROVER_LOCATION}","vmSize":"${vmsize}","adminUsername":"${ROVER_ADMIN_USER}","publicIp":"${pubip}","fqdn":"${fqdn}","privateIp":"${privip}","sshTarget":"${ROVER_ADMIN_USER}@${host}"}
JSON
}

# True if the first arg matches a flag in the remaining args.
has_flag() {
  local needle="$1"; shift
  local a
  for a in "$@"; do [ "$a" = "$needle" ] && return 0; done
  return 1
}
