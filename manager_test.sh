#!/bin/bash

# サーバのポートを指定します
GO_APP_PORT="8010"  # サーバが異なるポートで実行されている場合は調整してください

# テスト用のBLEとWiFiのCSVファイル名を指定します
BLE_CSV_FILE="ble_data.csv"
WIFI_CSV_FILE="wifi_data.csv"

# 現在のタイムスタンプを取得します
CURRENT_TIMESTAMP=$(date +%s)

# テストケースの定義（配列を使用）
TEST_NAMES=("しきい値と同じRSSI値" "しきい値より強いRSSI値" "しきい値より弱いRSSI値" "デバイスが見つからない場合" "BLEデータが空の場合")
# しきい値（データベースに設定されている値と一致させてください）
THRESHOLD=-65

# Basic認証のユーザー名とパスワード（パスワードは不要ですが、curlの仕様上指定が必要です）
BASIC_AUTH_USER="user1"
BASIC_AUTH_PASS="password"  # 任意の値

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
else
    # 通常のBLEデータを作成します
    cat > $BLE_CSV_FILE <<EOL
timestamp,uuid,rssi
$CURRENT_TIMESTAMP,8ebc2114-4abd-ba0d-b7c6-ff0a00200050,$RSSI_VALUE
EOL
fi

# WiFi CSVデータを作成します（内容はテストに影響しないため固定）
cat > $WIFI_CSV_FILE <<EOL
timestamp,bssid,rssi
$CURRENT_TIMESTAMP,66:77:88:99:AA:BB,-60
EOL

echo "データを /api/signals/submit に送信しています..."

# /api/signals/submit にデータを送信します（Basic認証を追加）
RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" -F "ble_data=@$BLE_CSV_FILE" -F "wifi_data=@$WIFI_CSV_FILE" https://elpis-m1.kajilab.dev/api/signals/submit)
echo "サーバからのレスポンス: $RESPONSE"

echo "データを /api/signals/server に送信しています..."

# /api/signals/server にデータを送信します（Basic認証を追加）
RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" -F "ble_data=@$BLE_CSV_FILE" -F "wifi_data=@$WIFI_CSV_FILE" https://elpis-m1.kajilab.dev/api/signals/server)
echo "サーバからのレスポンス: $RESPONSE"

# 後片付けとして、一時的に作成したCSVファイルを削除します
rm -f $BLE_CSV_FILE $WIFI_CSV_FILE

echo "テストが完了しました。"
