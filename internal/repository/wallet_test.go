package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"main.go/internal/models"
)

func connectToDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	err := godotenv.Load("../../config-test.env")
	if err != nil {
		t.Fatal(err)
	}

	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s",
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		"localhost",
		os.Getenv("POSTGRES_PORT"),
		os.Getenv("POSTGRES_DB"),
	)

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	config.MinConns = 0
	config.MaxConns = 10

	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping database: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

func createWallets(t *testing.T, pool *pgxpool.Pool, amount int) []uuid.UUID {
	t.Helper()

	ids := make([]uuid.UUID, amount)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for i := 0; i < len(ids); i++ {
		id := uuid.New()
		ids[i] = id
		_, err := pool.Exec(
			ctx,
			`INSERT INTO wallets (walletid, balance)
			VALUES ($1, 0)`,
			id.String(),
		)
		if err != nil {
			t.Fatalf("create wallets: %v", err)
		}
	}
	return ids
}

type BalanceChangeCase struct {
	t               *testing.T
	r               *PostgresWalletRepository
	Op              models.Operation
	ExpectedResult  error
	ExpectedBalance int
}

func (c *BalanceChangeCase) test() {
	c.t.Helper()

	err := c.r.ChangeBalance(c.t.Context(), c.Op)
	c.t.Logf("Request: ChangeBalance:\n%v\nResponse: %v", c.Op, err)
	if !errors.Is(err, c.ExpectedResult) {
		c.t.Errorf("unexpected reuslt: %v ; expected: %v", err, c.ExpectedResult)
	}

	wallet, _ := c.r.GetByID(c.t.Context(), c.Op.UUID)
	if wallet == nil {
		return
	}
	c.t.Logf("New balance: %v", wallet.Balance)
	if wallet.Balance != c.ExpectedBalance {
		c.t.Errorf("unexpected balance: %v ; expected: %v", wallet.Balance, c.ExpectedBalance)
	}
}

func formBalanceChangeCase(t *testing.T,
	r *PostgresWalletRepository,
	id uuid.UUID,
	opType string,
	amount int,
	expectedResult error,
	expectedBalance int,
) BalanceChangeCase {

	t.Helper()

	return BalanceChangeCase{
		t: t,
		r: r,
		Op: models.Operation{
			UUID:          id,
			OperationType: opType,
			Amount:        amount,
		},
		ExpectedResult:  expectedResult,
		ExpectedBalance: expectedBalance,
	}
}

func TestGetByID(t *testing.T) {
	pool := connectToDB(t)
	ids := createWallets(t, pool, 3)

	r := &PostgresWalletRepository{Pool: pool}

	//Получение баланса
	for _, id := range ids {
		t.Logf("Request: GetByID(%s)\n", id)
		wallet, err := r.GetByID(t.Context(), id)
		if err != nil {
			t.Errorf("getting wallet: %s", err)
			continue
		}

		t.Logf("Response: %v\n", wallet)
		if wallet.UUID != id {
			t.Errorf("error: wrong wallet")
		}
	}

	//Несуществующий ID
	id := uuid.New()
	wallet, err := r.GetByID(t.Context(), id)
	t.Logf("Request: GetByID(%s)\nResponse: %v\n", id, wallet)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected no rows error")
	}

	//Нулевой ID
	id = uuid.Nil()
	wallet, err = r.GetByID(t.Context(), id)
	t.Logf("Request: GetByID(%s)\nResponse: %v\n", id, wallet)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected no rows error")
	}

	//!Неверный формат отсекается хендлером
}

func TestChangeBalance(t *testing.T) {
	pool := connectToDB(t)
	id := createWallets(t, pool, 1)[0]

	const (
		wd = "WITHDRAW"
		dp = "DEPOSIT"
	)

	r := &PostgresWalletRepository{Pool: pool}

	cases := make([]BalanceChangeCase, 0, 50)

	//Пополнение
	cases = append(cases, formBalanceChangeCase(
		t, r, id, dp, 1000, nil, 1000))
	//Снятие
	cases = append(cases, formBalanceChangeCase(
		t, r, id, wd, 100, nil, 900))

	//Снятие > баланса
	cases = append(cases, formBalanceChangeCase(
		t, r, id, wd, 100000, models.ErrInsufficientFunds, 900))
	//Снятие = балансу
	cases = append(cases, formBalanceChangeCase(
		t, r, id, wd, 900, nil, 0))

	//Пополнение на 0
	cases = append(cases, formBalanceChangeCase(
		t, r, id, dp, 0, models.ErrInvalidOperation, 0))
	//Снятие 0
	cases = append(cases, formBalanceChangeCase(
		t, r, id, wd, 0, models.ErrInvalidOperation, 0))

	//Несуществующий ID
	cases = append(cases, formBalanceChangeCase(
		t, r, uuid.New(), dp, 900, models.ErrWalletNotFound, 0))
	//Нулевой ID
	cases = append(cases, formBalanceChangeCase(
		t, r, uuid.Nil(), dp, 900, models.ErrWalletNotFound, 0))

	for _, testCase := range cases {
		testCase.test()
	}
}

func TestConcurenct(t *testing.T) {
	pool := connectToDB(t)
	id := createWallets(t, pool, 1)[0]

	r := &PostgresWalletRepository{Pool: pool}

	_, err := pool.Exec(t.Context(),
		`UPDATE wallets SET balance = 1000 WHERE walletid = $1`,
		id.String(),
	)
	if err != nil {
		t.Fatal(err)
	}

	requestAmount := 150
	requestSum := 10

	start := make(chan struct{})
	results := make(chan error, requestAmount)

	var wg sync.WaitGroup

	for i := 0; i < requestAmount; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			err := r.ChangeBalance(t.Context(), models.Operation{
				UUID:          id,
				OperationType: "WITHDRAW",
				Amount:        requestSum,
			})

			results <- err
		}()
	}

	close(start)

	wg.Wait()
	close(results)

	var success, insufficient int
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, models.ErrInsufficientFunds):
			insufficient++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	wallet, err := r.GetByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Success: %v", success)
	t.Logf("Insufficient: %v", insufficient)
	t.Logf("Fincal balance: %v", wallet.Balance)

	if success != 100 {
		t.Errorf("expected 100 successes")
	}

	if insufficient != 50 {
		t.Errorf("expected 50 insufficient errors")
	}
}
