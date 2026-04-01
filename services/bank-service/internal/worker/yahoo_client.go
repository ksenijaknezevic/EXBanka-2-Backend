package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const yahooOptionsURLFmt = "https://query1.finance.yahoo.com/v6/finance/options/%s"

// ─── Response structs ────────────────────────────────────────────────────────

type yahooOptionsResp struct {
	OptionChain yahooOptionChain `json:"optionChain"`
}

type yahooOptionChain struct {
	Result []yahooOptionResult `json:"result"`
}

type yahooOptionResult struct {
	UnderlyingSymbol string              `json:"underlyingSymbol"`
	Quote            yahooQuote          `json:"quote"`
	Options          []yahooOptionExpiry `json:"options"`
}

type yahooQuote struct {
	RegularMarketPrice float64 `json:"regularMarketPrice"`
}

type yahooOptionExpiry struct {
	Calls []yahooContract `json:"calls"`
	Puts  []yahooContract `json:"puts"`
}

type yahooContract struct {
	ContractSymbol    string  `json:"contractSymbol"`
	Strike            float64 `json:"strike"`
	LastPrice         float64 `json:"lastPrice"`
	Bid               float64 `json:"bid"`
	Ask               float64 `json:"ask"`
	Volume            int64   `json:"volume"`
	OpenInterest      int64   `json:"openInterest"`
	ImpliedVolatility float64 `json:"impliedVolatility"`
}

// ─── Client function ─────────────────────────────────────────────────────────

// fetchYahooOptions dohvata opcijski lanac za dati underlying ticker sa Yahoo Finance.
// Yahoo Finance ne zahteva API ključ; koristimo browser User-Agent da izbegnemo 429.
func fetchYahooOptions(ctx context.Context, client *http.Client, underlyingSymbol string) (*yahooOptionsResp, error) {
	url := fmt.Sprintf(yahooOptionsURLFmt, underlyingSymbol)

	req, err := newYahooRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo options %s: http: %w", underlyingSymbol, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("yahoo options %s: HTTP %d", underlyingSymbol, resp.StatusCode)
	}

	result, err := decodeJSON[yahooOptionsResp](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yahoo options %s: decode: %w", underlyingSymbol, err)
	}
	return result, nil
}

// newYahooRequest kreira GET zahtev sa browser-like User-Agent headerom.
func newYahooRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("yahoo: create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EXBanka/1.0)")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// decodeJSON čita i parsira JSON iz io.Reader.
func decodeJSON[T any](r io.Reader) (*T, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &result, nil
}
