#!/bin/bash

# サーバのポートを指定します（ローカル環境用）
LOCAL_GO_APP_PORT="8010"

# 本番環境のURLを指定します
PROD_URL="https://elpis-m1.kajilab.dev"

# CSVファイルが格納されているディレクトリを指定します
NEGATIVE_SAMPLES_DIR="./estimation/negative_samples"
POSITIVE_SAMPLES_DIR="./estimation/positive_samples"

# Basic認証のユーザー名とパスワード（パスワードは不要ですが、curlの仕様上指定が必要です）
BASIC_AUTH_USER="hihumikan"
BASIC_AUTH_PASS="password"  # 任意の値

# 部屋ごとのUUIDを明確に定義
ROOM1_UUID="4e24ac47-b7e6-44f5-957f-1cdcefa2acab"  # 部屋ID: 1
ROOM2_UUID="517557dc-f2d6-42f1-9695-f9883f856a70"  # 部屋ID: 2

# 環境選択のメニュー
ENVIRONMENTS=(
    "ローカル環境"
    "本番環境"
)

echo "送信先の環境を選択してください:"
select ENV in "${ENVIRONMENTS[@]}"; do
    if [[ -n "$ENV" ]]; then
        echo "選択された環境: $ENV"
        break
    else
        echo "無効な選択です。もう一度お試しください。"
    fi
done

# 環境に応じて送信先URLを設定
if [[ "$ENV" == "ローカル環境" ]]; then
    SUBMIT_URL="http://localhost:${LOCAL_GO_APP_PORT}/api/signals/submit"
    SERVER_URL="http://localhost:${LOCAL_GO_APP_PORT}/api/signals/server"
elif [[ "$ENV" == "本番環境" ]]; then
    SUBMIT_URL="${PROD_URL}/api/signals/submit"
    SERVER_URL="${PROD_URL}/api/signals/server"
else
    echo "無効な環境が選択されました。スクリプトを終了します。"
    exit 1
fi

# BLE CSVファイルのリストアップ
BLE_FILES=($(find "$NEGATIVE_SAMPLES_DIR" "$POSITIVE_SAMPLES_DIR" -type f -name "ble_data_*.csv"))

if [ ${#BLE_FILES[@]} -eq 0 ]; then
    echo "BLEのCSVファイルが見つかりません。スクリプトを終了します。"
    exit 1
fi

# WiFi CSVファイルのリストアップ
WIFI_FILES=($(find "$NEGATIVE_SAMPLES_DIR" "$POSITIVE_SAMPLES_DIR" -type f -name "wifi_data_*.csv"))

if [ ${#WIFI_FILES[@]} -eq 0 ]; then
    echo "WiFiのCSVファイルが見つかりません。スクリプトを終了します。"
    exit 1
fi

# BLE CSVファイルを選択
echo "使用するBLEのCSVファイルを選択してください:"
select BLE_FILE in "${BLE_FILES[@]}"; do
    if [[ -n "$BLE_FILE" ]]; then
        echo "選択されたBLEのCSVファイル: $BLE_FILE"
        break
    else
        echo "無効な選択です。もう一度お試しください。"
    fi
done

# WiFi CSVファイルを選択
echo "使用するWiFiのCSVファイルを選択してください:"
select WIFI_FILE in "${WIFI_FILES[@]}"; do
    if [[ -n "$WIFI_FILE" ]]; then
        echo "選択されたWiFiのCSVファイル: $WIFI_FILE"
        break
    else
        echo "無効な選択です。もう一度お試しください。"
    fi
done

echo "データを /api/signals/submit に送信しています..."

# /api/signals/submit にデータを送信します（Basic認証を追加）
RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" \
    -F "ble_data=@$BLE_FILE" \
    -F "wifi_data=@$WIFI_FILE" \
    "$SUBMIT_URL")
echo "サーバからのレスポンス: $RESPONSE"

echo "データを /api/signals/server に送信しています..."

# /api/signals/server にデータを送信します（Basic認証を追加）
RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" \
    -F "ble_data=@$BLE_FILE" \
    -F "wifi_data=@$WIFI_FILE" \
    "$SERVER_URL")
echo "サーバからのレスポンス: $RESPONSE"

echo "テストが完了しました。"
