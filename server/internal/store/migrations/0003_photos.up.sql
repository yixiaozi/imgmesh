ALTER TABLE users ADD COLUMN bio text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN city text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN contact text NOT NULL DEFAULT '';

CREATE TABLE photo_cards (
    id text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT photo_cards_id_form CHECK (id ~ '^[A-Za-z0-9_-]{43}$')
);

CREATE TABLE photo_people (
    photo_id text NOT NULL REFERENCES photo_cards (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    shared boolean NOT NULL DEFAULT false,
    saved boolean NOT NULL DEFAULT false,
    from_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    share_id bytea,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, user_id)
);

CREATE INDEX photo_people_user ON photo_people (user_id);
