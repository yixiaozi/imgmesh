DROP TABLE holdings;
ALTER TABLE events DROP CONSTRAINT events_kind;
ALTER TABLE events ADD CONSTRAINT events_kind CHECK (kind IN ('open', 'save', 'remove'));
ALTER TABLE events DROP COLUMN user_id;
DROP TABLE devices;
DROP INDEX users_display_name_unique;
ALTER TABLE users ALTER COLUMN apple_sub SET NOT NULL;
