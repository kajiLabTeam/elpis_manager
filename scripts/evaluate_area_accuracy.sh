#!/usr/bin/env bash
set -euo pipefail

# 屋内測位サービスの推定精度を評価するスクリプト。
# 指定された各エリアについて、BLE/Wi-Fi の同一 UNIXTIME（ファイル名ベース）ペアを
# 10 件ずつ送信し、期待した RoomID と一致する割合を算出する。
#
# 期待エリアとデータパス:
# - 組織A 514: stash/20251223/120〜129
# - 組織B 513: stash/20251223/110〜119
# - 5階廊下:   stash/20251223/130〜134
# - 1階廊下:   stash/20251223/417〜421
#
# 環境変数:
# - PROXY_BASE        … 問い合わせ先エンドポイント (デフォルト: http://localhost:8080)
# - DATA_ROOT         … データルート (デフォルト: <repo>/stash/20251223)
# - SAMPLES_PER_CASE  … 各エリアから送信するサンプル数 (デフォルト: 10)

PROXY_BASE="${PROXY_BASE:-http://localhost:8080}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_ROOT="${DATA_ROOT:-${REPO_ROOT}/stash/20251223}"
SAMPLES_PER_CASE="${SAMPLES_PER_CASE:-10}"

command -v jq >/dev/null 2>&1 || {
  echo "jq が必要です。インストールしてから実行してください。" >&2
  exit 1
}

# BLE/Wi-Fi の同一ベース名（UNIXTIME）を見つけてペアを返す。
collect_pairs() {
  # 第1引数: 取得する上限件数, 残り: ディレクトリ一覧
  local limit="$1"; shift
  local -a pairs=()
  local dir base

  for dir in "$@"; do
    [[ -d "$dir" ]] || continue

    while IFS= read -r base; do
      pairs+=("${dir}/ble_data_${base}.csv::${dir}/wifi_data_${base}.csv")
      [[ ${#pairs[@]} -ge $limit ]] && break 2
    done < <(
      comm -12 \
        <(for f in "$dir"/ble_data_*.csv; do [[ -e "$f" ]] || continue; basename "${f%.csv}" | sed 's/^ble_data_//'; done | sort -u) \
        <(for f in "$dir"/wifi_data_*.csv; do [[ -e "$f" ]] || continue; basename "${f%.csv}" | sed 's/^wifi_data_//'; done | sort -u)
    )
  done

  printf '%s\n' "${pairs[@]}"
}

evaluate_case() {
  # 第1引数: ラベル, 第2引数: 期待RoomID, 第3引数: 上限件数, 残り: ディレクトリ一覧
  local label="$1" expected="$2" limit="$3"; shift 3
  local -a pairs=()

  while IFS= read -r line; do
    pairs+=("$line")
  done < <(collect_pairs "$limit" "$@")

  local total="${#pairs[@]}"
  if [[ "$total" -lt "$limit" ]]; then
    echo "[WARN] ${label}: BLE/Wi-Fi ペアが ${total}/${limit} 件しか見つかりませんでした。" >&2
  fi

  local correct=0 idx=1 tmp_body status resp room perc
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
echo "DATA_ROOT=${DATA_ROOT}"
echo "SAMPLES_PER_CASE=${SAMPLES_PER_CASE}"
echo

evaluate_case "組織A(514)" "514" "${SAMPLES_PER_CASE}" \
  "${DATA_ROOT}/120" "${DATA_ROOT}/121" "${DATA_ROOT}/122" "${DATA_ROOT}/123" "${DATA_ROOT}/124" \
  "${DATA_ROOT}/125" "${DATA_ROOT}/126" "${DATA_ROOT}/127" "${DATA_ROOT}/128" "${DATA_ROOT}/129"

evaluate_case "組織B(513)" "513" "${SAMPLES_PER_CASE}" \
  "${DATA_ROOT}/110" "${DATA_ROOT}/111" "${DATA_ROOT}/112" "${DATA_ROOT}/113" "${DATA_ROOT}/114" \
  "${DATA_ROOT}/115" "${DATA_ROOT}/116" "${DATA_ROOT}/117" "${DATA_ROOT}/118" "${DATA_ROOT}/119"

evaluate_case "5階廊下(590)" "590" "${SAMPLES_PER_CASE}" \
  "${DATA_ROOT}/130" "${DATA_ROOT}/131" "${DATA_ROOT}/132" "${DATA_ROOT}/133" "${DATA_ROOT}/134"

evaluate_case "1階廊下(190)" "190" "${SAMPLES_PER_CASE}" \
  "${DATA_ROOT}/417" "${DATA_ROOT}/418" "${DATA_ROOT}/419" "${DATA_ROOT}/420" "${DATA_ROOT}/421"
