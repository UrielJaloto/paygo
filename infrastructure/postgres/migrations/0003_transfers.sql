CREATE TABLE transfers (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    fingerprint char(64) NOT NULL,
    kind text NOT NULL,
    from_account_id uuid NOT NULL REFERENCES accounts,
    to_account_id uuid NOT NULL REFERENCES accounts,
    amount_minor bigint NOT NULL,
    currency char(3) NOT NULL,
    status text NOT NULL,
    reversal_of uuid NULL REFERENCES transfers,
    created_at timestamptz NOT NULL,
    UNIQUE (owner_id, idempotency_key)
);

CREATE INDEX transfers_from_account_id_id_idx ON transfers (from_account_id, id DESC);

CREATE INDEX transfers_to_account_id_id_idx ON transfers (to_account_id, id DESC);
