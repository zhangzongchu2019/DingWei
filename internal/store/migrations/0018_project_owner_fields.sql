-- R21: project owner accountability fields (spec v0.23 §3.5.10).

ALTER TABLE project ADD COLUMN owner_key TEXT;
ALTER TABLE project ADD COLUMN product_manager_key TEXT;
