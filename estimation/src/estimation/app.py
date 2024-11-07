# Filename: app.py

import pandas as pd
import numpy as np
import joblib
from fastapi import FastAPI, File, UploadFile, HTTPException
from fastapi.responses import JSONResponse
from typing import List
import uvicorn
import os

app = FastAPI(title="RSSI Percentage Prediction API")

# モデル、スケーラー、pivot_columnsのロード
try:
    model = joblib.load('trained_model.joblib')
    scaler = joblib.load('scaler.joblib')
    pivot_columns = joblib.load('pivot_columns.joblib')
    print("Model, scaler, and pivot_columns loaded successfully.")
except Exception as e:
    print(f"Error loading model, scaler, or pivot_columns: {e}")
    raise e

@app.post("/predict")
async def predict_percentage(file: UploadFile = File(...)):
    """
    CSVファイルを受け取り、予測されたパーセンテージを返却します。

    - **file**: CSVファイル
    """
    # ファイルの拡張子をチェック
    if not file.filename.endswith('.csv'):
        raise HTTPException(status_code=400, detail="Invalid file type. Please upload a CSV file.")

    try:
        # アップロードされたファイルをDataFrameに読み込む
        contents = await file.read()
        df = pd.read_csv(pd.io.common.BytesIO(contents), header=None, names=['timestamp', 'identifier', 'rssi'])

        # ピボットテーブルの作成
        pivot_df = df.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
        pivot_df.fillna(-100, inplace=True)

        # 学習時のピボットテーブルと同じ列順に揃える
        pivot_df = pivot_df.reindex(columns=pivot_columns, fill_value=-100)

        X_judgement = pivot_df.values

        # データのスケーリング
        X_judgement_scaled = scaler.transform(X_judgement)

        # 予測
        y_pred_judgement = model.predict(X_judgement_scaled)

        # ファイル全体の適合度（平均値）を計算
        average_percentage = np.mean(y_pred_judgement)

        # 結果をJSONで返却
        return JSONResponse(content={"predicted_percentage": f"{average_percentage:.2f}%"})

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"An error occurred during prediction: {e}")

if __name__ == "__main__":
    # FastAPI アプリケーションを実行
    uvicorn.run(app, host="0.0.0.0", port=8000)
