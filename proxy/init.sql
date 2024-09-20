-- 滞在管理システムテーブル
CREATE TABLE
    organizations (
        id INT AUTO_INCREMENT PRIMARY KEY, -- システムを一意に識別するID
        api_endpoint VARCHAR(512) NOT NULL, -- システムへのAPIエンドポイントURL
        port_number INT NOT NULL, -- APIアクセス用の認証キー
        last_updated DATETIME DEFAULT CURRENT_TIMESTAMP, -- 最終更新日時
    );
