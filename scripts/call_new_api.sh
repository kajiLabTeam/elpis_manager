#!/usr/bin/env bash
set -euo pipefail

# This script demonstrates the new federation APIs:
# 1) POST /api/service/register (proxy) … registers roomID per system_uri
# 2) POST /api/service/inquiry  (proxy) … federated inquiry across organizations
# 3) POST /api/proxy/inquiry    (service)… service-side forwarder with Basic Auth

# Override endpoints if you run outside Docker.
PROXY_BASE="${PROXY_BASE:-http://localhost:8080}"
SERVICE_BASE="${SERVICE_BASE:-http://localhost:8012}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq が必要です。brew install jq などでインストールしてください。" >&2
  exit 1
fi

if [[ -z "${PROXY_BASE}" || -z "${SERVICE_BASE}" ]]; then
  echo "PROXY_BASE と SERVICE_BASE が空です。環境変数で指定するかデフォルトを使ってください。" >&2
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

# Basic auth for the service forwarder (set BASIC_AUTH_USER/PASS to change)
BASIC_AUTH_USER="${BASIC_AUTH_USER:-user}"
BASIC_AUTH_PASS="${BASIC_AUTH_PASS:-PassWord@123}"

# temp files for sample WiFi/BLE CSVs
wifi_csv="$(mktemp)"
ble_csv="$(mktemp)"
trap 'rm -f "$wifi_csv" "$ble_csv"' EXIT

now="$(date +%s)"
cat >"$wifi_csv" <<EOF
UNIXTIME,BSSID,RSSI,SSID
$now,00:14:22:01:23:45,-45,Wi-Fi1
$now,00:25:96:FF:FE:0C,-55,Wi-Fi2
EOF

cat >"$ble_csv" <<EOF
UNIXTIME,MACADDRESS,RSSI,ServiceUUIDs
$now,A1:B2:C3:D4:E5:F6,-65,0000AAFE-0000-1000-8000-00805F9B34FB
$now,2E:3C:A8:03:7C:0A,-70,0000FEAA-0000-1000-8000-00805F9B34FB
EOF

SYSTEM_URI="${SYSTEM_URI:-manager}"
ROOM_ID="${ROOM_ID:-R066}"
SYSTEM_PORT="${SYSTEM_PORT:-8010}"

echo "== 1) service registration -> $PROXY_BASE/api/service/register"
reg_body=$(cat <<JSON
{"system_uri":"$SYSTEM_URI","roomID":"$ROOM_ID","port":$SYSTEM_PORT}
JSON
)
reg_resp=$(curl -fsS -X POST "$PROXY_BASE/api/service/register" \
  -H "Content-Type: application/json" \
  --data-binary "${reg_body}")
pretty_json "${reg_resp}"

echo "== 2) federated inquiry (proxy) -> $PROXY_BASE/api/service/inquiry"
inq_proxy_resp=$(curl -fsS -X POST "$PROXY_BASE/api/service/inquiry" \
  -F wifi_data=@"$wifi_csv" \
  -F ble_data=@"$ble_csv")
pretty_json "${inq_proxy_resp}"

echo "== 3) federated inquiry via service forwarder -> $SERVICE_BASE/api/proxy/inquiry"
inq_service_resp=$(curl -fsS -X POST "$SERVICE_BASE/api/proxy/inquiry" \
  -u "${BASIC_AUTH_USER}:${BASIC_AUTH_PASS}" \
  -F wifi_data=@"$wifi_csv" \
  -F ble_data=@"$ble_csv")
pretty_json "${inq_service_resp}"
