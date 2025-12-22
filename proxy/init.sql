CREATE TABLE organizations (
    api_endpoint VARCHAR PRIMARY KEY,
    scheme VARCHAR,
    port_number INTEGER,
    last_updated TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 屋内測位サービス連携用の部屋ID登録テーブル
CREATE TABLE IF NOT EXISTS service_registrations (
    id SERIAL PRIMARY KEY,
    system_uri VARCHAR NOT NULL UNIQUE,
    port_number INTEGER NOT NULL,
    room_id VARCHAR NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
