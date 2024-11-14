#!/bin/bash

# サーバのURLを設定
URL="http://localhost:8010/api/fingerprint/collect"

# サンプルタイプ（'positive' または 'negative'）
SAMPLE_TYPE="positive"

# ルームIDを設定
ROOM_ID="1"

# アップロードするファイルのパスを指定
BLE_DATA_FILE="./fingerprint/ble_data_111.csv"
WIFI_DATA_FILE="./fingerprint/wifi_data_111.csv"

# ファイルが存在するか確認
if [ ! -f "$WIFI_DATA_FILE" ]; then
    echo "WiFiデータファイル '$WIFI_DATA_FILE' が見つかりません。"
    exit 1
fi

if [ ! -f "$BLE_DATA_FILE" ]; then
    echo "BLEデータファイル '$BLE_DATA_FILE' が見つかりません。"
    exit 1
fi

# POSTリクエストを送信
curl -X POST "$URL" \
  -F "sample_type=$SAMPLE_TYPE" \
  -F "room_id=$ROOM_ID" \
  -F "wifi_data=@$WIFI_DATA_FILE;type=text/csv" \
  -F "ble_data=@$BLE_DATA_FILE;type=text/csv" \
  -H "Content-Type: multipart/form-data"

# レスポンスを表示
echo
