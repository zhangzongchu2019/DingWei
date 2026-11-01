-- Administrator-maintained identity mapping only; never grants repository access.
CREATE TABLE openspec_read_binding (
 principal TEXT NOT NULL REFERENCES console_principal(id),
 provider TEXT NOT NULL CHECK(provider='github'),
 instance TEXT NOT NULL CHECK(instance='github.com'),
 account_id TEXT NOT NULL,
 login TEXT NOT NULL,
 updated_by TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(principal,provider,instance)
);
CREATE TABLE openspec_read_binding_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 actor TEXT NOT NULL,
 principal TEXT NOT NULL,
 provider TEXT NOT NULL,
 instance TEXT NOT NULL,
 account_id TEXT NOT NULL,
 login TEXT NOT NULL,
 operation TEXT NOT NULL,
 created_at TEXT NOT NULL
);
