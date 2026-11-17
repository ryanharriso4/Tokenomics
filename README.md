# Tokenomics: Multi-Tenant Token Ledger & Rate Limiter

A fast API usage tracker and rate limiter built with **Go**, **Redis**, and **PostgreSQL**. It stops users from overspending their token quotas or spamming the API, without slowing down requests with heavy database locks.

## How It Works

```text
[Client / Vegeta]
       │  POST /transactions
       ▼
[Go HTTP Server]
       │  1. Atomic Lua script (Checks rate limit, deducts tokens, queues event)
       ▼
[Redis Memory] ──► Blocks request (429) if over limit or out of tokens
       │
       │  2. Background worker flushes batches of 100 events
       ▼
[PostgreSQL] ────► Saves permanent transaction log & updated org balances
```

* **Fast In-Memory Checks:** A Redis Lua script handles rate limiting, duplicate request checks, and token math in one step. This prevents race conditions and keeps balances from ever dropping below zero.
* **Background Database Writes:** Instead of making users wait on PostgreSQL disk writes, approved requests go into a Redis Stream. A background Go worker grabs them in batches of 100 and saves them to PostgreSQL safely.
* **Clean Ledger & Balances:** PostgreSQL keeps a permanent history of every transaction (`transactions`) alongside a quick lookup table for each company's current balance (`org_balances`).

## Load Test Benchmarks (Vegeta)

Tested locally at **200 req/s (3,000 requests)** across 10 organizations and 50 users spanning Starter, Pro, and Enterprise rate plans:

| Architecture | Median Latency (`p50`) | Tail Latency (`p95`) | `p99` Latency | Key Takeaway |
| :--- | :--- | :--- | :--- | :--- |
| **PostgreSQL Row-Locking Only** | `2.84 ms` | `4.36 ms` | `5.53 ms` | Synchronous `BEGIN/INSERT/UPDATE/COMMIT` row locks on every request |
| **Redis Lua + Async Stream Worker** | **`1.10 ms` (-61%)** | **`1.77 ms` (-59%)** | `5.75 ms` | Sub-millisecond in-memory enforcement with zero balance drift |

## Quick Start

### 1. Prerequisites
* Go 1.22+
* Docker & Docker Compose
* `golang-migrate` CLI

### 2. Start Infrastructure & Seed Database
```bash

#.env configuration
Create a .env file with the variables DB_NAME, DB_PASSWORD, DB_USER, DATABASE_URL, REDIS_PASSWORD

# Start PostgreSQL and Redis
docker compose up -d

# Run database migrations
migrate -source "file://migrations" -database "\$DATABASE_URL" up

# Seed plans, 10 organizations, 50 users, and initial balances
docker exec -i tokenomics-db-1 psql -U Tokenomics -d Tokenomics_DB < seed.sql
```

### 3. Run the Server & Load Test
```bash
# Terminal 1: Start API server
go run main.go

# Terminal 2: Run Vegeta load test
go run cmd/loadtest/main.go -url="http://127.0.0" -rate=200 -duration=15s
```

```
