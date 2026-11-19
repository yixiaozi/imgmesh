CREATE TABLE photo_likes (
    photo_id text NOT NULL REFERENCES photo_cards (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_id, user_id)
);

CREATE TABLE friendships (
    user_low uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_high uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    requester_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('pending', 'accepted')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_low, user_high),
    CONSTRAINT friendships_order CHECK (user_low < user_high),
    CONSTRAINT friendships_requester CHECK (requester_id = user_low OR requester_id = user_high)
);

CREATE TABLE messages (
    id uuid PRIMARY KEY,
    sender_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    recipient_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_not_self CHECK (sender_id <> recipient_id)
);

CREATE INDEX messages_pair_time ON messages (sender_id, recipient_id, created_at);

CREATE TABLE message_reads (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    peer_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    read_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, peer_id)
);
