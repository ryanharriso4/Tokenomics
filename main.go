package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type TransactionRequest struct {
	OrgID          int    `json:"org_id"`
	UserID         int    `json:"user_id"`
	NumTokens      int    `json:"num_tokens"`
	IdempotencyKey string `json:"idempotency_key"`
}

func main() {

	_ = godotenv.Load()

	dbName := os.Getenv("DB_NAME")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")

	dbURL := fmt.Sprintf("postgres://%s:%s@127.0.0.1:5433/%s?sslmode=disable", dbUser, dbPassword, dbName)

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Postgres connection failed: %v\n", err)
	}
	log.Println("Connected to PostgreSQL!")
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transactions", func(w http.ResponseWriter, r *http.Request) {
		var req TransactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		tx, err := pool.Begin(r.Context())
		if err != nil {
			log.Printf("Failed to start tx: %v\n", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(r.Context())

		tag, err := tx.Exec(r.Context(),
			`INSERT INTO transactions (org_id, user_id, num_tokens, idempotency_key)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (idempotency_key) DO NOTHING`,
			req.OrgID, req.UserID, req.NumTokens, req.IdempotencyKey,
		)

		if err != nil {
			log.Printf("Transaction insert failed: %v\n", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if tag.RowsAffected() == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		var remaining int64
		err = tx.QueryRow(r.Context(),
			`UPDATE org_balances
			 SET tokens_remaining = tokens_remaining - $1,
			     updated_at = NOW()
			 WHERE org_id = $2 AND tokens_remaining >= $1
			 RETURNING tokens_remaining`,
			req.NumTokens, req.OrgID,
		).Scan(&remaining)

		if err != nil {
			http.Error(w, "token quota exceeded", http.StatusTooManyRequests)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			log.Printf("Failed to commit tx: %v\n", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)

	})

	log.Println("Server listening on http://127.0.0.1:8080")
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", mux))

}
