package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadAPI(t *testing.T) {

	pool := connectToDB(t)
	id := createWallets(t, pool, 1)[0]

	_, err := pool.Exec(
		t.Context(),
		`UPDATE wallets SET balance = 10000 WHERE walletid = $1`,
		id,
	)
	if err != nil {
		t.Fatal(err)
	}

	url := "http://localhost:8081/api/v1/wallet"
	workers := 50

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        workers,
			MaxIdleConnsPerHost: workers,
			MaxConnsPerHost:     workers,
		},
	}

	var (
		requests atomic.Int64
		success  atomic.Int64
		failures atomic.Int64
	)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				if ctx.Err() != nil {
					return
				}

				body := strings.NewReader(fmt.Sprintf(`
				{
					"walletid": "%v",
					"operationType": "%v",
					"amount": %v
				}`, id.String(), "WITHDRAW", 1))
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
				if err != nil {
					failures.Add(1)
					//t.Log("bad request")
					return
				}

				resp, err := client.Do(req)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					//t.Logf("error response: %s", resp.StatusCode)
					failures.Add(1)
					continue
				}

				//Освобождаем соединение
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				requests.Add(1)

				if resp.StatusCode == 200 {
					success.Add(1)
				} else {
					failures.Add(1)
				}
			}
		}()
	}

	<-ctx.Done()
	wg.Wait()

	elapsed := time.Since(start).Seconds()

	fmt.Printf("\n--- LOAD TEST RESULTS ---\n")
	fmt.Printf("Workers:       %d\n", workers)
	fmt.Printf("Duration:      %.3f sec\n", elapsed)
	fmt.Printf("Requests:      %d\n", requests.Load())
	fmt.Printf("Successful:    %d\n", success.Load())
	fmt.Printf("Failed:        %d\n", failures.Load())
	fmt.Printf("RPS:           %.2f\n", float64(requests.Load())/elapsed)

	client.CloseIdleConnections()
}
