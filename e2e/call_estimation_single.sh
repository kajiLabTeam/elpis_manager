#!/usr/bin/env bash
set -euo pipefail

# Call ALL estimation API with a selectable aikawa BLE/WiFi pair.
# Usage:
#   ./scripts/call_all_estimation_aikawa.sh [room_id] [pair_ts]
# Env:
#   ALL_ESTIMATION_BASE : base URL (default: http://localhost:8105)
#   AIKAWA_DIR          : base directory (default: repo_root/stash/20260205)
#   AIKAWA_ROOM         : room directory (e.g. 421)
#   PAIR_TS             : timestamp suffix (e.g. 1756799559)
#   PAIR_INDEX          : 1-based index for the latest 12 pairs

ALL_ESTIMATION_BASE="${ALL_ESTIMATION_BASE:-http://localhost:8105}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
AIKAWA_DIR="${AIKAWA_DIR:-${REPO_ROOT}/stash/20260205}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required. Install it first." >&2
  exit 1
fi

if [[ -z "${ALL_ESTIMATION_BASE}" ]]; then
  echo "Set ALL_ESTIMATION_BASE first." >&2
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

# Room and pair selection from aikawa
AIKAWA_ROOM="${AIKAWA_ROOM:-${1:-}}"
PAIR_TS="${PAIR_TS:-${2:-}}"
PAIR_INDEX="${PAIR_INDEX:-}"

ROOM_IDS=()
for dir in "${AIKAWA_DIR}"/*; do
  [[ -d "${dir}" ]] || continue
  base="$(basename "${dir}")"
  if [[ "${base}" =~ ^[0-9]+$ ]]; then
    if (( (base >= 110 && base <= 119) || (base >= 120 && base <= 129) || (base >= 130 && base <= 135) || (base >= 401 && base <= 425) )); then
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
  echo "Select room directory:"
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
  if ((${#PAIR_TS_LIST[@]} >= 12)); then
    break
  fi
done < <(printf '%s\n' "${PAIR_TS_ALL[@]}" | sort -u -nr)

if [[ -n "${PAIR_TS}" ]]; then
  found=0
  for ts in "${PAIR_TS_LIST[@]}"; do
    if [[ "${ts}" == "${PAIR_TS}" ]]; then
      found=1
      break
    fi
  done
  if [[ "${found}" -ne 1 ]]; then
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
    printf '  %d) %s\n' "${idx}" "${ts}"
    idx=$((idx + 1))
  done
  read -r pair_choice
  if [[ "${pair_choice}" =~ ^[0-9]+$ ]] && (( pair_choice >= 1 && pair_choice <= ${#PAIR_TS_LIST[@]} )); then
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

echo "== all_estimation predict -> ${ALL_ESTIMATION_BASE}/predict"
resp=$(curl -fsS -X POST "${ALL_ESTIMATION_BASE}/predict" \
  -F wifi_data=@"${wifi_csv}" \
  -F ble_data=@"${ble_csv}")
pretty_json "${resp}"
print_room_id "${resp}"
