package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type TransactionRequest struct {
	OrgID          int    `json:"org_id"`
	UserID         int    `json:"user_id"`
	NumTokens      int    `json:"num_tokens"`
	IdempotencyKey string `json:"idempotency_key"`
}

const deductLuaScript = `
local org_key = KEYS[1]
local idem_key = KEYS[2]
local stream_key = KEYS[3]

local cost = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local payload = ARGV[3]
local idem_ttl = tonumber(ARGV[4])

-- 1. Idempotency Check
if redis.call("EXISTS", idem_key) == 1 then
    return 2
end

-- 2. Fetch Org Cache State
local data = redis.call("HMGET", org_key, "tokens_remaining", "rps", "burst_limit", "rl_tokens", "last_refill")
if not data[1] then
    return -3
end

local balance = tonumber(data[1])
local rps = tonumber(data[2])
local burst = tonumber(data[3])
local rl_tokens = tonumber(data[4]) or burst
local last_refill = tonumber(data[5]) or now

-- 3. Token-Bucket Rate Limiter Evaluation
local elapsed = math.max(0, now - last_refill)
rl_tokens = math.min(burst, rl_tokens + (elapsed * rps))

if rl_tokens < 1 then
    return -2
end

-- 4. Long-Term Token Balance Evaluation
if balance < cost then
    return -1
end

-- 5. Commit Memory Deductions and Append to Event Stream
rl_tokens = rl_tokens - 1
balance = balance - cost

redis.call("HMSET", org_key,
    "tokens_remaining", balance,
    "rl_tokens", rl_tokens,
    "last_refill", now
)
redis.call("SET", idem_key, "1", "EX", idem_ttl)
redis.call("XADD", stream_key, "*", "payload", payload)

return 1
`

func main() {
	_ = godotenv.Overload()

	// Listen for Ctrl+C (SIGINT) or SIGTERM for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Establish PostgreSQL Connection Pool
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbName := os.Getenv("DB_NAME")
		dbUser := os.Getenv("DB_USER")
		dbPassword := os.Getenv("DB_PASSWORD")
		dbURL = fmt.Sprintf("postgres://%s:%s@127.0.0.1:5433/%s?sslmode=disable", dbUser, dbPassword, dbName)
	}

	pgPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to PostgreSQL: %v\n", err)
	}
	defer pgPool.Close()

	if err := pgPool.Ping(ctx); err != nil {
		log.Fatalf("PostgreSQL ping failed: %v\n", err)
	}
	log.Println("Connected to PostgreSQL successfully!")

	// 2. Establish Redis Client Connection
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Redis ping failed: %v\n", err)
	}
	log.Println("Connected to Redis successfully!")

	// 3. Hydrate Redis Caches from PostgreSQL Snapshot Table
	log.Println("Hydrating Redis from PostgreSQL database...")
	if err := hydrateRedis(ctx, pgPool, rdb); err != nil {
		log.Fatalf("Cache hydration failed: %v\n", err)
	}
	log.Println("Redis hydration complete! Loading Lua script...")

	// 4. Pre-load Lua Script SHA into Redis
	luaSHA, err := rdb.ScriptLoad(ctx, deductLuaScript).Result()
	if err != nil {
		log.Fatalf("Failed to compile Lua script: %v\n", err)
	}

	// 5. Initialize Background Redis Stream Consumer Group & Worker
	streamKey := "transactions:stream"
	groupName := "pg_writers"
	_ = rdb.XGroupCreateMkStream(ctx, streamKey, groupName, "$").Err()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		streamToPostgresWorker(ctx, pgPool, rdb, streamKey, groupName, "worker_1")
	}()

	// 6. Configure HTTP Router & Server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transactions", func(w http.ResponseWriter, r *http.Request) {
		var req TransactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		payloadBytes, _ := json.Marshal(req)
		nowUnix := float64(time.Now().UnixNano()) / 1e9

		keys := []string{
			fmt.Sprintf("org:%d", req.OrgID),
			fmt.Sprintf("idem:%s", req.IdempotencyKey),
			streamKey,
		}
		args := []interface{}{
			req.NumTokens,
			nowUnix,
			string(payloadBytes),
			86400, // 24-hour Idempotency key TTL
		}

		res, err := rdb.EvalSha(r.Context(), luaSHA, keys, args...).Result()
		if err != nil {
			log.Printf("Lua execution error: %v\n", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		status := res.(int64)
		switch status {
		case 1:
			w.WriteHeader(http.StatusCreated)
		case 2:
			w.WriteHeader(http.StatusOK)
		case -1:
			http.Error(w, "token quota exhausted", http.StatusTooManyRequests)
		case -2:
			http.Error(w, "RPS rate limit exceeded", http.StatusTooManyRequests)
		default:
			http.Error(w, "organization context not found", http.StatusNotFound)
		}
	})

	srv := &http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: mux,
	}

	go func() {
		log.Println("Server listening on http://127.0.0.1:8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v\n", err)
		}
	}()

	// Block until Ctrl+C / SIGTERM is received
	<-ctx.Done()
	log.Println("Shutdown signal received, draining requests and stream worker...")

	// Give in-flight HTTP requests 5 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server forced shutdown: %v\n", err)
	}

	// Wait for background stream worker to finish flushing its active Postgres batch
	wg.Wait()
	log.Println("Graceful shutdown complete.")
}

