#!/bin/bash

# サーバのポートを指定
GO_APP_PORT="8010"  # サーバが異なるポートで実行されている場合は調整してください

# テスト用のBLE CSVファイル名
BLE_CSV_FILE="ble_data.csv"
WIFI_CSV_FILE="wifi_data.csv"

# 現在のタイムスタンプ
CURRENT_TIMESTAMP=$(date +%s)

# テストケースの定義（配列を使用）
TEST_NAMES=("EqualThreshold" "AboveThreshold" "BelowThreshold" "DeviceNotFound" "NoBLEData")
RSSI_VALUES=(-65 -64 -66 0 0)  # DeviceNotFoundとNoBLEDataの場合はRSSI値は使用しません

# しきい値（データベースに設定されている値と一致させてください）
THRESHOLD=-65

# 各テストケースを実行
for i in "${!TEST_NAMES[@]}"; do
    TEST_NAME=${TEST_NAMES[$i]}
    RSSI_VALUE=${RSSI_VALUES[$i]}

    echo "=== テストケース: $TEST_NAME ==="

    # BLE CSVデータを作成
    if [ "$TEST_NAME" == "DeviceNotFound" ]; then
        # デバイスが見つからない場合のBLEデータ（未知のUUIDを使用）
        cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,unknown-uuid,$RSSI_VALUE
EOL
    elif [ "$TEST_NAME" == "NoBLEData" ]; then
        # BLEデータが空の場合
        cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
EOL
    else
        # 通常のBLEデータ
        cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,8ebc2114-4abd-ba0d-b7c6-ff0a00200050,$RSSI_VALUE
EOL
    fi

    # WiFi CSVデータを作成（内容はテストに影響しないため固定）
    cat > $WIFI_CSV_FILE <<EOL
timestamp,bssid,rssi
$CURRENT_TIMESTAMP,66:77:88:99:AA:BB,-60
EOL

    echo "Sending test data to /api/signals/submit..."

    # /api/signals/submit にデータを送信
    RESPONSE=$(curl -s -F "ble_data=@$BLE_CSV_FILE" -F "wifi_data=@$WIFI_CSV_FILE" http://localhost:$GO_APP_PORT/api/signals/submit)
    echo "Response from /api/signals/submit: $RESPONSE"

    echo "Sending test data to /api/signals/server..."

    # /api/signals/server にデータを送信
    RESPONSE=$(curl -s -F "ble_data=@$BLE_CSV_FILE" -F "wifi_data=@$WIFI_CSV_FILE" http://localhost:$GO_APP_PORT/api/signals/server)
    echo "Response from /api/signals/server: $RESPONSE"

    echo ""
done

# 後片付け
rm -f $BLE_CSV_FILE $WIFI_CSV_FILE

echo "全てのテストが完了しました。"
