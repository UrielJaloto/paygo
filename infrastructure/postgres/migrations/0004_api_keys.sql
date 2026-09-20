CREATE TABLE api_keys (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    prefix char(8) NOT NULL UNIQUE,
    hash char(64) NOT NULL,
    created_at timestamptz NOT NULL,
    revoked_at timestamptz NULL
);
