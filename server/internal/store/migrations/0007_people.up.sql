ALTER TABLE events DROP CONSTRAINT events_kind;
ALTER TABLE events ADD CONSTRAINT events_kind CHECK (kind IN ('open', 'view', 'save', 'download', 'remove', 'append'));

ALTER TABLE devices DROP CONSTRAINT devices_user_id_key;

ALTER TABLE users ADD COLUMN recovery_hash bytea;
ALTER TABLE users ADD CONSTRAINT users_recovery_hash CHECK (recovery_hash IS NULL OR octet_length(recovery_hash) = 32);

CREATE TABLE photo_comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    photo_id text NOT NULL REFERENCES photo_cards (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT photo_comments_body CHECK (char_length(body) BETWEEN 1 AND 200)
);

CREATE INDEX photo_comments_photo ON photo_comments (photo_id, created_at);

CREATE TABLE activity_reads (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    activity_id text NOT NULL,
    read_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, activity_id),
    CONSTRAINT activity_reads_id_len CHECK (char_length(activity_id) BETWEEN 1 AND 200)
);
