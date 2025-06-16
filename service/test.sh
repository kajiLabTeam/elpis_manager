# ① 組織・部屋マッピング登録
curl -sX POST http://localhost:8012/api/register -H 'Content-Type: application/json' -d '{"management_server_url":"https://orgA.example.com","proxy_server_url":"https://proxA.example.com","mapping":[{"floor":"1","room_id":"101","room_name":"第一会議室"},{"floor":"2","room_id":"201","room_name":"第二会議室"}]}' | jq .

# ② 照会サーバ自己登録
curl -sX POST http://localhost:8012/api/partners/register -H 'Content-Type: application/json' -d '{"inquiry_server_uri":"https://inq.example.com","port":8010,"latitude":35.123456,"longitude":136.123456,"description":"テストビル"}' | jq .

# ③ 位置問い合わせ（Wi-Fi/BLE CSV をワンライナー生成）
curl -s -H 'Authorization: Basic dXNlcjpQYXNzV29yZEAxMjM=' -X POST http://localhost:8012/api/query \
  -F latitude=35.123456 -F longitude=136.123456 -F timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ") \
  -F wifi_data=@<(printf 'UNIXTIME,BSSID,RSSI,SSID\n%d,00:14:22:01:23:45,-45,Wi-Fi1\n%d,00:25:96:FF:FE:0C,-55,Wi-Fi2\n' $(date +%s) $(date +%s)) \
  -F ble_data=@<(printf 'UNIXTIME,MACADDRESS,RSSI,ServiceUUIDs\n%d,A1:B2:C3:D4:E5:F6,-65,0000AAFE-0000-1000-8000-00805F9B34FB\n%d,2E:3C:A8:03:7C:0A,-70,0000FEAA-0000-1000-8000-00805F9B34FB\n' $(date +%s) $(date +%s)) | jq .
