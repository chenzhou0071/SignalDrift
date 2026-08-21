-- schema.sql — 建表脚本：user/user_profile/user_friend/game_match_history（utf8mb4，幂等执行）
CREATE DATABASE IF NOT EXISTS signaldrift DEFAULT CHARACTER SET utf8mb4;
USE signaldrift;

CREATE TABLE IF NOT EXISTS `user` (
  uid BIGINT PRIMARY KEY AUTO_INCREMENT,
  username VARCHAR(64) NOT NULL UNIQUE,
  nickname VARCHAR(64) NOT NULL DEFAULT '',
  password_hash VARCHAR(128) NOT NULL,
  create_time DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS `user_profile` (
  uid BIGINT PRIMARY KEY,
  elo_score INT NOT NULL DEFAULT 1000,
  max_elo INT NOT NULL DEFAULT 1000,
  wins INT NOT NULL DEFAULT 0,
  losses INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS `user_friend` (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  uid BIGINT NOT NULL,
  friend_uid BIGINT NOT NULL,
  add_time DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_pair(uid, friend_uid)
);

CREATE TABLE IF NOT EXISTS `game_match_history` (
  match_id BIGINT PRIMARY KEY AUTO_INCREMENT,
  uid BIGINT NOT NULL,
  result TINYINT COMMENT '1胜 0平 -1负',
  elo_change INT,
  final_coverage FLOAT,
  painted_cells INT,
  straight_shots INT,
  lob_shots INT,
  hits_on_enemy INT,
  blackhole_destroyed INT,
  reflect_cnt INT,
  match_duration INT,
  start_time DATETIME,
  end_time DATETIME DEFAULT CURRENT_TIMESTAMP,
  KEY idx_uid(uid)
);
