-- registrations: 各組織のサーバ URL とプロキシ URL を登録
CREATE TABLE IF NOT EXISTS registrations (
    id SERIAL PRIMARY KEY,
    management_server_url TEXT NOT NULL,
    proxy_server_url    TEXT NOT NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT NOW()
);

-- room_mappings: registrations に紐づくフロア・部屋情報
CREATE TABLE IF NOT EXISTS room_mappings (
    id              SERIAL PRIMARY KEY,
    registration_id INT NOT NULL REFERENCES registrations(id) ON DELETE CASCADE,
    floor           VARCHAR(50)  NOT NULL,
    room_id         VARCHAR(50)  NOT NULL,
    room_name       VARCHAR(100) NOT NULL
);


CREATE INDEX IF NOT EXISTS idx_room_mappings_registration_id
    ON room_mappings(registration_id);

CREATE INDEX IF NOT EXISTS idx_room_mappings_floor
    ON room_mappings(floor);

CREATE INDEX IF NOT EXISTS idx_room_mappings_room_id
    ON room_mappings(room_id);
