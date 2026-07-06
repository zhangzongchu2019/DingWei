-- R19: hide dedicated sessions from online directory broadcasts.

ALTER TABLE session_endpoint ADD COLUMN no_directory INTEGER NOT NULL DEFAULT 0;
