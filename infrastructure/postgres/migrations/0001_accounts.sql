CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    currency char(3) NOT NULL CHECK (currency IN ('BRL', 'USD', 'EUR')),
    kind text NOT NULL CHECK (kind IN ('customer', 'settlement')),
    status text NOT NULL CHECK (status IN ('active', 'frozen')),
    balance_minor bigint NOT NULL DEFAULT 0,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX accounts_owner_id_id_idx ON accounts (owner_id, id);

INSERT INTO accounts (id, owner_id, currency, kind, status, version, created_at) VALUES
    ('01900000-0000-7000-8000-000000000001', '00000000-0000-7000-8000-000000000000', 'BRL', 'settlement', 'active', 1, now()),
    ('01900000-0000-7000-8000-000000000002', '00000000-0000-7000-8000-000000000000', 'USD', 'settlement', 'active', 1, now()),
    ('01900000-0000-7000-8000-000000000003', '00000000-0000-7000-8000-000000000000', 'EUR', 'settlement', 'active', 1, now());
