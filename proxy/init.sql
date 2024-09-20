-- 滞在管理システムテーブル
CREATE TABLE
    organizations (
        id INT AUTO_INCREMENT PRIMARY KEY,
        api_endpoint VARCHAR(512) NOT NULL,
        port_number INT NOT NULL,
        last_updated DATETIME DEFAULT CURRENT_TIMESTAMP,
    );
