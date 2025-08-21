import os
import logging
import numpy as np
import pandas as pd
from sklearn.model_selection import GridSearchCV
from sklearn.preprocessing import StandardScaler
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import (
    accuracy_score, precision_recall_fscore_support,
    roc_auc_score, average_precision_score
)

# ====== 設定 ======
# 学習（FINGERPRINT_DIR）で使うラベル
ROOM_IDS_TRAIN = ['513', '0']       # 正例=513, 負例=0
POS_LABEL = ROOM_IDS_TRAIN[0]
NEG_LABEL = ROOM_IDS_TRAIN[1]

# 評価（judgement）で使うラベル
JUDGE_POS = '513'
JUDGE_NEG_A = '0'
JUDGE_NEG_B = '514'

# --- 学習抽出数（timestamp単位） ---
TRAIN_POS = 80     # 正例
TRAIN_NEG = 120    # 負例（←要望どおり120に変更）

# --- 評価抽出数（judgement） ---
EVAL_POS = 125
EVAL_NEG_TOTAL = 125
EVAL_NEG_A = EVAL_NEG_TOTAL // 2         # 0 から 62
EVAL_NEG_B = EVAL_NEG_TOTAL - EVAL_NEG_A # 514 から 63

RANDOM_STATE = 42

# ログはコンソールのみ
logging.basicConfig(level=logging.INFO, format='%(asctime)s [%(levelname)s] %(message)s')

# ====== 共通ユーティリティ ======
def read_fp_dir(base_dir: str, allowed_labels: list[str]) -> pd.DataFrame:
    frames = []
    for lbl in allowed_labels:
        d = os.path.join(base_dir, lbl)
        if not os.path.isdir(d):
            logging.warning(f"Dir not found (skip): {d}")
            continue
        for fn in os.listdir(d):
            if fn.endswith('.csv') and (fn.startswith('ble') or fn.startswith('wifi')):
                fp = os.path.join(d, fn)
                df = pd.read_csv(fp, header=None, names=['timestamp', 'identifier', 'rssi'])
                df['label'] = lbl
                frames.append(df)
    if not frames:
        return pd.DataFrame(columns=['timestamp','identifier','rssi','label'])
    return pd.concat(frames, ignore_index=True)

def make_pivot(df: pd.DataFrame) -> tuple[pd.DataFrame, pd.DataFrame]:
    label_df = df[['timestamp','label']].drop_duplicates(subset='timestamp')
    pivot = df.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
    pivot = pivot.fillna(-100).loc[label_df['timestamp'].values]
    return pivot, label_df

def sample_timestamps(label_df: pd.DataFrame, label: str, n: int, rng: np.random.RandomState) -> list[int]:
    ts_list = label_df[label_df['label'].astype(str) == str(label)]['timestamp'].unique().tolist()
    if len(ts_list) < n:
        logging.warning(f"[{label}] requested {n}, available {len(ts_list)} -> use all available.")
        n = len(ts_list)
    return rng.choice(ts_list, size=n, replace=False).tolist()

# ====== 学習（FINGERPRINT_DIR） ======
def prepare_train(fingerprint_dir: str):
    df = read_fp_dir(fingerprint_dir, ROOM_IDS_TRAIN)
    if df.empty:
        raise RuntimeError("No training data found.")
    pivot, lab = make_pivot(df)

    rng = np.random.RandomState(RANDOM_STATE)
    # ← ここを変更：正例80 / 負例120
    pos_ts = sample_timestamps(lab, POS_LABEL, TRAIN_POS, rng)
    neg_ts = sample_timestamps(lab, NEG_LABEL, TRAIN_NEG, rng)
    train_ts_set = set(pos_ts) | set(neg_ts)

    is_train = lab['timestamp'].isin(train_ts_set).values
    X_train = pivot.values[is_train]
    y_train = lab['label'].values[is_train]
    train_cols = pivot.columns

    logging.info(f"[TRAIN] POS={np.sum(y_train==POS_LABEL)}, NEG={np.sum(y_train==NEG_LABEL)}, TOTAL={len(y_train)}")
    return X_train, y_train, train_cols

def train_model(X_train: np.ndarray, y_train: np.ndarray):
    scaler = StandardScaler()
    Xs = scaler.fit_transform(X_train)
    base = RandomForestClassifier(random_state=RANDOM_STATE)
    grid = GridSearchCV(base, {'n_estimators':[100,200], 'max_depth':[None,10,20]},
                        cv=5, scoring='accuracy', n_jobs=-1, refit=True)
    grid.fit(Xs, y_train)
    clf = grid.best_estimator_
    logging.info(f"[TRAIN] best_params={grid.best_params_}")
    return clf, scaler

