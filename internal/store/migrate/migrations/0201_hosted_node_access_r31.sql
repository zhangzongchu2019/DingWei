ALTER TABLE g4_node ADD COLUMN address text NOT NULL DEFAULT '';
ALTER TABLE g4_node ADD COLUMN address_source text NOT NULL DEFAULT '' CHECK(address_source IN ('','admin','node'));
ALTER TABLE g4_node ADD COLUMN scheduling_state text NOT NULL DEFAULT 'enabled' CHECK(scheduling_state IN ('enabled','disabled','draining'));
ALTER TABLE g4_node ADD COLUMN access_scope text NOT NULL DEFAULT 'admin_only' CHECK(access_scope IN ('admin_only','public','restricted'));
CREATE TABLE g4_node_access_grant (
 node_id text NOT NULL REFERENCES g4_node(id) ON DELETE CASCADE,
 subject_kind text NOT NULL CHECK(subject_kind IN ('user','group')),
 subject_id text NOT NULL CHECK(subject_id<>''),
 PRIMARY KEY(node_id,subject_kind,subject_id)
);
