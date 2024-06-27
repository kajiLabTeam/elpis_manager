CREATE TABLE Users (
	id SERIAL PRIMARY KEY,
	user_id VARCHAR(20) NOT NULL,
	password VARCHAR(20)
);

CREATE TABLE rooms (
    room_id SERIAL PRIMARY KEY,
    room_name VARCHAR(100) NOT NULL,
    location INT
);

CREATE TABLE beacons (
    beacon_id SERIAL PRIMARY KEY,
    beacon_name VARCHAR(100) NOT NULL,
    service_uuid CHAR(36),
    mac_address VARCHAR(17),
    room_id INT,
    FOREIGN KEY (room_id) REFERENCES rooms(room_id)
);

CREATE TABLE wifi_access_points (
    wifi_id SERIAL PRIMARY KEY,
    ssid VARCHAR(100) NOT NULL,
    bssid VARCHAR(17) NOT NULL,
    room_id INT,
    FOREIGN KEY (room_id) REFERENCES rooms(room_id)
);

CREATE TABLE roles (
    role_id SERIAL PRIMARY KEY,
    role_name VARCHAR(50) NOT NULL
);

CREATE TABLE user_roles (
    user_id INT,
    role_id INT,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (role_id) REFERENCES roles(role_id)
);

CREATE TABLE query_server (
    id SERIAL PRIMARY KEY,
    url VARCHAR(255) NOT NULL
);
