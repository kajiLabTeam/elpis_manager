#!/usr/bin/env bash
set -euo pipefail

# Federation demo with selectable org registration and aikawa dataset pairs.
# Usage:
#   ./scripts/call_new_api_multi_aikawa.sh [register_set] [room_id] [pair_ts]
# Env:
#   REGISTER_SET  : manager | manager+echo | manager+echo+bravo
#   AIKAWA_DIR    : base directory (default: repo_root/aikawa)
#   AIKAWA_ROOM   : room directory (e.g. 421)
#   PAIR_TS       : timestamp suffix (e.g. 1756799559)
#   PAIR_INDEX    : 1-based index for the latest 12 pairs

PROXY_BASE="${PROXY_BASE:-http://localhost:8080}"
SERVICE_BASE="${SERVICE_BASE:-http://localhost:8012}"

MANAGER_URI="${MANAGER_URI:-manager}"
MANAGER_ROOM="${MANAGER_ROOM:-514}"
MANAGER_PORT="${MANAGER_PORT:-8010}"

ECHO_URI="${ECHO_URI:-echo}"
ECHO_ROOM="${ECHO_ROOM:-513}"
ECHO_PORT="${ECHO_PORT:-8011}"

BRAVO_URI="${BRAVO_URI:-bravo}"
BRAVO_ROOM="${BRAVO_ROOM:-102}"
BRAVO_PORT="${BRAVO_PORT:-8013}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
AIKAWA_DIR="${AIKAWA_DIR:-${REPO_ROOT}/aikawa}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required. Install it first." >&2
  exit 1
fi

if [[ -z "${PROXY_BASE}" || -z "${SERVICE_BASE}" ]]; then
  echo "Set PROXY_BASE and SERVICE_BASE first." >&2
  exit 1
fi

if [[ ! -d "${AIKAWA_DIR}" ]]; then
  echo "AIKAWA_DIR not found: ${AIKAWA_DIR}" >&2
  exit 1
fi

pretty_json() {
  local body="$1"
  if ! echo "${body}" | jq .; then
    echo "--- raw response ---"
    echo "${body}"
    exit 1
  fi
}

print_room_id() {
  local body="$1"
  local room_id
  room_id="$(echo "${body}" | jq -r '.room_id // .RoomID // .roomID // empty')"
  if [[ -n "${room_id}" ]]; then
    echo "RoomID: ${room_id}"
  else
    echo "RoomID: (not found)"
  fi
}

# Basic auth for the service forwarder
BASIC_AUTH_USER="${BASIC_AUTH_USER:-user}"
BASIC_AUTH_PASS="${BASIC_AUTH_PASS:-PassWord@123}"

# Registration set: manager | manager+echo | manager+echo+bravo
# You can pass it as the first arg or via REGISTER_SET.
REGISTER_SET="${REGISTER_SET:-${1:-}}"

normalize_register_set() {
  case "$1" in
    1|manager)
      echo "manager"
      ;;
    2|manager+echo|manager-echo|manager_echo)
      echo "manager+echo"
      ;;
    3|manager+echo+bravo|manager-echo-bravo|manager_echo_bravo)
      echo "manager+echo+bravo"
      ;;
    *)
      return 1
      ;;
  esac
}

if [[ -n "${REGISTER_SET}" ]]; then
  if ! normalized_set="$(normalize_register_set "${REGISTER_SET}")"; then
    echo "REGISTER_SET must be manager / manager+echo / manager+echo+bravo." >&2
    exit 1
  fi
  REGISTER_SET="${normalized_set}"
elif [[ -t 0 ]]; then
  echo "Select registration set:"
  echo "  1) manager only"
  echo "  2) manager + echo"
  echo "  3) manager + echo + bravo"
  read -r choice
  if ! normalized_set="$(normalize_register_set "${choice}")"; then
    echo "Invalid choice. Use 1/2/3." >&2
    exit 1
  fi
  REGISTER_SET="${normalized_set}"
else
  REGISTER_SET="manager+echo"
fi

# Room and pair selection from aikawa
AIKAWA_ROOM="${AIKAWA_ROOM:-${2:-}}"
PAIR_TS="${PAIR_TS:-${3:-}}"
PAIR_INDEX="${PAIR_INDEX:-}"

