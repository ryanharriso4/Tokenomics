CREATE TABLE IF NOT EXISTS transactions (
    transaction_id SERIAL PRIMARY KEY,
    num_tokens integer NOT NULL, 
    tstamp timestamp, 
    idempotency_key UUID
);

CREATE TABLE IF NOT EXISTS transactions_organizations (
    id SERIAL PRIMARY KEY,
    transaction_id INT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    org_id INT NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
    UNIQUE (org_id, transaction_id)
);

CREATE TABLE IF NOT EXISTS transactions_users (
    id SERIAL PRIMARY KEY,
    transaction_id INT NOT NULL REFERENCES transactions(transaction_id) ON DELETE CASCADE,
    user_id INT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    UNIQUE (user_id, transaction_id)
);
