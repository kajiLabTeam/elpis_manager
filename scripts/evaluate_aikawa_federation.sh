#!/usr/bin/env bash
set -euo pipefail

# Evaluate aikawa fingerprints via proxy /api/service/inquiry with selectable registrations.
# Sends the latest BLE/WiFi pairs per room and reports per-room accuracy.

PROXY_BASE="${PROXY_BASE:-http://localhost:8080}"
AIKAWA_DIR="${AIKAWA_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/aikawa}"
PAIR_LIMIT="${PAIR_LIMIT:-12}"
SLEEP_SEC="${SLEEP_SEC:-0.2}"
VERBOSE="${VERBOSE:-1}"
SKIP_REGISTER="${SKIP_REGISTER:-0}"

MANAGER_URI="${MANAGER_URI:-manager}"
MANAGER_ROOM="${MANAGER_ROOM:-514}"
MANAGER_PORT="${MANAGER_PORT:-8010}"

ECHO_URI="${ECHO_URI:-echo}"
ECHO_ROOM="${ECHO_ROOM:-513}"
ECHO_PORT="${ECHO_PORT:-8011}"

BRAVO_URI="${BRAVO_URI:-bravo}"
BRAVO_ROOM="${BRAVO_ROOM:-102}"
BRAVO_PORT="${BRAVO_PORT:-8013}"

CHARLIE_URI="${CHARLIE_URI:-charlie}"
CHARLIE_ROOM="${CHARLIE_ROOM:-103}"
CHARLIE_PORT="${CHARLIE_PORT:-8014}"

command -v jq >/dev/null 2>&1 || {
  echo "jq is required. Install it first." >&2
  exit 1
}

if [[ ! -d "${AIKAWA_DIR}" ]]; then
  echo "AIKAWA_DIR not found: ${AIKAWA_DIR}" >&2
  exit 1
fi

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
    4|manager+echo+bravo+charlie|manager-echo-bravo-charlie|manager_echo_bravo_charlie)
      echo "manager+echo+bravo+charlie"
      ;;
    *)
      return 1
      ;;
  esac
}

if [[ -n "${REGISTER_SET}" ]]; then
  if ! normalized_set="$(normalize_register_set "${REGISTER_SET}")"; then
    echo "REGISTER_SET must be manager / manager+echo / manager+echo+bravo / manager+echo+bravo+charlie." >&2
    exit 1
  fi
  REGISTER_SET="${normalized_set}"
elif [[ -t 0 ]]; then
  echo "Select registration set:"
  echo "  1) manager only"
  echo "  2) manager + echo"
  echo "  3) manager + echo + bravo"
  echo "  4) manager + echo + bravo + charlie"
  read -r choice
  if ! normalized_set="$(normalize_register_set "${choice}")"; then
    echo "Invalid choice. Use 1/2/3/4." >&2
    exit 1
  fi
  REGISTER_SET="${normalized_set}"
else
  REGISTER_SET="manager+echo"
fi

register_org() {
  local uri="$1"
  local room="$2"
  local port="$3"
  local body
  body=$(cat <<JSON
{"system_uri":"${uri}","roomID":"${room}","port":${port}}
JSON
)
  echo "== register ${uri} -> ${PROXY_BASE}/api/service/register"
  curl -fsS --connect-timeout 3 --max-time 10 -X POST "${PROXY_BASE}/api/service/register" \
    -H "Content-Type: application/json" \
    --data-binary "${body}" >/dev/null
}

expected_room_id() {
  local room="$1"
  if (( room >= 110 && room <= 119 )); then
    echo "513"
  elif (( room >= 120 && room <= 129 )); then
    echo "514"
  elif (( room >= 130 && room <= 134 )); then
    echo "590"
  elif (( room >= 401 && room <= 410 )); then
    echo "102"
  elif (( room >= 411 && room <= 420 )); then
    echo "103"
  elif (( room >= 421 && room <= 425 )); then
    echo "190"
  else
    echo ""
  fi
}

collect_pairs() {
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

  local -a sorted_ts=()
  while IFS= read -r ts; do
    [[ -n "${ts}" ]] || continue
    sorted_ts+=("${ts}")
  done < <(printf '%s\n' "${ts_list[@]}" | sort -u -nr)
  ts_list=("${sorted_ts[@]}")

  local count=0
  for ts in "${ts_list[@]}"; do
    printf '%s\t%s\t%s\n' "${room_dir}/ble_data_${ts}.csv" "${room_dir}/wifi_data_${ts}.csv" "${ts}"
    count=$((count + 1))
    if (( count >= PAIR_LIMIT )); then
      break
    fi
  done
}

TOTAL_ALL=0
CORRECT_ALL=0
SUMMARY_LINES=()

