CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    apple_sub text NOT NULL UNIQUE,
    display_name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE shares (
    id bytea PRIMARY KEY,
    owner_id uuid NOT NULL REFERENCES users (id),
    access_tag bytea NOT NULL,
    manifest_object_id bytea,
    manifest_nonce bytea,
    photo_count integer NOT NULL DEFAULT 0,
    total_bytes bigint NOT NULL DEFAULT 0,
    expires_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT shares_id_len CHECK (octet_length(id) = 16),
    CONSTRAINT shares_tag_len CHECK (octet_length(access_tag) = 32),
    CONSTRAINT shares_manifest_len CHECK (manifest_object_id IS NULL OR octet_length(manifest_object_id) = 16),
    CONSTRAINT shares_nonce_len CHECK (manifest_nonce IS NULL OR octet_length(manifest_nonce) = 12)
);

CREATE TABLE objects (
    id bytea PRIMARY KEY,
    share_id bytea NOT NULL REFERENCES shares (id) ON DELETE CASCADE,
    storage_key text NOT NULL UNIQUE,
    byte_size bigint,
    plaintext_bytes bigint NOT NULL,
    kind text NOT NULL,
    status text NOT NULL,
    upload_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT objects_id_len CHECK (octet_length(id) = 16),
    CONSTRAINT objects_kind CHECK (kind IN ('manifest', 'thumb', 'original')),
    CONSTRAINT objects_status CHECK (status IN ('pending', 'ready'))
);

CREATE TABLE events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    share_id bytea NOT NULL REFERENCES shares (id) ON DELETE CASCADE,
    kind text NOT NULL,
    client text NOT NULL,
    display_name text,
    device_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT events_kind CHECK (kind IN ('open', 'save', 'remove')),
    CONSTRAINT events_client CHECK (client IN ('web', 'ios'))
);

CREATE INDEX shares_owner_idx ON shares (owner_id, created_at DESC);
CREATE INDEX objects_share_idx ON objects (share_id);
CREATE INDEX events_share_idx ON events (share_id, created_at);
