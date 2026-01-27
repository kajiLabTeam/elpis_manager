#!/usr/bin/env bash
set -euo pipefail

# Prepare samples for ALL model and optionally train it.
# Usage:
#   ./scripts/train_all_model.sh
# Env:
#   AIKAWA_DIR     : base directory (default: repo_root/aikawa)
#   ALL_EST_DIR    : base directory (default: repo_root/all_estimation)
#   FINGERPRINT_DIR: fingerprint output dir (default: all_estimation/manager_fingerprint)
#   MODEL_DIR      : model output dir for local training (default: all_estimation/model)
#   PAIR_LIMIT     : number of pairs per room (default: 12)
#   RESET_DEST     : 1 to clear existing samples in target labels (default: 1)
#   USE_DOCKER     : 1 to run training via docker compose (default: 1)
#   RUN_TRAIN      : 1 to run training after sample prep (default: 1)

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
AIKAWA_DIR="${AIKAWA_DIR:-${REPO_ROOT}/aikawa}"
ALL_EST_DIR="${ALL_EST_DIR:-${REPO_ROOT}/all_estimation}"
FINGERPRINT_DIR="${FINGERPRINT_DIR:-${ALL_EST_DIR}/manager_fingerprint}"
MODEL_DIR="${MODEL_DIR:-${ALL_EST_DIR}/model}"
PAIR_LIMIT="${PAIR_LIMIT:-12}"
RESET_DEST="${RESET_DEST:-1}"
USE_DOCKER="${USE_DOCKER:-1}"
RUN_TRAIN="${RUN_TRAIN:-1}"

if [[ ! -d "${AIKAWA_DIR}" ]]; then
  echo "AIKAWA_DIR not found: ${AIKAWA_DIR}" >&2
  exit 1
fi

for label in 102 103 513 514 590 190; do
  mkdir -p "${FINGERPRINT_DIR}/${label}"
done

if [[ "${RESET_DEST}" == "1" ]]; then
  for label in 102 103 513 514 590 190; do
    rm -f "${FINGERPRINT_DIR}/${label}"/*_data_*.csv
  done
fi

select_pairs() {
  local room_dir="$1"
  local -a ts_list=()

  for ble in "${room_dir}"/ble_data_*.csv; do
    [[ -e "${ble}" ]] || continue
    local ts="${ble##*_}"
    ts="${ts%.csv}"
    local wifi="${room_dir}/wifi_data_${ts}.csv"
    if [[ -f "${wifi}" ]]; then
      ts_list+=("${ts}")
    fi
  done

  if ((${#ts_list[@]} == 0)); then
    return 0
  fi

  printf '%s\n' "${ts_list[@]}" | sort -n | head -n "${PAIR_LIMIT}"
}

copy_room_range() {
  local start="$1"
  local end="$2"
  local label="$3"

  for room in $(seq "${start}" "${end}"); do
    local room_dir="${AIKAWA_DIR}/${room}"
    if [[ ! -d "${room_dir}" ]]; then
      echo "[WARN] room ${room}: directory missing, skip."
      continue
    fi

    local ts_list
    ts_list="$(select_pairs "${room_dir}")"
    if [[ -z "${ts_list}" ]]; then
      echo "[WARN] room ${room}: no BLE/WiFi pairs found."
      continue
    fi

    local count=0
    while IFS= read -r ts; do
      [[ -n "${ts}" ]] || continue
      cp -f "${room_dir}/ble_data_${ts}.csv" "${FINGERPRINT_DIR}/${label}/"
      cp -f "${room_dir}/wifi_data_${ts}.csv" "${FINGERPRINT_DIR}/${label}/"
      count=$((count + 1))
    done <<< "${ts_list}"

    echo "room ${room} -> ${label}: ${count} pairs"
  done
}

echo "== collect samples for ALL model =="
copy_room_range 110 119 513
copy_room_range 120 129 514
copy_room_range 130 135 590
copy_room_range 401 410 102
copy_room_range 411 420 103
copy_room_range 421 425 190

if [[ "${RUN_TRAIN}" != "1" ]]; then
  exit 0
fi

echo "== train ALL model =="
if [[ "${USE_DOCKER}" == "1" ]]; then
  docker compose run --rm all_estimation-model
else
  FINGERPRINT_DIR="${FINGERPRINT_DIR}" MODEL_DIR="${MODEL_DIR}" \
    python3 "${ALL_EST_DIR}/src/estimation/main.py"
fi
