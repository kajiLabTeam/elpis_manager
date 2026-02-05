# Filename: app.py

import pandas as pd
import numpy as np
import joblib
from fastapi import FastAPI, File, UploadFile, HTTPException
from fastapi.responses import JSONResponse
import uvicorn
import os
from typing import Optional

app = FastAPI(title="RSSI Multi-Class Room Prediction API")

MODEL_DIR = 'model'

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

async def read_csv(upload: UploadFile) -> pd.DataFrame:
    if not upload.filename.endswith('.csv'):
        raise HTTPException(status_code=400, detail="CSVファイルをアップロードしてください。")
    return pd.read_csv(
        pd.io.common.BytesIO(await upload.read()),
        header=None,
        names=['timestamp', 'identifier', 'rssi'],
    )

@app.post("/predict")
async def predict_room(
    file: Optional[UploadFile] = File(None),
    wifi_data: Optional[UploadFile] = File(None),
    ble_data: Optional[UploadFile] = File(None),
):
    if file is None and (wifi_data is None or ble_data is None):
        raise HTTPException(status_code=400, detail="file もしくは wifi_data と ble_data を指定してください。")

    try:
        if file is not None:
            df = await read_csv(file)
        else:
            wifi_df = await read_csv(wifi_data)
            ble_df = await read_csv(ble_data)
            df = pd.concat([ble_df, wifi_df], ignore_index=True)

        pivot_df = df.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first').fillna(-100)
        pivot_df = pivot_df.reindex(columns=pivot_columns, fill_value=-100)

        X = scaler.transform(pivot_df.values)
        proba = model.predict_proba(X)

        avg_proba = np.mean(proba, axis=0)
        best_idx = int(np.argmax(avg_proba))
        best_room = str(model.classes_[best_idx])
        best_percentage = float(avg_proba[best_idx] * 100)

        return JSONResponse(content={
            "room_id": best_room,
            "percentage_processed": int(round(best_percentage)),
        })

    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Prediction error: {e}")

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8105)
