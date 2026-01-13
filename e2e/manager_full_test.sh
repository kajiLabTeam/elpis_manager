#!/bin/bash

# サーバのポートを指定します（ローカル環境用）
LOCAL_GO_APP_PORTS=(
    "8010"
    "8011"
)

# 本番環境のURLを指定します
PROD_URLS=(
    "https://elpis-m1.kajilab.dev"
    "https://elpis-m2.kajilab.dev"
)

# ディレクトリの候補を指定します
# 送信したいファイルが含まれるトップレベルのディレクトリを指定します
SAMPLE_DIRS=(
    "./manager_estimation/judgement"
)

# Basic認証のユーザー名とパスワード（パスワードは任意の値）
BASIC_AUTH_USER="hihumikan"
BASIC_AUTH_PASS="password"

# 環境選択のメニュー
ENVIRONMENTS=(
    "ローカル環境 (8010)"
    "ローカル環境 (8011)"
    "本番環境1 (elpis-m1)"
    "本番環境2 (elpis-m2)"
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
if [[ "$ENV" == "ローカル環境 (8010)" ]]; then
    BASE_URL="http://localhost:${LOCAL_GO_APP_PORTS[0]}/api/signals"
elif [[ "$ENV" == "ローカル環境 (8011)" ]]; then
    BASE_URL="http://localhost:${LOCAL_GO_APP_PORTS[1]}/api/signals"
elif [[ "$ENV" == "本番環境1 (elpis-m1)" ]]; then
    BASE_URL="${PROD_URLS[0]}/api/signals"
elif [[ "$ENV" == "本番環境2 (elpis-m2)" ]]; then
    BASE_URL="${PROD_URLS[1]}/api/signals"
else
    echo "無効な環境が選択されました。スクリプトを終了します。"
    exit 1
fi

# 使用するディレクトリを選択
echo "使用するデータディレクトリを選択してください:"
select SAMPLE_DIR in "${SAMPLE_DIRS[@]}"; do
    if [[ -d "$SAMPLE_DIR" ]]; then
        echo "選択されたディレクトリ: $SAMPLE_DIR"
        break
    else
        echo "無効なディレクトリが選択されました。もう一度お試しください。"
    fi
done

# 送信先のエンドポイントを選択
ENDPOINTS=(
    "submit"
    "server"
)

echo "データを送信するエンドポイントを選択してください:"
select ENDPOINT in "${ENDPOINTS[@]}"; do
    if [[ -n "$ENDPOINT" ]]; then
        echo "選択されたエンドポイント: $ENDPOINT"
        break
    else
        echo "無効な選択です。もう一度お試しください。"
    fi
done

FULL_URL="${BASE_URL}/${ENDPOINT}"
echo "データを ${FULL_URL} に送信します..."

# 指定ディレクトリ以下からBLEとWiFiのCSVファイルをそれぞれ取得（再帰的に検索）
BLE_FILES=($(find "$SAMPLE_DIR" -type f -name "ble_data_*.csv"))
WIFI_FILES=($(find "$SAMPLE_DIR" -type f -name "wifi_data_*.csv"))

if [ ${#BLE_FILES[@]} -eq 0 ]; then
    echo "BLEのCSVファイルが見つかりません。スクリプトを終了します。"
    exit 1
fi

if [ ${#WIFI_FILES[@]} -eq 0 ]; then
    echo "WiFiのCSVファイルが見つかりません。スクリプトを終了します。"
    exit 1
fi

# BLEファイルごとにunixtimeを抽出し、同じディレクトリ内にあるWiFiファイルとペアにして送信
for ble_file in "${BLE_FILES[@]}"; do
    # ファイル名からunixtimeを抽出（例: ble_data_1738627149.csv → 1738627149）
    if [[ $(basename "$ble_file") =~ ble_data_([0-9]+)\.csv ]]; then
        timestamp="${BASH_REMATCH[1]}"
        # BLEファイルと同じディレクトリを取得
        dir=$(dirname "$ble_file")
        # 同じディレクトリ内の対応するWiFiファイルのパスを組み立てる
        wifi_file_candidate="${dir}/wifi_data_${timestamp}.csv"
        if [ -f "$wifi_file_candidate" ]; then
            echo "[$timestamp] ペアのファイルを送信します: "
            echo "   BLE: $ble_file"
            echo "   WiFi: $wifi_file_candidate"
            # curlコマンドで送信（Basic認証付き）
            RESPONSE=$(curl -s -u "$BASIC_AUTH_USER:$BASIC_AUTH_PASS" \
                -F "ble_data=@$ble_file" \
                -F "wifi_data=@$wifi_file_candidate" \
                "$FULL_URL")
            echo "サーバからのレスポンス: $RESPONSE"
            # 1秒間の待機を挟む
            sleep 1
        else
            echo "[$timestamp] 対応するWiFiファイル (wifi_data_${timestamp}.csv) が見つかりません。"
        fi
    else
        echo "ファイル名からタイムスタンプを抽出できませんでした: $ble_file"
    fi
done

echo "全てのペアの送信が完了しました。"
