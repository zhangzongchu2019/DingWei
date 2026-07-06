-- R8: global system services and keyword routes.
CREATE TABLE IF NOT EXISTS system_service (
  name        TEXT PRIMARY KEY,
  description TEXT,
  delivery    TEXT NOT NULL DEFAULT 'internal',
  endpoint    TEXT,
  active      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS system_route (
  keyword      TEXT PRIMARY KEY,
  service_name TEXT NOT NULL,
  priority     INTEGER NOT NULL DEFAULT 10,
  active       INTEGER NOT NULL DEFAULT 1
);

INSERT OR IGNORE INTO system_service(name, description, delivery, endpoint, active)
VALUES('scheduler', '系统级 deepseek 调度器', 'internal', '', 1);

INSERT OR IGNORE INTO system_route(keyword, service_name, priority, active)
VALUES('sys:调度', 'scheduler', 10, 1);
