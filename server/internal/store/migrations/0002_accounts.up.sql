ALTER TABLE users ALTER COLUMN apple_sub DROP NOT NULL;

CREATE UNIQUE INDEX users_display_name_unique
    ON users (lower(display_name))
    WHERE display_name <> '';

CREATE TABLE devices (
    id text PRIMARY KEY,
    user_id uuid NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    platform text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT devices_platform CHECK (platform IN ('ios', 'web')),
    CONSTRAINT devices_id_form CHECK (id ~ '^[A-Za-z0-9_-]{16,64}$')
);

ALTER TABLE events ADD COLUMN user_id uuid REFERENCES users (id);
ALTER TABLE events DROP CONSTRAINT events_kind;
ALTER TABLE events ADD CONSTRAINT events_kind CHECK (kind IN ('open', 'view', 'save', 'download', 'remove'));

CREATE TABLE holdings (
    share_id bytea NOT NULL REFERENCES shares (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id),
    device_id text NOT NULL,
    client text NOT NULL,
    saved_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (share_id, user_id),
    CONSTRAINT holdings_client CHECK (client IN ('web', 'ios'))
);
