import pandas as pd
import numpy as np
import os
from sklearn.model_selection import train_test_split, GridSearchCV
from sklearn.preprocessing import StandardScaler
from sklearn.ensemble import RandomForestRegressor
from sklearn.metrics import mean_absolute_error, mean_squared_error, r2_score
import matplotlib.pyplot as plt
import seaborn as sns
import matplotlib.font_manager as fm
import warnings
from sklearn.exceptions import ConvergenceWarning

def main():
    negative_dir = 'negative_samples'
    positive_dir = 'positive_samples'

    data_list = []

    for filename in os.listdir(negative_dir):
        if filename.endswith('.csv'):
            filepath = os.path.join(negative_dir, filename)
            if filename.startswith('ble') or filename.startswith('wifi'):
                df = pd.read_csv(filepath, header=None, names=['timestamp', 'identifier', 'rssi'])
                df['label'] = 0
                data_list.append(df)
                print(f"Loaded negative sample file: {filename}")

    for filename in os.listdir(positive_dir):
        if filename.endswith('.csv'):
            filepath = os.path.join(positive_dir, filename)
            if filename.startswith('ble') or filename.startswith('wifi'):
                df = pd.read_csv(filepath, header=None, names=['timestamp', 'identifier', 'rssi'])
                df['label'] = 1
                data_list.append(df)
                print(f"Loaded positive sample file: {filename}")

    if not data_list:
        print("データが読み込まれていません。ディレクトリ内のCSVファイルを確認してください。")
        return

    data = pd.concat(data_list, ignore_index=True)
    print(f"Total samples loaded: {len(data_list)}")
    print(f"Total data points: {data.shape[0]}")
    print(f"Number of negative samples: {(data['label'] == 0).sum()}")
    print(f"Number of positive samples: {(data['label'] == 1).sum()}")
    print("Data head:")
    print(data.head())
    print(f"Class distribution:\n{data['label'].value_counts()}")
    print(f"Unique identifiers: {data['identifier'].nunique()}")
    print("Sample data points:")
    print(data.sample(5))
    print(f"Data types:\n{data.dtypes}")
    print(f"Missing values:\n{data.isnull().sum()}")
    print(f"Unique timestamps: {data['timestamp'].nunique()}")

    # ラベルをパーセンテージにスケーリング（0と1を0と100に変換）
    data['percentage'] = data['label'] * 100

    # ラベルデータの作成（各タイムスタンプごとに一意のラベルを取得）
    label_df = data[['timestamp', 'percentage']].drop_duplicates(subset='timestamp')
    y = label_df['percentage'].values

    # ピボットテーブルの作成
    pivot_df = data.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
    pivot_df.fillna(-100, inplace=True)

    # Xとyのインデックスを揃える
    pivot_df = pivot_df.loc[label_df['timestamp'].values]

    X = pivot_df.values

    # データの分割とスケーリング
    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=0.2, random_state=42
    )

    scaler = StandardScaler()
    X_train_scaled = scaler.fit_transform(X_train)
    X_test_scaled = scaler.transform(X_test)

    warnings.filterwarnings("ignore", category=ConvergenceWarning)

    # ランダムフォレスト回帰モデルの訓練
    model = RandomForestRegressor(random_state=42)
    model.fit(X_train_scaled, y_train)

    # 予測
    y_pred = model.predict(X_test_scaled)

    # 評価指標の計算
    mae = mean_absolute_error(y_test, y_pred)
    mse = mean_squared_error(y_test, y_pred)
    r2 = r2_score(y_test, y_pred)

    print("\n回帰評価指標:")
    print(f"平均絶対誤差 (MAE): {mae:.2f}")
    print(f"平均二乗誤差 (MSE): {mse:.2f}")
    print(f"決定係数 (R²): {r2:.2f}")

    # ハイパーパラメータチューニング
    param_grid = {
        'n_estimators': [100, 200, 300],
        'max_depth': [None, 10, 20, 30],
        'min_samples_split': [2, 5, 10]
    }

    grid = GridSearchCV(RandomForestRegressor(random_state=42),
                        param_grid, refit=True, cv=5, scoring='neg_mean_absolute_error')
    grid.fit(X_train_scaled, y_train)

    print("\n最適なハイパーパラメータ:")
    print(grid.best_params_)

    # 最適モデルによる予測
    y_pred_grid = grid.predict(X_test_scaled)

    # 再評価指標の計算
    mae_grid = mean_absolute_error(y_test, y_pred_grid)
    mse_grid = mean_squared_error(y_test, y_pred_grid)
    r2_grid = r2_score(y_test, y_pred_grid)

    print("\nハイパーパラメータチューニング後の回帰評価指標:")
    print(f"平均絶対誤差 (MAE): {mae_grid:.2f}")
    print(f"平均二乗誤差 (MSE): {mse_grid:.2f}")
    print(f"決定係数 (R²): {r2_grid:.2f}")

    # 結果の可視化（実測値 vs 予測値）
    plt.figure(figsize=(8,6))
    sns.scatterplot(x=y_test, y=y_pred_grid)
    plt.plot([0, 100], [0, 100], color='red', linestyle='--')
    plt.xlabel('実測値 (%)')
    plt.ylabel('予測値 (%)')
    plt.title('実測値 vs 予測値')
    plt.show()

if __name__ == "__main__":
    main()
