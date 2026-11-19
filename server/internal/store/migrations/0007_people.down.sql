DROP TABLE activity_reads;
DROP TABLE photo_comments;
ALTER TABLE users DROP CONSTRAINT users_recovery_hash;
ALTER TABLE users DROP COLUMN recovery_hash;
ALTER TABLE devices ADD CONSTRAINT devices_user_id_key UNIQUE (user_id);
ALTER TABLE events DROP CONSTRAINT events_kind;
ALTER TABLE events ADD CONSTRAINT events_kind CHECK (kind IN ('open', 'view', 'save', 'download', 'remove'));
