#!/bin/bash

# e2e_test.sh: エンドツーエンドテストスクリプト
# このスクリプトは manager/manager_fingerprint/ 内のCSVファイルを対象とし、
# ファイルが所属する直下のディレクトリ名が "0" ならネガティブ、それ以外ならポジティブと判別します。

# 設定
BASE_DIR="./manager/manager_fingerprint"
API_URL="${API_URL:-http://localhost:8101/predict}"  # APIエンドポイント

echo "APIエンドポイント: $API_URL"

# 必要なコマンドの確認
for cmd in curl jq; do
    if ! command -v $cmd &> /dev/null; then
        echo "Error: '$cmd' コマンドが見つかりません。インストールしてください。"
        exit 1
    fi
done

# BASE_DIR以下のCSVファイルを取得（サブディレクトリも対象）
FILES=$(find "$BASE_DIR" -type f -name "*.csv")
if [ -z "$FILES" ]; then
    echo "Error: $BASE_DIR 内にCSVファイルが存在しません。"
    exit 1
fi

# CSVファイルの一覧表示
echo "CSVファイルのリスト:"
FILE_ARRAY=()
index=1
while IFS= read -r line; do
    FILE_ARRAY+=("$line")
    echo "[$index] $line"
    index=$((index + 1))
done <<< "$FILES"

# ユーザーによるファイル選択
echo "ファイルを選択してください（番号を入力）: "
read -r selection

# 入力チェック
if ! [[ "$selection" =~ ^[0-9]+$ ]]; then
    echo "Error: 無効な選択です。数字を入力してください。"
    exit 1
fi

if (( selection < 1 || selection > ${#FILE_ARRAY[@]} )); then
    echo "Error: 無効な選択です。番号が範囲外です。"
    exit 1
fi

SELECTED_FILE="${FILE_ARRAY[$((selection - 1))]}"

# サンプル種別の判定：BASE_DIR直下のディレクトリ名を抽出
PARENT_DIR=$(dirname "$SELECTED_FILE")
# BASE_DIR以降のパス部分を取得（例："0" や "513" など）
RELATIVE_DIR=${PARENT_DIR#"$BASE_DIR/"}

if [ "$RELATIVE_DIR" = "0" ]; then
    SAMPLE_TYPE="Negative"
else
    SAMPLE_TYPE="Positive"
fi

echo "選択されたファイル: $SELECTED_FILE"
echo "サンプル種別判定: $SAMPLE_TYPE"

# POSTリクエスト送信
echo "CSVファイルを送信しています..."
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
  -H "accept: application/json" \
  -F "file=@$SELECTED_FILE" \
  "$API_URL")

# HTTPレスポンスの分割
HTTP_BODY=$(echo "$RESPONSE" | sed '$d')
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)

echo "APIレスポンスステータスコード: $HTTP_CODE"
echo "APIレスポンスボディ: $HTTP_BODY"

if [ "$HTTP_CODE" -ne 200 ]; then
    echo "Error: APIリクエストが失敗しました。ステータスコード: $HTTP_CODE"
    echo "レスポンス: $HTTP_BODY"
    exit 1
fi

# JSONレスポンスから予測値を抽出
PREDICTED_PERCENTAGE=$(echo "$HTTP_BODY" | jq -r '.predicted_percentage')

if [ "$PREDICTED_PERCENTAGE" == "null" ] || [ -z "$PREDICTED_PERCENTAGE" ]; then
    echo "Error: 予測結果が無効です。レスポンス: $HTTP_BODY"
    exit 1
fi

echo "テスト成功: 予測されたパーセンテージ = $PREDICTED_PERCENTAGE"
exit 0
