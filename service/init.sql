-- 組織ごとの在室管理サーバ
CREATE TABLE IF NOT EXISTS registrations (
    id SERIAL PRIMARY KEY,
    management_server_url TEXT NOT NULL,
    proxy_server_url      TEXT NOT NULL,
    created_at            TIMESTAMP NOT NULL DEFAULT NOW()
);

-- 各登録システムのフロア・部屋対応表
CREATE TABLE IF NOT EXISTS room_mappings (
    id              SERIAL PRIMARY KEY,
    registration_id INT NOT NULL REFERENCES registrations(id) ON DELETE CASCADE,
    floor           VARCHAR(50)  NOT NULL,
    room_id         VARCHAR(50)  NOT NULL,
    room_name       VARCHAR(100) NOT NULL
);

-- 照会サーバ（Inquiry Server）
CREATE TABLE IF NOT EXISTS inquiry_partners (
    id                SERIAL PRIMARY KEY,
    inquiry_server_uri TEXT NOT NULL,
    port              INT  NOT NULL,
    latitude          DOUBLE PRECISION NOT NULL,
    longitude         DOUBLE PRECISION NOT NULL,
    description       TEXT,
    created_at        TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_room_mappings_registration_id
    ON room_mappings(registration_id);
