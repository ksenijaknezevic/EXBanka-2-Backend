package worker

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const avBaseURL = "https://www.alphavantage.co/query"

// alphaVantageClient je HTTP klijent za Alpha Vantage API.
type alphaVantageClient struct {
	apiKey     string
	httpClient *http.Client
}

func newAlphaVantageClient(apiKey string) *alphaVantageClient {
	return &alphaVantageClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ─── Company Overview (STOCK) ─────────────────────────────────────────────────

// avCompanyOverview mapira relevantna polja iz /query?function=OVERVIEW odgovora.
type avCompanyOverview struct {
	SharesOutstanding string `json:"SharesOutstanding"`
	DividendYield     string `json:"DividendYield"`
}

// CompanyOverview dohvata pregled kompanije za dati ticker.
func (c *alphaVantageClient) CompanyOverview(ctx context.Context, symbol string) (*avCompanyOverview, error) {
	url := fmt.Sprintf("%s?function=OVERVIEW&symbol=%s&apikey=%s", avBaseURL, symbol, c.apiKey)
	result, err := doGet[avCompanyOverview](ctx, c.httpClient, url)
	if err != nil {
		return nil, fmt.Errorf("av company overview %s: %w", symbol, err)
	}
	return result, nil
}

// ─── Currency Exchange Rate (FOREX) ──────────────────────────────────────────

// avForexInner je unutrašnji objekat u AV odgovoru za kurseve.
type avForexInner struct {
	ExchangeRate string `json:"5. Exchange Rate"`
	BidPrice     string `json:"8. Bid Price"`
	AskPrice     string `json:"9. Ask Price"`
}

// avForexResponse je ceo JSON odgovor za CURRENCY_EXCHANGE_RATE.
type avForexResponse struct {
	Data avForexInner `json:"Realtime Currency Exchange Rate"`
}

// avForexRate su parsirane float64 vrednosti iz avForexResponse.
type avForexRate struct {
	ExchangeRate float64
	BidPrice     float64
	AskPrice     float64
}

// ForexRate dohvata kurs između dve valute.
func (c *alphaVantageClient) ForexRate(ctx context.Context, fromCurrency, toCurrency string) (*avForexRate, error) {
	url := fmt.Sprintf(
		"%s?function=CURRENCY_EXCHANGE_RATE&from_currency=%s&to_currency=%s&apikey=%s",
		avBaseURL, fromCurrency, toCurrency, c.apiKey,
	)
	raw, err := doGet[avForexResponse](ctx, c.httpClient, url)
	if err != nil {
		return nil, fmt.Errorf("av forex rate %s/%s: %w", fromCurrency, toCurrency, err)
	}

	rate, err := parseAVFloat(raw.Data.ExchangeRate)
	if err != nil || rate == 0 {
		return nil, fmt.Errorf("av forex rate %s/%s: neispravan kurs %q", fromCurrency, toCurrency, raw.Data.ExchangeRate)
	}

	bid, _ := parseAVFloat(raw.Data.BidPrice)
	ask, _ := parseAVFloat(raw.Data.AskPrice)

	// Ako AV ne vrati bid/ask, koristimo spread od 0.05%
	if bid == 0 {
		bid = rate * 0.9995
	}
	if ask == 0 {
		ask = rate * 1.0005
	}

	return &avForexRate{
		ExchangeRate: rate,
		BidPrice:     bid,
		AskPrice:     ask,
	}, nil
}

// ─── Helper ───────────────────────────────────────────────────────────────────

// parseAVFloat parsira string u float64 (AV vraća brojeve kao stringove).
func parseAVFloat(s string) (float64, error) {
	if s == "" || s == "None" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
