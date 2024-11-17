# Filename: app.py

import pandas as pd
import numpy as np
import joblib
from fastapi import FastAPI, File, UploadFile, HTTPException
from fastapi.responses import JSONResponse
import uvicorn
import os

app = FastAPI(title="RSSI Percentage Prediction API")

# モデル保存用ディレクトリの指定
MODEL_DIR = 'model'

# モデル、スケーラー、pivot_columnsのロード
try:
    model_path = os.path.join(MODEL_DIR, 'trained_model.joblib')
    scaler_path = os.path.join(MODEL_DIR, 'scaler.joblib')
    pivot_columns_path = os.path.join(MODEL_DIR, 'pivot_columns.joblib')

    if not os.path.exists(model_path):
        raise FileNotFoundError(f"Model file not found at {model_path}")
    if not os.path.exists(scaler_path):
        raise FileNotFoundError(f"Scaler file not found at {scaler_path}")
    if not os.path.exists(pivot_columns_path):
        raise FileNotFoundError(f"Pivot columns file not found at {pivot_columns_path}")

    model = joblib.load(model_path)
    scaler = joblib.load(scaler_path)
    pivot_columns = joblib.load(pivot_columns_path)
    print("Model, scaler, and pivot_columns loaded successfully from the 'model' directory.")
except Exception as e:
    print(f"Error loading model, scaler, or pivot_columns: {e}")
    raise e

@app.get("/")
async def healthcheck():
    """
    ヘルスチェックエンドポイント
    """
    return {"status": "running"}

@app.post("/predict")
async def predict_percentage(
    wifi_data: UploadFile = File(...),
    ble_data: UploadFile = File(...)
):
    """
    WiFiとBLEのCSVファイルを受け取り、予測されたパーセンテージを返却します。

    - **wifi_data**: WiFi用のCSVファイル
    - **ble_data**: BLE用のCSVファイル
    """
    # ファイルの拡張子をチェック
    for file in [wifi_data, ble_data]:
        if not file.filename.endswith('.csv'):
            raise HTTPException(status_code=400, detail=f"Invalid file type for {file.filename}. Please upload a CSV file.")

    try:
        # WiFiデータの読み込み
        wifi_contents = await wifi_data.read()
        wifi_df = pd.read_csv(pd.io.common.BytesIO(wifi_contents), header=None, names=['timestamp', 'BSSID', 'rssi'])

        # BLEデータの読み込み
        ble_contents = await ble_data.read()
        ble_df = pd.read_csv(pd.io.common.BytesIO(ble_contents), header=None, names=['timestamp', 'UUID', 'rssi'])

        # ピボットテーブルの作成 (WiFiとBLEを統合する場合の例)
        combined_df = pd.concat([
            wifi_df.rename(columns={'BSSID': 'identifier'}),
            ble_df.rename(columns={'UUID': 'identifier'})
        ])
        pivot_df = combined_df.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
        pivot_df.fillna(-100, inplace=True)

        # 学習時のピボットテーブルと同じ列順に揃える
        pivot_df = pivot_df.reindex(columns=pivot_columns, fill_value=-100)

        # 特徴量の抽出
        X_judgement = pivot_df.values

        # データのスケーリング
        X_judgement_scaled = scaler.transform(X_judgement)

        # 予測
        y_pred_judgement = model.predict(X_judgement_scaled)

        # ファイル全体の適合度（平均値）を計算し、四捨五入して整数に変換
        average_percentage = int(np.round(np.mean(y_pred_judgement)))

        # 結果をJSONで返却（整数型）
        return JSONResponse(content={"percentage_processed": average_percentage})


    except Exception as e:
        raise HTTPException(status_code=500, detail=f"An error occurred during prediction: {e}")

if __name__ == "__main__":
    # FastAPI アプリケーションを実行
    uvicorn.run(app, host="0.0.0.0", port=8101)
