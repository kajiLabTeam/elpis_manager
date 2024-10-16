# elpis_manager

## 必要条件

- Go 1.22以上
- Python 3.10以上
  - uv
- Node.js 22以上
- Docker / Docker Compose

## インストール

1. リポジトリをクローンします。

    ```sh
    git clone git@github.com:kajiLabTeam/elpis_proxy.git
    cd elpis_proxy
    ```

2. 必要な依存関係をインストールします。

    ```sh
    go mod download
    ```

## 使い方

### サーバーの起動方法

#### 方法1: Docker Composeで立ち上げる

Docker Composeを使用して、すべてのサービスをバックグラウンドで起動します。

```sh
make up
```

サービスの状態を確認するには:

```sh
docker compose ps
```

すべてのサービスを停止するには:

```sh
make down
```

#### 方法2: ローカル環境で立ち上げる

ローカル環境で各サービスを個別に起動します。

1. **データベースの起動**

    データベースサービスのみを起動します。

    ```sh
    make db-up
    ```

2. **プロキシサービスの起動**

    別のターミナルで、プロキシサービスをローカルで起動します。

    ```sh
    make proxy-local
    ```

3. **マネージャーサービスの起動**

    別のターミナルで、マネージャーサービスをローカルで起動します。

    ```sh
    make manager-local
    ```

4. **サービスの停止**

    関連するボリュームを削除します。

    ```sh
    make db-down
    ```

### その他のコマンド

- **サービスの再起動**

    全サービスを再起動するには:

    ```sh
    make restart
    ```

- **特定のサービスを再起動**

    マネージャーサービスのみを再起動:

    ```sh
    make restart-manager
    ```

    プロキシサービスのみを再起動:

    ```sh
    make restart-proxy
    ```

- **エンドツーエンドテストの実行**

    ```sh
    make e2e-test
    ```

## Python(uv)

推定サーバにはPythonを使った機械学習モデルを採用しています。下記リンクからuvを導入してください。
<https://zenn.dev/turing_motors/articles/594fbef42a36ee>

- **推定サービスのローカル起動**

    ```sh
    make est-local
    ```

## ヘルプの表示

利用可能なすべてのMakeコマンドを確認するには:

```sh
make help
```

## ライセンス

現状のElpisプロジェクトは、GPLv3ライセンスの下で公開されています。
ライセンスの詳細については、[LICENSE](LICENSE)ファイルを参照してください。
