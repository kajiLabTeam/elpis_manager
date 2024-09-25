CREATE TABLE
    Users (
        id SERIAL PRIMARY KEY,
        user_id VARCHAR(20) NOT NULL UNIQUE,
        password VARCHAR(20)
    );

CREATE TABLE
    rooms (
        room_id SERIAL PRIMARY KEY,
        room_name VARCHAR(100) NOT NULL,
        location INT
    );

CREATE TABLE
    beacons (
        beacon_id SERIAL PRIMARY KEY,
        beacon_name VARCHAR(100) NOT NULL,
        service_uuid CHAR(36),
        mac_address VARCHAR(17),
        rssi INT,
        room_id INT,
        FOREIGN KEY (room_id) REFERENCES rooms (room_id)
    );

CREATE TABLE
    wifi_access_points (
        wifi_id SERIAL PRIMARY KEY,
        ssid VARCHAR(100) NOT NULL,
        bssid VARCHAR(17) NOT NULL,
        rssi INT,
        room_id INT,
        FOREIGN KEY (room_id) REFERENCES rooms (room_id)
    );

CREATE TABLE
    roles (
        role_id SERIAL PRIMARY KEY,
        role_name VARCHAR(50) NOT NULL
    );

CREATE TABLE
    user_roles (
        user_id INT,
        role_id INT,
        PRIMARY KEY (user_id, role_id),
        FOREIGN KEY (user_id) REFERENCES users (id),
        FOREIGN KEY (role_id) REFERENCES roles (role_id)
    );

CREATE TABLE
    query_server (id SERIAL PRIMARY KEY, url VARCHAR(255) NOT NULL);

-- ユーザーの在室セッションを保存するテーブル
CREATE TABLE
    user_presence_sessions (
        session_id SERIAL PRIMARY KEY,
        user_id INT REFERENCES Users (id),
        room_id INT REFERENCES rooms (room_id),
        start_time TIMESTAMP NOT NULL,
        end_time TIMESTAMP,
        last_seen TIMESTAMP NOT NULL
    );

-- インデックスの追加
CREATE INDEX idx_user_presence_sessions_user_id ON user_presence_sessions (user_id);

CREATE INDEX idx_user_presence_sessions_end_time ON user_presence_sessions (end_time);

CREATE INDEX idx_user_presence_sessions_last_seen ON user_presence_sessions (last_seen);

-- ユーザーのデータを挿入
INSERT INTO
    Users (user_id, password)
VALUES
    ('user1', 'password1'),
    ('user2', 'password2'),
    ('user3', 'password3');

-- 部屋のデータを挿入
INSERT INTO
    rooms (room_name, location)
VALUES
    ('Graduate Students Room', 513),
    ('Undergraduate Students Room', 514),
    ('Professors Office', 515);

-- ビーコンデバイスのデータを挿入、RSSI値も指定
INSERT INTO
    beacons (
        beacon_name,
        service_uuid,
        mac_address,
        rssi,
        room_id
    )
VALUES
    (
        'elpis-001',
        'd546df97-4757-47ef-be09-3e2dcbdd0c77',
        'DC:0D:30:1E:33:91',
        -80,
        2
    ),
    (
        'elpis-002',
        'd546df97-4757-47ef-be09-3e2dcbdd0c77',
        'DC:0D:30:1E:33:84',
        -75,
        3
    ),
    (
        'elpis-003',
        'd546df97-4757-47ef-be09-3e2dcbdd0c77',
        'DC:0D:30:1E:33:3E',
        -75,
        3
    ),;

-- WiFiアクセスポイントのデータを挿入、RSSI値も指定
INSERT INTO
    wifi_access_points (ssid, bssid, rssi, room_id)
VALUES
    ('WiFi A', '66:77:88:99:AA:BB', -65, 1),
    ('WiFi B', '66:77:88:99:AA:BC', -70, 2),
    ('WiFi C', '66:77:88:99:AA:BD', -75, 3);

-- ロールのデータを挿入
INSERT INTO
    roles (role_name)
VALUES
    ('Admin'),
    ('User'),
    ('Guest');

-- ユーザーロールのデータを挿入
INSERT INTO
    user_roles (user_id, role_id)
VALUES
    (1, 1),
    (2, 2),
    (3, 3);
