import pandas as pd
import numpy as np
import os
from sklearn.model_selection import train_test_split, GridSearchCV
from sklearn.preprocessing import StandardScaler
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import classification_report, accuracy_score
import joblib
import logging
from fpdf import FPDF

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    handlers=[logging.StreamHandler(), logging.FileHandler("app.log")]
)

# 部屋ID定義
ROOM_IDS = ['103', '0']  # 正例:103、負例:0

def load_data(fingerprint_dir):
    data_list = []
    for room_id in os.listdir(fingerprint_dir):
        room_path = os.path.join(fingerprint_dir, room_id)
        if os.path.isdir(room_path) and room_id in ROOM_IDS:
            for filename in os.listdir(room_path):
                if filename.endswith('.csv') and (filename.startswith('ble') or filename.startswith('wifi')):
                    df = pd.read_csv(os.path.join(room_path, filename), header=None, names=['timestamp', 'identifier', 'rssi'])
                    df['label'] = room_id
                    data_list.append(df)

    if not data_list:
        logging.warning("No data loaded.")
        return None

    return pd.concat(data_list, ignore_index=True)

def preprocess_data(data):
    label_df = data[['timestamp', 'label']].drop_duplicates(subset='timestamp')
    y = label_df['label'].values

    pivot_df = data.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
    pivot_df.fillna(-100, inplace=True)

    pivot_df = pivot_df.loc[label_df['timestamp'].values]
    X = pivot_df.values

    return X, y, pivot_df

def train_model(X, y, model_dir):
    X_train, X_test, y_train, y_test = train_test_split(X, y, test_size=0.2, random_state=42)

    scaler = StandardScaler()
    X_train_scaled = scaler.fit_transform(X_train)
    X_test_scaled = scaler.transform(X_test)

    model = RandomForestClassifier(random_state=42)
    param_grid = {'n_estimators': [100, 200], 'max_depth': [None, 10, 20]}

    grid = GridSearchCV(model, param_grid, cv=5, scoring='accuracy', n_jobs=-1)
    grid.fit(X_train_scaled, y_train)

    y_pred = grid.predict(X_test_scaled)

    accuracy = accuracy_score(y_test, y_pred)
    report = classification_report(y_test, y_pred)

    logging.info(f"Accuracy: {accuracy:.2f}\n{report}")

    os.makedirs(model_dir, exist_ok=True)
    joblib.dump(grid.best_estimator_, os.path.join(model_dir, 'classifier_model.joblib'))
    joblib.dump(scaler, os.path.join(model_dir, 'scaler.joblib'))

    return grid, scaler

def predict_judgement(grid, scaler, judgement_dir, pivot_columns, model_dir):
    results = []

    for root, _, files in os.walk(judgement_dir):
        dir_relative = os.path.relpath(root, judgement_dir)
        if dir_relative == '.':
            dir_relative = judgement_dir
        
        for file in files:
            if file.endswith('.csv'):
                filepath = os.path.join(root, file)
                df = pd.read_csv(filepath, header=None, names=['timestamp', 'identifier', 'rssi'])

                pivot_df = df.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
                pivot_df.fillna(-100, inplace=True)
                pivot_df = pivot_df.reindex(columns=pivot_columns, fill_value=-100)
                X_judgement_scaled = scaler.transform(pivot_df.values)

                pred_proba = grid.predict_proba(X_judgement_scaled)
                avg_proba = np.mean(pred_proba, axis=0)

                result = {'directory': dir_relative, 'file': file}
                for idx, room_id in enumerate(grid.classes_):
                    result[f'room_{room_id}_prob'] = avg_proba[idx]

                results.append(result)

    results_df = pd.DataFrame(results)
    results_csv_path = os.path.join(model_dir, 'judgement_results.csv')
    results_df.to_csv(results_csv_path, index=False)

    generate_pdf_report(results_df, os.path.join(model_dir, 'judgement_results.pdf'))

    logging.info(results_df)

def generate_pdf_report(results_df, pdf_path):
    pdf = FPDF()
    pdf.add_page()
    pdf.set_font("Arial", size=10)

    pdf.cell(200, 10, txt="Judgement Results", ln=True, align='C')
    pdf.ln(10)

    col_width = pdf.w / (len(results_df.columns) + 1)

    # Header
    for column in results_df.columns:
        pdf.cell(col_width, 10, column, border=1)
    pdf.ln()

    # Rows
    for _, row in results_df.iterrows():
        for item in row:
            pdf.cell(col_width, 10, str(round(item, 4)) if isinstance(item, float) else str(item), border=1)
        pdf.ln()

    pdf.output(pdf_path)

def main():
    fingerprint_dir = os.getenv('FINGERPRINT_DIR', '/app/manager_fingerprint')
    judgement_dir = 'judgement'
    model_dir = 'model'

    data = load_data(fingerprint_dir)
    if data is None:
        return

    X, y, pivot_df = preprocess_data(data)
    grid, scaler = train_model(X, y, model_dir)

    pivot_columns = pivot_df.columns
    joblib.dump(pivot_columns.tolist(), os.path.join(model_dir, 'pivot_columns.joblib'))

    predict_judgement(grid, scaler, judgement_dir, pivot_columns, model_dir)

if __name__ == "__main__":
    main()
