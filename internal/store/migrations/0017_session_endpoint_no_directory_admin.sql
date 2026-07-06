-- R20: separate sessionHelper-reported isolation from M9 admin override.

ALTER TABLE session_endpoint ADD COLUMN no_directory_admin INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_endpoint ADD COLUMN no_directory_reported INTEGER NOT NULL DEFAULT 0;
UPDATE session_endpoint SET no_directory_reported=no_directory WHERE no_directory=1;
