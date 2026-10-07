-- Generated from original receiver sqlite_master; execute only inside audited migrate-v1.
CREATE TABLE ack_rebases (request TEXT PRIMARY KEY, previous_request TEXT NOT NULL UNIQUE, intent TEXT NOT NULL, result TEXT, original_receipt TEXT, reserve BLOB NOT NULL) STRICT;
CREATE UNIQUE INDEX ack_rebases_pending ON ack_rebases((1)) WHERE result IS NULL AND original_receipt IS NULL;
