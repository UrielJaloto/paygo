CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY,
    transfer_id uuid NOT NULL,
    account_id uuid NOT NULL REFERENCES accounts,
    direction text NOT NULL CHECK (direction IN ('debit', 'credit')),
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency char(3) NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX ledger_entries_account_id_id_idx ON ledger_entries (account_id, id DESC);

CREATE FUNCTION reject_ledger_entry_change() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_entry_change();
