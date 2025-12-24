#!/usr/bin/env bash
set -u

# 屋内測位プロキシの推定結果を評価するスクリプト。
# 各カテゴリ（組織A=514、組織B=513、5階廊下=590、1階廊下=190）で
# BLE/Wi-Fiのペアを10件ずつ投げ、期待したRoomIDと合致する割合を出力する。
#
# PROXY_BASE 環境変数でエンドポイントを上書き可能（デフォルト: http://localhost:8080）。

PROXY_BASE="${PROXY_BASE:-http://localhost:8080}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_ROOT="${REPO_ROOT}/stash/20251223"

command -v jq >/dev/null 2>&1 || {
  echo "jq が必要です。インストールしてから実行してください。" >&2
  exit 1
}

collect_pairs() {
  # 第1引数: 上限件数, 残り: ディレクトリ一覧
  local limit="$1"; shift
  local -a pairs=()
  for dir in "$@"; do
    [[ -d "$dir" ]] || continue
    for ble in "$dir"/ble_data_*.csv; do
      [[ -e "$ble" ]] || continue
      local base="${ble##*/ble_data_}"
      local wifi="$dir/wifi_data_${base}"
      if [[ -f "$wifi" ]]; then
        pairs+=("$ble::$wifi")
        [[ ${#pairs[@]} -ge $limit ]] && break 2
      fi
    done
  done
  printf '%s\n' "${pairs[@]}"
}

run_case() {
  # 第1引数: ラベル, 第2引数: 期待RoomID, 第3引数: 上限件数, 残り: ディレクトリ一覧
  local label="$1" expected="$2" limit="$3"; shift 3
  local pairs=()
  while IFS= read -r line; do
    pairs+=("$line")
  done < <(collect_pairs "$limit" "$@")

  local total="${#pairs[@]}"
  if [[ "$total" -lt "$limit" ]]; then
    echo "[WARN] ${label}: ペアが ${total}/${limit} 件しかありませんでした。" >&2
  fi

  local correct=0 idx=1
  for entry in "${pairs[@]}"; do
    IFS="::" read -r ble wifi <<<"$entry"
    tmp_body="$(mktemp)"
    status="$(curl -s -o "${tmp_body}" -w '%{http_code}' -X POST "${PROXY_BASE}/api/service/inquiry" \
      -F "wifi_data=@${wifi}" \
      -F "ble_data=@${ble}" || true)"

    if [[ "${status}" -lt 200 || "${status}" -ge 300 ]]; then
      echo "${label} #${idx}: リクエスト失敗 (status=${status}, ble=${ble})"
      head -n 1 "${tmp_body}" | sed 's/^/  body: /'
      rm -f "${tmp_body}"
      ((idx++))
      continue
    fi

    resp="$(cat "${tmp_body}")"
    rm -f "${tmp_body}"

    local room
    room="$(echo "$resp" | jq -r '.room_id // .RoomID // .roomID // empty')"
    local perc
    perc="$(echo "$resp" | jq -r '.percentageProcessed // .PercentageProcessed // .percentage // empty')"

    if [[ "$room" == "$expected" ]]; then
      ((correct++))
      echo "${label} #${idx}: expected=${expected} got=${room} (✔ ${perc}%)"
    else
      echo "${label} #${idx}: expected=${expected} got=${room:-N/A} (${perc:-N/A}%)"
    fi
    ((idx++))
  done

  local acc
  if [[ "$total" -gt 0 ]]; then
    acc="$(awk -v c="$correct" -v t="$total" 'BEGIN{printf "%.1f", (c*100)/t}')"
  else
    acc="0.0"
  fi
  echo "${label} accuracy: ${correct}/${total} (${acc}%)"
  echo
}

echo "PROXY_BASE=${PROXY_BASE}"
echo "評価を開始します..."
echo

run_case "組織A(514)" "514" 10 \
  "${DATA_ROOT}/120" "${DATA_ROOT}/121" "${DATA_ROOT}/122" "${DATA_ROOT}/123" "${DATA_ROOT}/124" \
  "${DATA_ROOT}/125" "${DATA_ROOT}/126" "${DATA_ROOT}/127" "${DATA_ROOT}/128" "${DATA_ROOT}/129"

run_case "組織B(513)" "513" 10 \
  "${DATA_ROOT}/110" "${DATA_ROOT}/111" "${DATA_ROOT}/112" "${DATA_ROOT}/113" "${DATA_ROOT}/114" \
  "${DATA_ROOT}/115" "${DATA_ROOT}/116" "${DATA_ROOT}/117" "${DATA_ROOT}/118" "${DATA_ROOT}/119"

run_case "5階廊下(590)" "590" 10 \
  "${DATA_ROOT}/130" "${DATA_ROOT}/131" "${DATA_ROOT}/132" "${DATA_ROOT}/133" "${DATA_ROOT}/134"

run_case "1階廊下(190)" "190" 10 \
  "${DATA_ROOT}/417" "${DATA_ROOT}/418" "${DATA_ROOT}/419" "${DATA_ROOT}/420" "${DATA_ROOT}/421"
