CREATE TABLE IF NOT EXISTS plans (
    plan_id serial primary key,
    rps          integer not null, 
    max          integer not null, 
    conc_limit   integer not null,
    burst_limit  integer not null
);