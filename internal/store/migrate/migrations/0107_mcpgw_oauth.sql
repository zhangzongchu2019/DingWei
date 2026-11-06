-- R53 B 路，组长 2026-11-06T16:13:02Z 分配 0107。
-- resource 隔离授权服务器实例；payload 为 AES-256-GCM 密文，不存原始 state/code。
-- expires_ns 为 UTC Unix 纳秒。B 每次 Put 清除本 resource 的过期行。
CREATE TABLE mcpgw_oauth (
 resource text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('attempt','login','code','cstate')),
 key_hash text NOT NULL CHECK(length(key_hash)=64),
 sealed bytea NOT NULL,
 expires_ns bigint NOT NULL,
 PRIMARY KEY(resource,kind,key_hash)
);
CREATE INDEX mcpgw_oauth_expiry ON mcpgw_oauth(resource,expires_ns);
