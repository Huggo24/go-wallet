package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"main.go/internal/repository"
)

type testRequest struct {
	t            *testing.T
	Req          *http.Request
	Handler      func(http.ResponseWriter, *http.Request)
	ExpectedCode int
}

func (tr *testRequest) test() {
	tr.t.Helper()

	resp := httptest.NewRecorder()

	reqBody, _ := io.ReadAll(tr.Req.Body)
	tr.t.Logf("\nRequest: %s\n%s", tr.Req.RequestURI, reqBody)
	tr.Req.Body = io.NopCloser(bytes.NewReader(reqBody))

	tr.Handler(resp, tr.Req)

	tr.t.Logf("Response:%s\n", resp.Body)

	if status := resp.Code; status != tr.ExpectedCode {
		tr.t.Errorf("POST /wallet: status %v", status)
	}
}

func formTestRequestGetByID(t *testing.T, h *WalletHandler, id string, expectedCode int) testRequest {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/wallets/", nil)
	req.SetPathValue("id", id)
	return testRequest{
		t:            t,
		Req:          req,
		Handler:      h.GetByID,
		ExpectedCode: expectedCode,
	}
}

func formTestRequestWalletOperation(t *testing.T, h *WalletHandler, id, operationType, amount string, expectedCode int) testRequest {
	t.Helper()

	body := formOperation(t, id, operationType, amount)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/wallet", body)
	return testRequest{
		t:            t,
		Req:          req,
		Handler:      h.WalletOperation,
		ExpectedCode: expectedCode,
	}

}

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
	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
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

func formOperation(t *testing.T, id, operationType, amount string) *strings.Reader {
	t.Helper()

	body := strings.NewReader(
		fmt.Sprintf(`
			{
				"walletid": "%v",
				"operationType": "%v",
				"amount": %v
			}`,
			id,
			operationType,
			amount,
		),
	)
	return body
}

func TestGetByID(t *testing.T) {
	pool := connectToDB(t)
	ids := createWallets(t, pool, 3)

	walletRepo := &repository.PostgresWalletRepository{Pool: pool}
	h := &WalletHandler{Repo: walletRepo}

	cases := make([]testRequest, 0, 50)
	//Существующие id
	for _, id := range ids {
		cases = append(cases, formTestRequestGetByID(t, h, id.String(), 200))
	}

	//Несуществующий id
	cases = append(cases, formTestRequestGetByID(t, h, uuid.New().String(), 404))
	//Пустой id
	cases = append(cases, formTestRequestGetByID(t, h, uuid.Nil().String(), 400))

	//Неверный формат id
	var wrongIds = []string{
		"testing-uuid-format-check-ing",
		"!@#$%^&*()_+",
		"-----",
		"?123&456",
	}
	for _, id := range wrongIds {
		cases = append(cases, formTestRequestGetByID(t, h, id, 400))
	}

	for _, testCase := range cases {
		testCase.test()
	}
}

func TestWalletOperation(t *testing.T) {
	pool := connectToDB(t)
	id := createWallets(t, pool, 1)[0].String()
	const (
		wd = "WITHDRAW"
		dp = "DEPOSIT"
	)

	walletRepo := &repository.PostgresWalletRepository{Pool: pool}
	h := &WalletHandler{Repo: walletRepo}

	cases := make([]testRequest, 0, 50)

	//Пополнение
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, dp, "1000", 200))
	//Снятие
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, wd, "100", 200))

	//Отриц. пополнение
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, dp, "-100", 400))
	//Отриц. снятие
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, wd, "-100", 400))
	//Несуществующий ID
	cases = append(cases, formTestRequestWalletOperation(
		t, h, uuid.New().String(), dp, "100", 400))
	//Пустой ID
	cases = append(cases, formTestRequestWalletOperation(
		t, h, uuid.New().String(), dp, "100", 400))
	//Неверный формат ID
	cases = append(cases, formTestRequestWalletOperation(
		t, h, "1-2-3-4-5", wd, "100", 400))
	cases = append(cases, formTestRequestWalletOperation(
		t, h, "!@#$%^&*()_+", wd, "100", 400))
	//Неверный формат суммы
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, wd, "10.5", 400))
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, wd, "aaa", 400))
	//Неверные операции
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, "BALANCE", "100", 400))
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, "123123", "100", 400))
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, "", "100", 400))

	//Недостаточно средств
	cases = append(cases, formTestRequestWalletOperation(
		t, h, id, wd, "100000", 409))

	for _, testCase := range cases {
		testCase.test()
	}
}