# ====== 評価（judgement） ======
def prepare_eval_from_judgement(judgement_dir: str, train_columns: pd.Index):
    rng = np.random.RandomState(RANDOM_STATE + 1)

    parts = []
    for lbl, need_n in [(JUDGE_POS, EVAL_POS), (JUDGE_NEG_A, EVAL_NEG_A), (JUDGE_NEG_B, EVAL_NEG_B)]:
        df_lbl = read_fp_dir(judgement_dir, [lbl])
        if df_lbl.empty:
            logging.warning(f"[EVAL] No data for label {lbl} in judgement.")
            continue
        pivot_lbl, lab_lbl = make_pivot(df_lbl)
        pick_ts = sample_timestamps(lab_lbl, lbl, need_n, rng)
        sel = lab_lbl['timestamp'].isin(pick_ts).values
        pivot_lbl = pivot_lbl.reindex(columns=train_columns, fill_value=-100)
        X_part = pivot_lbl.values[sel]
        y_part = lab_lbl['label'].values[sel]
        parts.append((X_part, y_part))

    if not parts:
        raise RuntimeError("No evaluation data prepared from judgement.")

    X_eval = np.vstack([p[0] for p in parts])
    y_eval = np.concatenate([p[1] for p in parts])
    logging.info(f"[EVAL] counts: 513={np.sum(y_eval==JUDGE_POS)}, 0={np.sum(y_eval==JUDGE_NEG_A)}, 514={np.sum(y_eval==JUDGE_NEG_B)}, TOTAL={len(y_eval)}")
    return X_eval, y_eval

def evaluate(clf, scaler, X_eval, y_eval):
    Xs = scaler.transform(X_eval)
    y_pred = clf.predict(Xs)

    y_proba = None
    try:
        y_proba = clf.predict_proba(Xs)
    except Exception:
        pass

    acc = accuracy_score(y_eval, y_pred)
    prec_w, rec_w, f1_w, _ = precision_recall_fscore_support(y_eval, y_pred, average='weighted', zero_division=0)
    prec_m, rec_m, f1_m, _ = precision_recall_fscore_support(y_eval, y_pred, average='macro', zero_division=0)

    roc_auc = None
    pr_auc = None
    classes_ = getattr(clf, "classes_", None)
    if (y_proba is not None) and (classes_ is not None) and (len(classes_) == 2):
        pos_label = POS_LABEL if POS_LABEL in classes_ else classes_[1]
        pos_idx = list(classes_).index(pos_label)
        y_true_bin = (y_eval == pos_label).astype(int)
        try:
            roc_auc = roc_auc_score(y_true_bin, y_proba[:, pos_idx])
        except Exception:
            roc_auc = None
        try:
            pr_auc = average_precision_score(y_true_bin, y_proba[:, pos_idx])
        except Exception:
            pr_auc = None

    metrics_summary = [
        ('accuracy', acc),
        ('precision_macro', prec_m),
        ('recall_macro', rec_m),
        ('f1_macro', f1_m),
        ('precision_weighted', prec_w),
        ('recall_weighted', rec_w),
        ('f1_weighted', f1_w),
        ('roc_auc(binary_only)', '' if roc_auc is None else roc_auc),
        ('pr_auc(binary_only)', '' if pr_auc is None else pr_auc),
        ('best_params', str({k:getattr(clf,k) for k in []})),  # 形式合わせ（空でOK）
    ]
    logging.info(f"[EVAL] Accuracy={acc:.4f} | F1-macro={f1_m:.4f} | F1-weighted={f1_w:.4f}")
    return metrics_summary

def main():
    fingerprint_dir = os.getenv('FINGERPRINT_DIR', '/app/manager_fingerprint')
    judgement_dir = os.getenv('JUDGEMENT_DIR', 'judgement')
    model_dir = 'model'
    os.makedirs(model_dir, exist_ok=True)

    X_train, y_train, train_cols = prepare_train(fingerprint_dir)
    clf, scaler = train_model(X_train, y_train)

    X_eval, y_eval = prepare_eval_from_judgement(judgement_dir, train_cols)
    metrics_summary = evaluate(clf, scaler, X_eval, y_eval)

    pd.DataFrame(metrics_summary, columns=['metric','value']) \
      .to_csv(os.path.join(model_dir, 'metrics_summary.csv'), index=False)

if __name__ == '__main__':
    main()
