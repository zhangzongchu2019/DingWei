-- Additive only. Feature-off rollback retains registry, aliases and delivery audit.
CREATE TABLE sub_agent_registry (
 root_agent_id TEXT NOT NULL,
 node_id TEXT NOT NULL,
 parent_node_id TEXT NOT NULL DEFAULT '',
 generation INTEGER NOT NULL CHECK(generation>=1),
 canonical_name TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL CHECK(state IN ('allocating','active','draining','ended')),
 reason TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 0,
 digest TEXT NOT NULL DEFAULT '',
 conn_seq INTEGER NOT NULL,
 created_at TEXT NOT NULL DEFAULT '',
 policy_json TEXT NOT NULL DEFAULT '{}',
 PRIMARY KEY(root_agent_id,node_id)
);
CREATE TABLE sub_agent_alias (
 root_agent_id TEXT NOT NULL,
 parent_node_id TEXT NOT NULL DEFAULT '',
 alias TEXT NOT NULL,
 node_id TEXT NOT NULL,
 PRIMARY KEY(root_agent_id,parent_node_id,alias),
 FOREIGN KEY(root_agent_id,node_id) REFERENCES sub_agent_registry(root_agent_id,node_id)
);
CREATE TABLE session_delivery_sub_route (
 delivery_id TEXT PRIMARY KEY REFERENCES session_delivery(id),
 route_json TEXT NOT NULL
);
