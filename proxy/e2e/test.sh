#!/bin/bash

# 起動中のサーバーを停止するためにポートを使用するプロセスを探して終了させる
kill $(lsof -t -i:8080) 2>/dev/null

# Goサーバーをバックグラウンドで起動
make run &
SERVER_PID=$!
echo "Server started with PID: $SERVER_PID"
sleep 2  # サーバーが起動するのを待つ

# Register request
echo "Testing /api/register POST endpoint..."
curl -X POST http://localhost:8080/api/register \
    -H "Content-Type: application/json" \
    -d '{"system_uri":"http://example.com","port":8081}'

echo -e "\nTesting /api/register GET endpoint..."
curl -X GET http://localhost:8080/api/register

# Create sample CSV files for testing /api/inquiry if they don't exist
if [ ! -f wifi_data.csv ]; then
  echo "Creating wifi_data.csv..."
  cat <<EOL > wifi_data.csv
UNIX,BSSID,RSSI
1622551234,00:14:22:01:23:45,-45
1622551267,00:25:96:FF:FE:0C,-55
EOL
else
  echo "wifi_data.csv already exists. Using existing file."
fi

if [ ! -f ble_data.csv ]; then
  echo "Creating ble_data.csv..."
  cat <<EOL > ble_data.csv
UNIXTIME,MACADDRESS,RSSI,ServiceUUIDs
1622551234,A1:B2:C3:D4:E5:F6,-65,0000AAFE-0000-1000-8000-00805F9B34FB
1622551267,2E-3C-A8-03-7C-0A,-70,0000FEAA-0000-1000-8000-00805F9B34FB
EOL
else
  echo "ble_data.csv already exists. Using existing file."
fi

# Inquiry request
echo -e "\nTesting /api/inquiry POST endpoint..."
curl -X POST http://localhost:8080/api/inquiry \
    -F "wifi_data=@wifi_data.csv" \
    -F "ble_data=@ble_data.csv"

# サーバーを停止
echo "Stopping server with PID: $SERVER_PID"
kill $SERVER_PID
