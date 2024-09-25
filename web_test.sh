#!/bin/bash

# サーバのベースURL
SERVER_URL="http://localhost:8010"

# presence_history用のユーザーID
USER_ID=123

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