ROOM_IDS=()
for dir in "${AIKAWA_DIR}"/*; do
  [[ -d "${dir}" ]] || continue
  base="$(basename "${dir}")"
  if [[ "${base}" =~ ^[0-9]+$ ]]; then
    if (( (base >= 110 && base <= 134) || (base >= 401 && base <= 425) )); then
      ROOM_IDS+=("${base}")
    fi
  fi
done

if ((${#ROOM_IDS[@]} == 0)); then
  echo "No room directories found under ${AIKAWA_DIR}." >&2
  exit 1
fi

OLD_IFS="${IFS}"
IFS=$'\n' ROOM_IDS_SORTED=($(printf '%s\n' "${ROOM_IDS[@]}" | sort -n))
IFS="${OLD_IFS}"

room_exists() {
  local target="$1"
  local id
  for id in "${ROOM_IDS_SORTED[@]}"; do
    if [[ "${id}" == "${target}" ]]; then
      return 0
    fi
  done
  return 1
}

if [[ -n "${AIKAWA_ROOM}" ]]; then
  if ! room_exists "${AIKAWA_ROOM}"; then
    echo "Room not in range or missing: ${AIKAWA_ROOM}" >&2
    exit 1
  fi
elif [[ -t 0 ]]; then
  echo "Select room directory (110-134, 401-425):"
  idx=1
  for id in "${ROOM_IDS_SORTED[@]}"; do
    printf '  %d) %s\n' "${idx}" "${id}"
    idx=$((idx + 1))
  done
  read -r room_choice
  if room_exists "${room_choice}"; then
    AIKAWA_ROOM="${room_choice}"
  elif [[ "${room_choice}" =~ ^[0-9]+$ ]] && (( room_choice >= 1 && room_choice <= ${#ROOM_IDS_SORTED[@]} )); then
    AIKAWA_ROOM="${ROOM_IDS_SORTED[$((room_choice - 1))]}"
  else
    echo "Invalid room selection." >&2
    exit 1
  fi
else
  echo "AIKAWA_ROOM is required when not running interactively." >&2
  exit 1
fi

ROOM_DIR="${AIKAWA_DIR}/${AIKAWA_ROOM}"
if [[ ! -d "${ROOM_DIR}" ]]; then
  echo "Room directory not found: ${ROOM_DIR}" >&2
  exit 1
fi

PAIR_TS_LIST=()
PAIR_TS_ALL=()
for ble in "${ROOM_DIR}"/ble_data_*.csv; do
  [[ -e "${ble}" ]] || continue
  ts="${ble##*_}"
  ts="${ts%.csv}"
  wifi="${ROOM_DIR}/wifi_data_${ts}.csv"
  if [[ -f "${wifi}" ]]; then
    PAIR_TS_ALL+=("${ts}")
  fi
done

if ((${#PAIR_TS_ALL[@]} == 0)); then
  echo "No BLE/WiFi pairs found in ${ROOM_DIR}." >&2
  exit 1
fi

while IFS= read -r ts; do
  PAIR_TS_LIST+=("${ts}")
done < <(printf '%s\n' "${PAIR_TS_ALL[@]}" | sort -u -nr)

if ((${#PAIR_TS_LIST[@]} > 12)); then
  PAIR_TS_LIST=("${PAIR_TS_LIST[@]:0:12}")
fi

pair_exists() {
  local target="$1"
  local ts
  for ts in "${PAIR_TS_LIST[@]}"; do
    if [[ "${ts}" == "${target}" ]]; then
      return 0
    fi
  done
  return 1
}

if [[ -n "${PAIR_TS}" ]]; then
  if ! pair_exists "${PAIR_TS}"; then
    echo "PAIR_TS must be one of the latest 12 pairs in ${ROOM_DIR}." >&2
    exit 1
  fi
elif [[ -n "${PAIR_INDEX}" ]]; then
  if [[ "${PAIR_INDEX}" =~ ^[0-9]+$ ]] && (( PAIR_INDEX >= 1 && PAIR_INDEX <= ${#PAIR_TS_LIST[@]} )); then
    PAIR_TS="${PAIR_TS_LIST[$((PAIR_INDEX - 1))]}"
  else
    echo "PAIR_INDEX out of range." >&2
    exit 1
  fi
elif [[ -t 0 ]]; then
  echo "Select BLE/WiFi pair (latest 12 timestamps):"
  idx=1
  for ts in "${PAIR_TS_LIST[@]}"; do
    printf '  %d) %s (ble_data_%s.csv / wifi_data_%s.csv)\n' "${idx}" "${ts}" "${ts}" "${ts}"
    idx=$((idx + 1))
  done
  read -r pair_choice
  if pair_exists "${pair_choice}"; then
    PAIR_TS="${pair_choice}"
  elif [[ "${pair_choice}" =~ ^[0-9]+$ ]] && (( pair_choice >= 1 && pair_choice <= ${#PAIR_TS_LIST[@]} )); then
    PAIR_TS="${PAIR_TS_LIST[$((pair_choice - 1))]}"
  else
    echo "Invalid pair selection." >&2
    exit 1
  fi
else
  echo "PAIR_TS or PAIR_INDEX is required when not running interactively." >&2
  exit 1
fi

ble_csv="${ROOM_DIR}/ble_data_${PAIR_TS}.csv"
wifi_csv="${ROOM_DIR}/wifi_data_${PAIR_TS}.csv"

if [[ ! -f "${ble_csv}" || ! -f "${wifi_csv}" ]]; then
  echo "Selected pair files not found:" >&2
  echo "  ${ble_csv}" >&2
  echo "  ${wifi_csv}" >&2
  exit 1
fi

echo "Using room ${AIKAWA_ROOM}"
echo "BLE:  ${ble_csv}"
echo "WiFi: ${wifi_csv}"

register_org() {
  local uri="$1"
  local room="$2"
  local port="$3"
  local body
  body=$(cat <<JSON
{"system_uri":"$uri","roomID":"$room","port":$port}
JSON
)
  echo "== register ${uri} (room ${room}, port ${port}) -> ${PROXY_BASE}/api/service/register"
  pretty_json "$(curl -fsS -X POST "${PROXY_BASE}/api/service/register" \
    -H "Content-Type: application/json" \
    --data-binary "${body}")"
}

case "${REGISTER_SET}" in
  manager)
    register_org "${MANAGER_URI}" "${MANAGER_ROOM}" "${MANAGER_PORT}"
    ;;
  manager+echo)
    register_org "${MANAGER_URI}" "${MANAGER_ROOM}" "${MANAGER_PORT}"
    register_org "${ECHO_URI}" "${ECHO_ROOM}" "${ECHO_PORT}"
    ;;
  manager+echo+bravo)
    register_org "${MANAGER_URI}" "${MANAGER_ROOM}" "${MANAGER_PORT}"
    register_org "${ECHO_URI}" "${ECHO_ROOM}" "${ECHO_PORT}"
    register_org "${BRAVO_URI}" "${BRAVO_ROOM}" "${BRAVO_PORT}"
    ;;
  *)
    echo "Invalid REGISTER_SET: ${REGISTER_SET}" >&2
    exit 1
    ;;
 esac

echo "== federated inquiry (proxy) -> ${PROXY_BASE}/api/service/inquiry"
inq_proxy_resp=$(curl -fsS -X POST "${PROXY_BASE}/api/service/inquiry" \
  -F wifi_data=@"${wifi_csv}" \
  -F ble_data=@"${ble_csv}")
pretty_json "${inq_proxy_resp}"
print_room_id "${inq_proxy_resp}"

echo "== federated inquiry via service forwarder -> ${SERVICE_BASE}/api/proxy/inquiry"
inq_service_resp=$(curl -fsS -X POST "${SERVICE_BASE}/api/proxy/inquiry" \
  -u "${BASIC_AUTH_USER}:${BASIC_AUTH_PASS}" \
  -F wifi_data=@"${wifi_csv}" \
  -F ble_data=@"${ble_csv}")
pretty_json "${inq_service_resp}"
print_room_id "${inq_service_resp}"
