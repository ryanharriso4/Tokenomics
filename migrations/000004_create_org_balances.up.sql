CREATE TABLE IF NOT EXISTS org_balances (
    org_id INT PRIMARY KEY REFERENCES organizations(org_id) ON DELETE CASCADE,
    plan_id INT NOT NULL REFERENCES plans(plan_id),
    tokens_remaining BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);