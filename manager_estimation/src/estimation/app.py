# Filename: app.py

import pandas as pd
import numpy as np
import joblib
from fastapi import FastAPI, File, UploadFile, HTTPException
from fastapi.responses import JSONResponse
import uvicorn
import os

app = FastAPI(title="RSSI Positive-Class Probability API")

MODEL_DIR = 'model'
POSITIVE_CLASS = '513'  # 正例ラベル

# モデル・スケーラー・pivot_columns のロード
try:
    model = joblib.load(os.path.join(MODEL_DIR, 'classifier_model.joblib'))
    scaler = joblib.load(os.path.join(MODEL_DIR, 'scaler.joblib'))
    pivot_columns = joblib.load(os.path.join(MODEL_DIR, 'pivot_columns.joblib'))
except Exception as e:
    raise RuntimeError(f"Failed loading model artifacts: {e}")

@app.get("/")
async def healthcheck():
    return {"status": "running"}

@app.post("/predict")
async def predict_percentage(file: UploadFile = File(...)):
    if not file.filename.endswith('.csv'):
        raise HTTPException(status_code=400, detail="CSVファイルをアップロードしてください。")

    try:
        df = pd.read_csv(pd.io.common.BytesIO(await file.read()), header=None, names=['timestamp','identifier','rssi'])
        pivot_df = df.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first').fillna(-100)
        pivot_df = pivot_df.reindex(columns=pivot_columns, fill_value=-100)

        X = scaler.transform(pivot_df.values)
        proba = model.predict_proba(X)

        # positive-class index
        class_index = list(model.classes_).index(POSITIVE_CLASS)
        avg_proba = float(np.mean(proba[:, class_index]) * 100)

        return JSONResponse(content={"predicted_percentage": int(round(avg_proba))})

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Prediction error: {e}")

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8101)
