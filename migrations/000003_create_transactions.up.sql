CREATE TABLE IF NOT EXISTS transactions (
    transaction_id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
    user_id INT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    num_tokens INTEGER NOT NULL,
    tstamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    idempotency_key UUID UNIQUE,
    FOREIGN KEY (org_id, user_id) REFERENCES organization_users(org_id, user_id)
);


CREATE INDEX IF NOT EXISTS idx_transactions_org_id ON transactions (org_id);
CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions (user_id);