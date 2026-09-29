-- Remoto MySQL schema (GORM also auto-migrates; this is reference DDL)

CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  display_name VARCHAR(128) DEFAULT '',
  role VARCHAR(16) NOT NULL DEFAULT 'user',
  status TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  UNIQUE KEY uk_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS devices (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  device_uid VARCHAR(64) NOT NULL,
  name VARCHAR(128) DEFAULT '',
  os_version VARCHAR(64) DEFAULT '',
  agent_version VARCHAR(32) DEFAULT '',
  last_ip VARCHAR(45) DEFAULT '',
  last_seen_at DATETIME(3) NULL,
  status TINYINT NOT NULL DEFAULT 0,
  auth_token_hash VARCHAR(255) NOT NULL,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  UNIQUE KEY uk_devices_uid (device_uid),
  KEY idx_devices_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS bind_codes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  code VARCHAR(16) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  used TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME(3) NULL,
  UNIQUE KEY uk_bind_codes_code (code),
  KEY idx_bind_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS remote_sessions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  device_id BIGINT UNSIGNED NOT NULL,
  started_at DATETIME(3) NULL,
  ended_at DATETIME(3) NULL,
  quality VARCHAR(16) DEFAULT '',
  bytes_relayed BIGINT NOT NULL DEFAULT 0,
  end_reason VARCHAR(64) DEFAULT '',
  room_id VARCHAR(64) DEFAULT '',
  KEY idx_sess_user (user_id),
  KEY idx_sess_device (device_id),
  KEY idx_sess_room (room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  actor_type VARCHAR(16) DEFAULT '',
  actor_id BIGINT UNSIGNED NULL,
  action VARCHAR(64) DEFAULT '',
  resource_type VARCHAR(32) DEFAULT '',
  resource_id BIGINT UNSIGNED NULL,
  ip VARCHAR(45) DEFAULT '',
  user_agent VARCHAR(255) DEFAULT '',
  detail JSON NULL,
  created_at DATETIME(3) NULL,
  KEY idx_audit_action (action),
  KEY idx_audit_actor (actor_id),
  KEY idx_audit_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  token_hash VARCHAR(255) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NULL,
  UNIQUE KEY uk_refresh_hash (token_hash),
  KEY idx_refresh_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
