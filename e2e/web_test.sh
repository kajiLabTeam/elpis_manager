#!/bin/bash

# ローカル環境のサーバのベースURLとポートを指定します
LOCAL_SERVER_URL="http://localhost:8010"

# 本番環境のサーバのベースURLを指定します
PROD_SERVER_URL="https://elpis-m1.kajilab.dev"

# presence_history用のユーザーID
USER_ID=1

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
    SERVER_URL="${LOCAL_SERVER_URL}"
elif [[ "$ENV" == "本番環境" ]]; then
    SERVER_URL="${PROD_SERVER_URL}"
else
    echo "無効な環境が選択されました。スクリプトを終了します。"
    exit 1
fi

# presence_historyエンドポイントにリクエスト
echo "ユーザーID $USER_ID の在室履歴を取得中..."

# 実行するcurlコマンドを表示
PRESENCE_HISTORY_CURL="curl -s \"${SERVER_URL}/api/presence_history?user_id=${USER_ID}\" -H \"Accept: application/json\""
echo "実行コマンド: $PRESENCE_HISTORY_CURL"

# curlコマンドを実行してレスポンスを取得
response=$(curl -s "${SERVER_URL}/api/presence_history?user_id=${USER_ID}" -H "Accept: application/json")

# ステータスチェック
if [ $? -eq 0 ]; then
    echo "在室履歴の取得に成功しました。結果:"
    echo "$response" | jq .
else
    echo "在室履歴の取得に失敗しました。"
fi

# current_occupantsエンドポイントにリクエスト
echo "現在の在室者情報を取得中..."

# 実行するcurlコマンドを表示
CURRENT_OCCUPANTS_CURL="curl -s \"${SERVER_URL}/api/current_occupants\" -H \"Accept: application/json\""
echo "実行コマンド: $CURRENT_OCCUPANTS_CURL"

# curlコマンドを実行してレスポンスを取得
response=$(curl -s "${SERVER_URL}/api/current_occupants" -H "Accept: application/json")

# ステータスチェック
if [ $? -eq 0 ]; then
    echo "現在の在室者情報の取得に成功しました。結果:"
    echo "$response" | jq .
else
    echo "現在の在室者情報の取得に失敗しました。"
fi
