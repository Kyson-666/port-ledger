package store

// schema 与设计稿 §7「数据模型」及 §4.2「字典数据结构」一致。
const schema = `
CREATE TABLE IF NOT EXISTS ports (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  port       INTEGER NOT NULL,
  protocol   TEXT    NOT NULL,
  name       TEXT    NOT NULL DEFAULT '',
  note       TEXT    NOT NULL DEFAULT '',
  tags       TEXT    NOT NULL DEFAULT '',
  project    TEXT    NOT NULL DEFAULT '',
  updated_by TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL,
  updated_at TEXT    NOT NULL,
  UNIQUE (port, protocol)
);

CREATE TABLE IF NOT EXISTS port_observations (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  scan_id        INTEGER NOT NULL,
  port           INTEGER NOT NULL,
  protocol       TEXT    NOT NULL,
  state          TEXT    NOT NULL,
  source         TEXT    NOT NULL,
  bind_addr      TEXT    NOT NULL DEFAULT '',
  pid            INTEGER NOT NULL DEFAULT 0,
  process_name   TEXT    NOT NULL DEFAULT '',
  cmdline        TEXT    NOT NULL DEFAULT '',
  container_id   TEXT    NOT NULL DEFAULT '',
  container_name TEXT    NOT NULL DEFAULT '',
  image          TEXT    NOT NULL DEFAULT '',
  compose_file   TEXT    NOT NULL DEFAULT '',
  observed_at    TEXT    NOT NULL,
  conflict       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_obs_scan ON port_observations(scan_id);
CREATE INDEX IF NOT EXISTS idx_obs_port ON port_observations(port, protocol);

CREATE TABLE IF NOT EXISTS reservations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  port       INTEGER NOT NULL,
  protocol   TEXT    NOT NULL,
  owner      TEXT    NOT NULL DEFAULT '',
  project    TEXT    NOT NULL DEFAULT '',
  reason     TEXT    NOT NULL DEFAULT '',
  expires_at TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS scan_runs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at    TEXT NOT NULL,
  finished_at   TEXT NOT NULL DEFAULT '',
  scope         TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL,
  counts        TEXT NOT NULL DEFAULT '',
  source_health TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS audit_log (
  id     INTEGER PRIMARY KEY AUTOINCREMENT,
  ts     TEXT NOT NULL,
  actor  TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS system_port_dict (
  port         INTEGER NOT NULL,
  port_end     INTEGER NOT NULL DEFAULT 0,
  protocol     TEXT    NOT NULL,
  category     TEXT    NOT NULL,
  display_name TEXT    NOT NULL,
  description  TEXT    NOT NULL DEFAULT '',
  source_ref   TEXT    NOT NULL DEFAULT '',
  builtin      INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (port, port_end, protocol, category)
);

CREATE TABLE IF NOT EXISTS dict_user (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  port         INTEGER NOT NULL,
  port_end     INTEGER NOT NULL DEFAULT 0,
  protocol     TEXT    NOT NULL,
  category     TEXT    NOT NULL,
  display_name TEXT    NOT NULL,
  description  TEXT    NOT NULL DEFAULT '',
  created_at   TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS dict_override (
  port         INTEGER NOT NULL,
  protocol     TEXT    NOT NULL,
  display_name TEXT    NOT NULL,
  description  TEXT    NOT NULL DEFAULT '',
  updated_at   TEXT    NOT NULL,
  PRIMARY KEY (port, protocol)
);
`