// hydrateRedis loads current balances and plan limits from Postgres into Redis on startup
func hydrateRedis(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client) error {
	rows, err := pool.Query(ctx, `
		SELECT b.org_id, b.tokens_remaining, p.rps, p.burst_limit
		FROM org_balances b
		JOIN plans p ON p.plan_id = b.plan_id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	pipe := rdb.Pipeline()
	for rows.Next() {
		var orgID int
		var tokensRemaining int64
		var rps, burstLimit int
		if err := rows.Scan(&orgID, &tokensRemaining, &rps, &burstLimit); err != nil {
			return err
		}

		key := fmt.Sprintf("org:%d", orgID)
		pipe.HSet(ctx, key, map[string]interface{}{
			"tokens_remaining": tokensRemaining,
			"rps":              rps,
			"burst_limit":      burstLimit,
			"rl_tokens":        burstLimit,
		})
	}
	_, err = pipe.Exec(ctx)
	return err
}

// streamToPostgresWorker processes batches of transactions from Redis Streams into Postgres asynchronously
func streamToPostgresWorker(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, stream, group, consumer string) {
	log.Printf("Background stream consumer %s initialized.\n", consumer)

	for ctx.Err() == nil {
		// Read up to 100 entries at a time; unblocks immediately if ctx is canceled
		streams, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumer,
			Streams:  []string{stream, ">"},
			Count:    100,
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, redis.Nil) {
				continue
			}
			log.Printf("Stream read error: %v\n", err)
			time.Sleep(time.Second)
			continue
		}

		if len(streams) == 0 || len(streams[0].Messages) == 0 {
			continue
		}

		messages := streams[0].Messages

		// Use a dedicated write context so an in-progress batch finishes saving even during shutdown
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		tx, err := pool.Begin(writeCtx)
		if err != nil {
			log.Printf("Worker database transaction failed: %v\n", err)
			writeCancel()
			continue
		}

		batchOk := true
		var msgIDs []string

		for _, msg := range messages {
			payloadStr, ok := msg.Values["payload"].(string)
			if !ok {
				log.Printf("Warning: invalid payload in stream message %s\n", msg.ID)
				continue
			}

			var req TransactionRequest
			_ = json.Unmarshal([]byte(payloadStr), &req)

			_, err = tx.Exec(writeCtx,
				`INSERT INTO transactions (org_id, user_id, num_tokens, idempotency_key)
				 VALUES ($1, $2, $3, $4) ON CONFLICT (idempotency_key) DO NOTHING`,
				req.OrgID, req.UserID, req.NumTokens, req.IdempotencyKey,
			)
			if err != nil {
				log.Printf("Asynchronous ledger write failed: %v\n", err)
				batchOk = false
				break
			}

			_, err = tx.Exec(writeCtx,
				`UPDATE org_balances SET tokens_remaining = tokens_remaining - $1, updated_at = NOW() WHERE org_id = $2`,
				req.NumTokens, req.OrgID,
			)
			if err != nil {
				log.Printf("Asynchronous balance adjustment failed: %v\n", err)
				batchOk = false
				break
			}

			msgIDs = append(msgIDs, msg.ID)
		}

		if batchOk && len(msgIDs) > 0 {
			if err := tx.Commit(writeCtx); err == nil {
				pipe := rdb.Pipeline()
				pipe.XAck(writeCtx, stream, group, msgIDs...)
				pipe.XDel(writeCtx, stream, msgIDs...)
				_, _ = pipe.Exec(writeCtx)
			} else {
				log.Printf("Worker transaction commit failed: %v\n", err)
				_ = tx.Rollback(writeCtx)
			}
		} else {
			_ = tx.Rollback(writeCtx)
			if !batchOk {
				time.Sleep(time.Second)
			}
		}
		writeCancel()
	}

	log.Printf("Background stream consumer %s stopped cleanly.\n", consumer)
}
