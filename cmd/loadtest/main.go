package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// TransactionRequest matches the columns in your transactions table
type TransactionRequest struct {
	OrgID          int    `json:"org_id"`
	UserID         int    `json:"user_id"`
	NumTokens      int    `json:"num_tokens"`
	IdempotencyKey string `json:"idempotency_key"`
}

// NewSeededTargeter builds valid requests that match seed.sql (Users 1..50 mapped to Orgs 1..10)
func NewSeededTargeter(targetURL string, numUsers, usersPerOrg int) vegeta.Targeter {
	return func(tgt *vegeta.Target) error {
		if tgt == nil {
			return vegeta.ErrNilTarget
		}

		// Pick user 1..50 and derive their exact org 1..10 so the composite FK always succeeds
		userID := rand.Intn(numUsers) + 1
		orgID := ((userID - 1) / usersPerOrg) + 1

		payload := TransactionRequest{
			OrgID:          orgID,
			UserID:         userID,
			NumTokens:      rand.Intn(10) + 1, // Random spend of 1 to 10 tokens
			IdempotencyKey: uuid.NewString(),  // Fresh UUID per request
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		tgt.Method = http.MethodPost
		tgt.URL = targetURL
		tgt.Body = body
		tgt.Header = http.Header{
			"Content-Type": []string{"application/json"},
		}
		return nil
	}
}

func main() {
	// Command-line flags so you can tweak load without editing code
	url := flag.String("url", "http://localhost:8080/transactions", "Target API endpoint")
	freq := flag.Int("rate", 100, "Requests per second")
	dur := flag.Duration("duration", 15*time.Second, "Test duration")
	flag.Parse()

	rate := vegeta.Rate{Freq: *freq, Per: time.Second}
	targeter := NewSeededTargeter(*url, 50, 5)
	attacker := vegeta.NewAttacker()

	fmt.Printf("Attacking %s at %d req/s for %s...\n\n", *url, *freq, *dur)

	var metrics vegeta.Metrics
	for res := range attacker.Attack(targeter, rate, *dur, "Tokenomics Load Test") {
		metrics.Add(res)
	}
	metrics.Close()

	// Print latency percentiles (p50, p95, p99), throughput, and error summary
	reporter := vegeta.NewTextReporter(&metrics)
	reporter.Report(os.Stdout)

	fmt.Printf("\nHTTP Status Breakdown: %v\n", metrics.StatusCodes)
	if len(metrics.Errors) > 0 {
		fmt.Printf("Sample Errors: %v\n", metrics.Errors)
	}
}
