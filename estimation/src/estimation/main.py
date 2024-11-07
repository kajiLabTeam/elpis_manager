import pandas as pd
import numpy as np
import os
from sklearn.model_selection import train_test_split, GridSearchCV
from sklearn.preprocessing import StandardScaler
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import classification_report, confusion_matrix, accuracy_score
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

    data['timestamp'] = data['timestamp'].astype(int)

    pivot_df = data.pivot_table(index='timestamp', columns='identifier', values='rssi', aggfunc='first')
    pivot_df.fillna(-100, inplace=True)

    label_df = data[['timestamp', 'label']].drop_duplicates(subset='timestamp')
    y = label_df['label'].values
    pivot_df = pivot_df.loc[label_df['timestamp'].values]

    X = pivot_df.values

    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=0.2, random_state=42, stratify=y
    )

    scaler = StandardScaler()
    X_train_scaled = scaler.fit_transform(X_train)
    X_test_scaled = scaler.transform(X_test)

    warnings.filterwarnings("ignore", category=ConvergenceWarning)

    model = LogisticRegression(random_state=42, solver='liblinear', max_iter=5000)
    model.fit(X_train_scaled, y_train)

    y_pred = model.predict(X_test_scaled)

    print("\n混同行列:")
    print(confusion_matrix(y_test, y_pred))

    print("\n分類レポート:")
    print(classification_report(y_test, y_pred))

    print("正解率:", accuracy_score(y_test, y_pred))

    param_grid = {
        'C': [0.01, 0.1, 1, 10, 100],
        'penalty': ['l2']
    }

    grid = GridSearchCV(LogisticRegression(random_state=42, solver='liblinear', max_iter=5000),
                        param_grid, refit=True, cv=5, scoring='f1')
    grid.fit(X_train_scaled, y_train)

    print("\n最適なハイパーパラメータ:")
    print(grid.best_params_)

    y_pred_grid = grid.predict(X_test_scaled)

    print("\nハイパーパラメータチューニング後の分類レポート:")
    print(classification_report(y_test, y_pred_grid))

    print("正解率:", accuracy_score(y_test, y_pred_grid))

    cm = confusion_matrix(y_test, y_pred_grid)

    try:
        font_path = '/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc'
        font_prop = fm.FontProperties(fname=font_path)
        plt.rcParams['font.family'] = font_prop.get_name()
    except FileNotFoundError:
        print("指定したフォントが見つかりません。日本語が正しく表示されない可能性があります。")

    plt.figure(figsize=(6,4))
    sns.heatmap(cm, annot=True, fmt='d', cmap='Blues',
                xticklabels=['Negative', 'Positive'],
                yticklabels=['Negative', 'Positive'])
    plt.xlabel('予測値')
    plt.ylabel('実測値')
    plt.title('混同行列')
    plt.show()

if __name__ == "__main__":
    main()
