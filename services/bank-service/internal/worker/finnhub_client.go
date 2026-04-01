package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const finnhubBaseURL = "https://finnhub.io/api/v1"

// finnhubClient je jednostavan HTTP klijent za Finnhub API.
type finnhubClient struct {
	apiKey     string
	httpClient *http.Client
}

func newFinnhubClient(apiKey string) *finnhubClient {
	return &finnhubClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ─── Quote ────────────────────────────────────────────────────────────────────

// finnhubQuote mapira odgovor /quote endpoint-a.
type finnhubQuote struct {
	C  float64 `json:"c"`  // current price
	D  float64 `json:"d"`  // change
	Dp float64 `json:"dp"` // percent change
	H  float64 `json:"h"`  // high of the day
	L  float64 `json:"l"`  // low of the day
	O  float64 `json:"o"`  // open of the day
	Pc float64 `json:"pc"` // previous close
	T  int64   `json:"t"`  // timestamp (unix)
}

// Quote dohvata trenutni quote za dati ticker.
func (c *finnhubClient) Quote(ctx context.Context, symbol string) (*finnhubQuote, error) {
	url := fmt.Sprintf("%s/quote?symbol=%s&token=%s", finnhubBaseURL, symbol, c.apiKey)
	return doGet[finnhubQuote](ctx, c.httpClient, url)
}

// ─── Stock Candles ────────────────────────────────────────────────────────────

// finnhubCandles mapira odgovor /stock/candle endpoint-a.
type finnhubCandles struct {
	C []float64 `json:"c"` // close prices
	H []float64 `json:"h"` // high prices
	L []float64 `json:"l"` // low prices
	O []float64 `json:"o"` // open prices
	V []float64 `json:"v"` // volumes
	T []int64   `json:"t"` // unix timestamps
	S string    `json:"s"` // status: "ok" | "no_data"
}

// Candles dohvata OHLCV podatke za dnevnu rezoluciju u datom periodu.
func (c *finnhubClient) Candles(ctx context.Context, symbol string, from, to time.Time) (*finnhubCandles, error) {
	url := fmt.Sprintf(
		"%s/stock/candle?symbol=%s&resolution=D&from=%d&to=%d&token=%s",
		finnhubBaseURL, symbol, from.Unix(), to.Unix(), c.apiKey,
	)
	return doGet[finnhubCandles](ctx, c.httpClient, url)
}

// ─── Generic HTTP helper ──────────────────────────────────────────────────────

func doGet[T any](ctx context.Context, client *http.Client, url string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("finnhub HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &result, nil
}
