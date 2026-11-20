package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"main.go/internal/handler"
	"main.go/internal/repository"
)

func main() {
	pool, err := ConnectToDB()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer pool.Close()

	walletRepo := &repository.PostgresWalletRepository{Pool: pool}

	walletHandler := handler.WalletHandler{Repo: walletRepo}

	http.HandleFunc("GET /api/v1/wallets/{id}", walletHandler.GetByID)
	http.HandleFunc("POST /api/v1/wallet", walletHandler.WalletOperation)

	fmt.Println("go-wallet запущен на порту :8080")
	http.ListenAndServe(":8080", nil)
}

func ConnectToDB() (*pgxpool.Pool, error) {
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:5432/%s",
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_DB"),
	)

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	config.MinConns = 10
	config.MaxConns = 50

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
