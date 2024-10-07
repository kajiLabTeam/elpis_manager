import pandas as pd
import numpy as np
from sklearn.model_selection import train_test_split, GridSearchCV
from sklearn.preprocessing import StandardScaler
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import classification_report, confusion_matrix, accuracy_score
import matplotlib.pyplot as plt
import seaborn as sns

def main():
    # 1. データの準備と前処理
    data = [
        [1728289304401, '722eb21f-8f6a-4ba9-a12f-05c0f970a177', -52],
        [1728289305506, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -74],
        [1728289305506, '8ebc2114-4abd-ba0d-b7c6-ff0f00200031', -70],
        [1728289305506, '722eb21f-8f6a-4ba9-a12f-05c0f970a177', -54],
        [1728289306611, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -60],
        [1728289311036, '8ebc2114-4abd-ba0d-b7c6-ff0f00200012', -79],
        [1728289312142, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -76],
        [1728289312142, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -73],
        [1728289312142, 'd546df97-4757-47ef-be09-3e2dcbdd0c77', -63],
        [1728289314356, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -78],
        [1728289317680, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -74],
        [1728289317680, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -66],
        [1728289317680, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -84],
        [1728289318792, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -60],
        [1728289319891, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -60],
        [1728289320997, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -81],
        [1728289320997, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -84],
        [1728289322106, '8ebc2114-4abd-ba0d-b7c6-ff0f00200004', -66],
        [1728289324318, '722eb21f-8f6a-4ba9-a12f-05c0f970a177', -57],
        [1728289325425, '4e24ac47-b7e6-44f5-957f-1cdcefa2acab', -80],
        [1728289325425, '722eb21f-8f6a-4ba9-a12f-05c0f970a177', -60],
        [1728289326539, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -76],
        [1728289326539, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -66],
        [1728289328761, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -57],
        [1728289329869, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -77],
        [1728289329869, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -78],
        [1728289330993, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -73],
        [1728289330993, 'd546df97-4757-47ef-be09-3e2dcbdd0c77', -65],
        [1728289332083, '8ebc2114-4abd-ba0d-b7c6-ff0f00200004', -63],
        [1728289332083, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -77],
        [1728289332083, 'd546df97-4757-47ef-be09-3e2dcbdd0c77', -62],
        [1728289333189, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -89],
        [1728289333189, '4e24ac47-b7e6-44f5-957f-1cdcefa2acab', -79],
        [1728289334302, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001b', -76],
        [1728289334302, '722eb21f-8f6a-4ba9-a12f-05c0f970a177', -74],
        [1728289337641, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001b', -75],
        [1728289338738, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -81],
        [1728289338738, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -81],
        [1728289340956, '8ebc2114-4abd-ba0d-b7c6-ff0f00200012', -84],
        [1728289340956, 'd546df97-4757-47ef-be09-3e2dcbdd0c77', -68],
        [1728289342067, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -73],
        [1728289344288, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -76],
        [1728289345398, '8ebc2114-4abd-ba0d-b7c6-ff0f00200052', -79],
        [1728289345398, '722eb21f-8f6a-4ba9-a12f-05c0f970a177', -57],
        [1728289347604, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -58],
        [1728289347604, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -71],
        [1728289348718, 'fda50693-a4e2-4fb1-afcf-c6eb07647825', -58],
        [1728289349829, 'd546df97-4757-47ef-be09-3e2dcbdd0c77', -58],
        [1728289350931, '8ebc2114-4abd-ba0d-b7c6-ff0f0020001f', -74],
        [1728289350931, 'd546df97-4757-47ef-be09-3e2dcbdd0c77', -62],
        [1728289352036, '8ebc2114-4abd-ba0d-b7c6-ff0f00200004', -62],
    ]
    # DataFrameの作成
    df = pd.DataFrame(data, columns=['timestamp', 'uuid', 'rssi'])

    print("データの先頭5行:")
    print(df.head())

    # 2. 特徴量の作成
    pivot_df = df.pivot_table(index='timestamp', columns='uuid', values='rssi', aggfunc='first')
    pivot_df.fillna(-100, inplace=True)

    print("\nピボットテーブルの形状:")
    print(pivot_df.shape)

    print("\nピボットテーブルの先頭5行:")
    print(pivot_df.head())

    # 3. ラベルの準備
    y = np.ones(len(pivot_df))
    num_negative = int(0.1 * len(y))
    np.random.seed(42)  # 再現性のため
    negative_indices = np.random.choice(len(y), size=num_negative, replace=False)
    y[negative_indices] = 0

    print("\nラベルの分布:")
    print(pd.Series(y).value_counts())

    # 4. データの分割とスケーリング
    X = pivot_df.values

    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=0.2, random_state=42, stratify=y
    )

    scaler = StandardScaler()
    X_train_scaled = scaler.fit_transform(X_train)
    X_test_scaled = scaler.transform(X_test)

    print("\n訓練データの形状:", X_train_scaled.shape)
    print("テストデータの形状:", X_test_scaled.shape)

    # 5. モデルの選定と学習
    model = LogisticRegression(random_state=42)
    model.fit(X_train_scaled, y_train)

    y_pred = model.predict(X_test_scaled)

    print("\n混同行列:")
    print(confusion_matrix(y_test, y_pred))

    print("\n分類レポート:")
    print(classification_report(y_test, y_pred))

    print("正解率:", accuracy_score(y_test, y_pred))

    # 6. ハイパーパラメータチューニング（オプション）
    param_grid = {
        'C': [0.01, 0.1, 1, 10, 100],
        'solver': ['liblinear', 'lbfgs'],
        'penalty': ['l2']
    }

    grid = GridSearchCV(LogisticRegression(random_state=42), param_grid, refit=True, cv=5, scoring='f1')
    grid.fit(X_train_scaled, y_train)

    print("\n最適なハイパーパラメータ:")
    print(grid.best_params_)

    y_pred_grid = grid.predict(X_test_scaled)

    print("\nハイパーパラメータチューニング後の分類レポート:")
    print(classification_report(y_test, y_pred_grid))

    print("正解率:", accuracy_score(y_test, y_pred_grid))

    # 7. 結果の可視化（オプション）
    cm = confusion_matrix(y_test, y_pred_grid)

    plt.figure(figsize=(6,4))
    sns.heatmap(cm, annot=True, fmt='d', cmap='Blues', xticklabels=['Negative', 'Positive'], yticklabels=['Negative', 'Positive'])
    plt.xlabel('Predicted')
    plt.ylabel('Actual')
    plt.title('Confusion Matrix')
    plt.show()

if __name__ == "__main__":
    main()
