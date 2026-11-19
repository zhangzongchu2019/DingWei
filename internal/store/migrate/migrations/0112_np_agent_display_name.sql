-- External Agent presentation only. Canonical name/address columns are unchanged.
ALTER TABLE np_agent ADD COLUMN display_name text;

-- Existing principal records may contain the Feishu display value available at
-- their original verified sign-in. Keep missing values NULL so clients fall
-- back to the canonical address.
UPDATE np_agent a
   SET display_name = CASE
       WHEN octet_length(p.display_name) <= 320
       THEN NULLIF(btrim(p.display_name), '')
       ELSE NULL
   END
  FROM np_extseat s
  JOIN np_external_principal p ON p.id = s.principal_id
 WHERE a.id = s.agent_id
   AND a.origin = 'external';
