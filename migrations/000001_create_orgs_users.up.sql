CREATE TABLE IF NOT EXISTS organizations (
    org_id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    user_id SERIAL PRIMARY KEY,
    full_name VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS organization_users (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    org_id INT NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
    UNIQUE (org_id, user_id)
);
