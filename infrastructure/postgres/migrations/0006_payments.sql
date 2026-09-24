ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check
    CHECK (kind IN ('customer', 'settlement', 'provider_clearing', 'payout_hold'));

INSERT INTO accounts (id, owner_id, currency, kind, status, version, created_at) VALUES
    ('01900000-0000-7000-8000-000000000011', '00000000-0000-7000-8000-000000000000', 'BRL', 'provider_clearing', 'active', 1, now()),
    ('01900000-0000-7000-8000-000000000021', '00000000-0000-7000-8000-000000000000', 'BRL', 'payout_hold', 'active', 1, now());

CREATE TABLE charges (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    fingerprint char(64) NOT NULL,
    account_id uuid NOT NULL REFERENCES accounts,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL,
    payer_document text NOT NULL,
    payer_name text NOT NULL,
    provider text NOT NULL,
    provider_id text NULL UNIQUE,
    pix_copy_paste text NOT NULL DEFAULT '',
    pix_qr_base64 text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('pending', 'paid', 'expired', 'failed')),
    transfer_id uuid NULL REFERENCES transfers,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (owner_id, idempotency_key)
);

CREATE INDEX charges_pending_created_at_idx ON charges (created_at) WHERE status = 'pending';

CREATE TABLE payouts (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    fingerprint char(64) NOT NULL,
    account_id uuid NOT NULL REFERENCES accounts,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL,
    pix_key text NOT NULL,
    pix_key_type text NOT NULL,
    provider text NOT NULL,
    provider_id text NULL UNIQUE,
    hold_transfer_id uuid NOT NULL REFERENCES transfers,
    final_transfer_id uuid NULL REFERENCES transfers,
    status text NOT NULL CHECK (status IN ('pending', 'completed', 'failed')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (owner_id, idempotency_key)
);

CREATE INDEX payouts_pending_created_at_idx ON payouts (created_at) WHERE status = 'pending';

CREATE TABLE provider_events (
    provider text NOT NULL,
    event_id text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, event_id)
);

CREATE TABLE provider_customers (
    provider text NOT NULL,
    document text NOT NULL,
    provider_customer_id text NOT NULL,
    PRIMARY KEY (provider, document)
);
