package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	// Load the .env file from the current directory
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Warning: No .env file found, falling back to system environment variables")
	}

	// Fetch variables loaded from your .env
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbHost := "localhost" // Assuming running on your host machine
	dbPort := "5432"

	// Construct the secure connection string dynamically
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		dbUser, dbPassword, dbHost, dbPort, dbName,
	)

	// Configure the connection pool
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to parse connection string: %v\n", err)
		os.Exit(1)
	}

	// Set a connection timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect to the database pool
	fmt.Println("Connecting to Docker Postgres securely using environment variables...")
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create connection pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Ping the connection
	err = pool.Ping(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ping failed. Could not reach database: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Successfully connected and pinged the database!")
}
