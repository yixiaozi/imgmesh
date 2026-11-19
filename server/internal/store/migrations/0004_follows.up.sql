CREATE TABLE follows (
    share_id bytea NOT NULL REFERENCES shares (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (share_id, user_id)
);
