-- Shared authority: preserve the row/generation after release.
CREATE TABLE IF NOT EXISTS console_transport_slot (
 principal_id text NOT NULL,
 origin text NOT NULL,
 owner_session text NOT NULL,
 owner_instance text NOT NULL,
 nonce text NOT NULL,
 generation bigint NOT NULL CHECK(generation>0),
 expires_at timestamptz NOT NULL,
 PRIMARY KEY(principal_id,origin)
);
