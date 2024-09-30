#!/bin/bash

# サーバのポートを指定します
GO_APP_PORT="8010"  # サーバが異なるポートで実行されている場合は調整してください

# テスト用のBLEとWiFiのCSVファイル名を指定します
BLE_CSV_FILE="ble_data.csv"
WIFI_CSV_FILE="wifi_data.csv"

# 現在のタイムスタンプを取得します
CURRENT_TIMESTAMP=$(date +%s)

# テストケースの定義（配列を使用）
TEST_NAMES=(
    "しきい値と同じRSSI値"
    "しきい値より強いRSSI値"
    "しきい値より弱いRSSI値"
    "デバイスが見つからない場合"
    "BLEデータが空の場合"
    "複数UUIDの検出（部屋1が強い）"      # 新しいテストケース1
    "複数UUIDの検出（部屋2が強い）"      # 新しいテストケース2
    "複数UUIDの検出（両方同じ強さ）"      # 新しいテストケース3
)

# しきい値（データベースに設定されている値と一致させてください）
THRESHOLD=-65

# Basic認証のユーザー名とパスワード（パスワードは不要ですが、curlの仕様上指定が必要です）
BASIC_AUTH_USER="user1"
BASIC_AUTH_PASS="password"  # 任意の値

# 部屋ごとのUUIDを明確に定義
ROOM1_UUID="4e24ac47-b7e6-44f5-957f-1cdcefa2acab"  # 部屋ID: 1
ROOM2_UUID="517557dc-f2d6-42f1-9695-f9883f856a70"  # 部屋ID: 2

# メニューを表示し、実行したいテストケースを選択します
echo "実行したいテストケースを選択してください:"
select TEST_NAME in "${TEST_NAMES[@]}"; do
    if [[ -n "$TEST_NAME" ]]; then
        echo "選択されたテストケース: $TEST_NAME"
        break
    else
        echo "無効な選択です。もう一度お試しください。"
    fi
done

# テストケースに応じてRSSI値を設定します
case "$TEST_NAME" in
    "しきい値と同じRSSI値")
        RSSI_VALUE=$THRESHOLD
        ;;
    "しきい値より強いRSSI値")
        RSSI_VALUE=$(($THRESHOLD + 1))
        ;;
    "しきい値より弱いRSSI値")
        RSSI_VALUE=$(($THRESHOLD - 1))
        ;;
    "デバイスが見つからない場合" | "BLEデータが空の場合")
        RSSI_VALUE=0  # RSSI値は使用しない
        ;;
    "複数UUIDの検出（部屋1が強い）")
        # 部屋1を強く、部屋2を弱く設定
        RSSI_VALUE_ROOM1=$(($THRESHOLD + 5))  # -60（強い）
        RSSI_VALUE_ROOM2=$(($THRESHOLD - 5))  # -70（弱い）
        ;;
    "複数UUIDの検出（部屋2が強い）")
        # 部屋2を強く、部屋1を弱く設定
        RSSI_VALUE_ROOM1=$(($THRESHOLD - 3))  # -68（弱い）
        RSSI_VALUE_ROOM2=$(($THRESHOLD + 4))  # -61（強い）
        ;;
    "複数UUIDの検出（両方同じ強さ）")
        # 部屋1と部屋2の両方を同じ強さに設定
        RSSI_VALUE_ROOM1=$(($THRESHOLD + 2))  # -63（強い）
        RSSI_VALUE_ROOM2=$(($THRESHOLD + 2))  # -63（強い）
        ;;
    *)
        echo "無効なテストケースです。"
        exit 1
        ;;
esac

# BLE CSVデータを作成します
if [ "$TEST_NAME" == "デバイスが見つからない場合" ]; then
    # 未知のUUIDを使用してBLEデータを作成します
    cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,unknown-uuid,$RSSI_VALUE
EOL
elif [ "$TEST_NAME" == "BLEデータが空の場合" ]; then
    # ヘッダーのみの空のBLEデータを作成します
    cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
EOL
elif [[ "$TEST_NAME" == "複数UUIDの検出（部屋1が強い）" || "$TEST_NAME" == "複数UUIDの検出（部屋2が強い）" || "$TEST_NAME" == "複数UUIDの検出（両方同じ強さ）" ]]; then
    # 複数UUIDを含むBLEデータを作成します
    if [[ "$TEST_NAME" == "複数UUIDの検出（部屋1が強い）" ]]; then
        BLE_DATA="
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,$ROOM1_UUID,$RSSI_VALUE_ROOM1
$CURRENT_TIMESTAMP,$ROOM2_UUID,$RSSI_VALUE_ROOM2
"
    elif [[ "$TEST_NAME" == "複数UUIDの検出（部屋2が強い）" ]]; then
        BLE_DATA="
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,$ROOM1_UUID,$RSSI_VALUE_ROOM1
$CURRENT_TIMESTAMP,$ROOM2_UUID,$RSSI_VALUE_ROOM2
"
    elif [[ "$TEST_NAME" == "複数UUIDの検出（両方同じ強さ）" ]]; then
        BLE_DATA="
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,$ROOM1_UUID,$RSSI_VALUE_ROOM1
$CURRENT_TIMESTAMP,$ROOM2_UUID,$RSSI_VALUE_ROOM2
"
    fi
    echo "$BLE_DATA" > $BLE_CSV_FILE
else
    # 通常のBLEデータを作成します
    cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,722eb21f-8f6a-4ba9-a12f-05c0f970a177,$RSSI_VALUE
EOL
fi

# WiFi CSVデータを作成します（内容はテストに影響しないため固定）
cat > $WIFI_CSV_FILE <<EOL
timestamp,bssid,rssi
$CURRENT_TIMESTAMP,66:77:88:99:AA:BB,-60
EOL

echo "データを /api/signals/submit に送信しています..."

# /api/signals/submit にデータを送信します（Basic認証を追加）
RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" \
    -F "ble_data=@$BLE_CSV_FILE" \
    -F "wifi_data=@$WIFI_CSV_FILE" \
    http://localhost:$GO_APP_PORT/api/signals/submit)
echo "サーバからのレスポンス: $RESPONSE"

echo "データを /api/signals/server に送信しています..."

# /api/signals/server にデータを送信します（Basic認証を追加）
RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" \
    -F "ble_data=@$BLE_CSV_FILE" \
    -F "wifi_data=@$WIFI_CSV_FILE" \
    http://localhost:$GO_APP_PORT/api/signals/server)
echo "サーバからのレスポンス: $RESPONSE"

# 後片付けとして、一時的に作成したCSVファイルを削除します
rm -f $BLE_CSV_FILE $WIFI_CSV_FILE

echo "テストが完了しました。"
