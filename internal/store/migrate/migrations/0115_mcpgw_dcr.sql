-- S-MCP-DCR: resource-scoped OAuth public clients, separate from short grants.
-- Metadata is AES-GCM sealed; source/subject keys are domain-separated HMACs.
CREATE TABLE mcpgw_dcr_client (
 resource text NOT NULL,
 client_id text NOT NULL,
 sealed bytea NOT NULL,
 source_hash text NOT NULL,
 created_ns bigint NOT NULL,
 expires_ns bigint NOT NULL,
 bound boolean NOT NULL DEFAULT false,
 PRIMARY KEY(resource,client_id)
);
CREATE INDEX mcpgw_dcr_expiry ON mcpgw_dcr_client(resource,expires_ns);
CREATE INDEX mcpgw_dcr_source ON mcpgw_dcr_client(resource,source_hash,expires_ns);
CREATE TABLE mcpgw_dcr_binding (
 resource text NOT NULL,
 client_id text NOT NULL,
 principal_hash text NOT NULL,
 redeemed_ns bigint NOT NULL,
 expires_ns bigint NOT NULL,
 PRIMARY KEY(resource,client_id,principal_hash),
 FOREIGN KEY(resource,client_id) REFERENCES mcpgw_dcr_client(resource,client_id) ON DELETE CASCADE
);
CREATE INDEX mcpgw_dcr_subject ON mcpgw_dcr_binding(resource,principal_hash,expires_ns,redeemed_ns);
CREATE TABLE mcpgw_dcr_bucket (
 resource text NOT NULL,
 bucket text NOT NULL,
 tokens double precision NOT NULL,
 updated_ns bigint NOT NULL,
 PRIMARY KEY(resource,bucket)
);
CREATE INDEX mcpgw_dcr_bucket_age ON mcpgw_dcr_bucket(resource,updated_ns);