run_room() {
  local room="$1"
  local expected="$2"
  local room_dir="${AIKAWA_DIR}/${room}"

  if [[ ! -d "${room_dir}" ]]; then
    echo "Room ${room}: directory missing, skip."
    SUMMARY_LINES+=("Room ${room}: N/A (0/0)")
    return
  fi

  local -a pairs=()
  while IFS= read -r line; do
    pairs+=("${line}")
  done < <(collect_pairs "${room_dir}")

  local total="${#pairs[@]}"
  if (( total == 0 )); then
    echo "Room ${room}: no pairs found."
    SUMMARY_LINES+=("Room ${room}: N/A (0/0)")
    return
  fi

  local correct=0
  local ok_resp=0
  local err_resp=0
  local missing_room=0
  local idx=1

  for entry in "${pairs[@]}"; do
    IFS=$'\t' read -r ble wifi ts <<<"${entry}"

    if [[ ! -f "${ble}" || ! -f "${wifi}" ]]; then
      err_resp=$((err_resp + 1))
      echo "Room ${room} #${idx}: missing files (ble=${ble}, wifi=${wifi})"
      idx=$((idx + 1))
      if [[ "${SLEEP_SEC}" != "0" && "${SLEEP_SEC}" != "0.0" ]]; then
        sleep "${SLEEP_SEC}"
      fi
      continue
    fi

    local tmp_body
    tmp_body="$(mktemp)"
    local status curl_rc
    set +e
    status="$(curl -s -o "${tmp_body}" -w '%{http_code}' -X POST "${PROXY_BASE}/api/service/inquiry" \
      -F "wifi_data=@${wifi}" \
      -F "ble_data=@${ble}")"
    curl_rc=$?
    set -e
    if (( curl_rc != 0 )); then
      status="000"
    fi

    if [[ "${status}" -lt 200 || "${status}" -ge 300 ]]; then
      err_resp=$((err_resp + 1))
      echo "Room ${room} #${idx}: request failed (status=${status}, curl_rc=${curl_rc}, ts=${ts})"
      head -n 1 "${tmp_body}" | sed 's/^/  body: /'
      rm -f "${tmp_body}"
      idx=$((idx + 1))
      if [[ "${SLEEP_SEC}" != "0" && "${SLEEP_SEC}" != "0.0" ]]; then
        sleep "${SLEEP_SEC}"
      fi
      continue
    fi

    local resp room_id perc
    resp="$(cat "${tmp_body}")"
    rm -f "${tmp_body}"

    room_id="$(echo "${resp}" | jq -r '.room_id // .RoomID // .roomID // empty')"
    perc="$(echo "${resp}" | jq -r '.percentage_processed // .PercentageProcessed // .percentage // empty')"
    ok_resp=$((ok_resp + 1))
    if [[ -z "${room_id}" ]]; then
      missing_room=$((missing_room + 1))
    fi

    if [[ "${room_id}" == "${expected}" ]]; then
      correct=$((correct + 1))
      if [[ "${VERBOSE}" == "1" ]]; then
        echo "Room ${room} #${idx}: status=${status} ts=${ts} expected=${expected} got=${room_id} (ok) perc=${perc:-N/A} ble=$(basename "${ble}") wifi=$(basename "${wifi}")"
      fi
    else
      if [[ "${VERBOSE}" == "1" ]]; then
        echo "Room ${room} #${idx}: status=${status} ts=${ts} expected=${expected} got=${room_id:-N/A} perc=${perc:-N/A} ble=$(basename "${ble}") wifi=$(basename "${wifi}")"
      fi
    fi

    idx=$((idx + 1))
    if [[ "${SLEEP_SEC}" != "0" && "${SLEEP_SEC}" != "0.0" ]]; then
      sleep "${SLEEP_SEC}"
    fi
  done

  local acc
  acc="$(awk -v c="${correct}" -v t="${total}" 'BEGIN{printf "%.1f", (t>0)?(c*100)/t:0}')"
  echo "Room ${room} expected=${expected}: ${correct}/${total} (${acc}%) ok=${ok_resp} err=${err_resp} room_id_missing=${missing_room}"
  SUMMARY_LINES+=("Room ${room}: ${acc}% (${correct}/${total})")

  TOTAL_ALL=$((TOTAL_ALL + total))
  CORRECT_ALL=$((CORRECT_ALL + correct))
}

if [[ "${SKIP_REGISTER}" != "1" ]]; then
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
    manager+echo+bravo+charlie)
      register_org "${MANAGER_URI}" "${MANAGER_ROOM}" "${MANAGER_PORT}"
      register_org "${ECHO_URI}" "${ECHO_ROOM}" "${ECHO_PORT}"
      register_org "${BRAVO_URI}" "${BRAVO_ROOM}" "${BRAVO_PORT}"
      register_org "${CHARLIE_URI}" "${CHARLIE_ROOM}" "${CHARLIE_PORT}"
      ;;
    *)
      echo "Invalid REGISTER_SET: ${REGISTER_SET}" >&2
      exit 1
      ;;
  esac
fi

rooms=()
for i in {110..119}; do rooms+=("${i}"); done
for i in {120..129}; do rooms+=("${i}"); done
for i in {130..134}; do rooms+=("${i}"); done
for i in {401..410}; do rooms+=("${i}"); done
for i in {411..420}; do rooms+=("${i}"); done
for i in {421..425}; do rooms+=("${i}"); done

for room in "${rooms[@]}"; do
  expected="$(expected_room_id "${room}")"
  if [[ -z "${expected}" ]]; then
    continue
  fi
  run_room "${room}" "${expected}"
done

if (( TOTAL_ALL > 0 )); then
  overall="$(awk -v c="${CORRECT_ALL}" -v t="${TOTAL_ALL}" 'BEGIN{printf "%.1f", (c*100)/t}')"
  echo "Overall accuracy: ${CORRECT_ALL}/${TOTAL_ALL} (${overall}%)"
fi

if ((${#SUMMARY_LINES[@]} > 0)); then
  echo
  echo "Summary:"
  for line in "${SUMMARY_LINES[@]}"; do
    echo "${line}"
  done
fi
