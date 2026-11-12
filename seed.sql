BEGIN;

-- 1. Wipe existing test data and reset all SERIAL sequences back to 1
TRUNCATE TABLE 
    transactions, 
    org_balances,
    organization_users, 
    users, 
    organizations, 
    plans 
RESTART IDENTITY CASCADE;

-- 2. Seed sample rate-limiting plans (plan_id 1 through 3)
INSERT INTO plans (rps, max, conc_limit, burst_limit) VALUES
    (10, 1000, 5, 20),       -- Plan 1: Free / Starter tier
    (100, 50000, 25, 100),   -- Plan 2: Pro tier
    (1000, 1000000, 100, 500); -- Plan 3: Enterprise tier

-- 3. Seed 10 organizations (org_id 1 through 10)
INSERT INTO organizations (name)
SELECT 'Organization ' || i
FROM generate_series(1, 10) AS i;

-- 4. Seed 50 users (user_id 1 through 50)
INSERT INTO users (full_name)
SELECT 'Test User ' || i
FROM generate_series(1, 50) AS i;

-- 5. Link users to organizations (5 users per org) so (org_id, user_id) is valid
-- Users 1..5 -> Org 1, Users 6..10 -> Org 2, ..., Users 46..50 -> Org 10
INSERT INTO organization_users (org_id, user_id)
SELECT 
    ((i - 1) / 5) + 1 AS org_id,
    i AS user_id
FROM generate_series(1, 50) AS i;

-- 6. Initialize org_balances for all 10 organizations
-- Orgs 1..4 get Plan 1 (1k tokens), Orgs 5..8 get Plan 2 (50k), Orgs 9..10 get Plan 3 (1M)
INSERT INTO org_balances (org_id, plan_id, tokens_remaining)
SELECT 
    o.org_id,
    CASE 
        WHEN o.org_id <= 4 THEN 1
        WHEN o.org_id <= 8 THEN 2
        ELSE 3
    END AS plan_id,
    p.max AS tokens_remaining
FROM organizations o
JOIN plans p ON p.plan_id = CASE 
    WHEN o.org_id <= 4 THEN 1
    WHEN o.org_id <= 8 THEN 2
    ELSE 3
END;

COMMIT;

