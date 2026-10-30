CREATE TABLE outbox_events (
    id uuid PRIMARY KEY,
    type text NOT NULL,
    transfer_id uuid REFERENCES transfers,
    owner_ids uuid[] NOT NULL,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz NULL
);

CREATE INDEX outbox_events_unpublished_idx ON outbox_events (id) WHERE published_at IS NULL;

CREATE TABLE webhook_endpoints (
    id uuid PRIMARY KEY,
    owner_id uuid NOT NULL,
    url text NOT NULL,
    secret text NOT NULL,
    is_active boolean NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE TABLE webhook_deliveries (
    id uuid PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES outbox_events,
    endpoint_id uuid NOT NULL REFERENCES webhook_endpoints,
    owner_id uuid NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'succeeded', 'dead')),
    attempts int NOT NULL,
    next_attempt_at timestamptz NOT NULL,
    last_error text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL
);

CREATE INDEX webhook_deliveries_due_idx ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';

CREATE INDEX webhook_deliveries_owner_id_id_idx ON webhook_deliveries (owner_id, id DESC);
