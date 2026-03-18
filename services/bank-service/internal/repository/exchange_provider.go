// Package repository — ExchangeRate-API HTTP provider for live currency mid-rates.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// exchangeRateAPIResponse is the JSON response shape from ExchangeRate-API v6.
//
// Example endpoint: GET https://v6.exchangerate-api.com/v6/{KEY}/latest/USD
type exchangeRateAPIResponse struct {
	Result          string             `json:"result"`           // "success" | "error"
	ConversionRates map[string]float64 `json:"conversion_rates"` // target code → rate from base
}

// ExchangeRateProvider fetches live mid rates from ExchangeRate-API v6.
// It uses USD as the base currency (available on all plan tiers) and derives
// RSD-per-unit values for every supported currency.
type ExchangeRateProvider struct {
	apiKey  string
	baseURL string // e.g. "https://v6.exchangerate-api.com/v6"
	client  *http.Client
}

// NewExchangeRateProvider creates a new provider.
// baseURL should not have a trailing slash.
func NewExchangeRateProvider(apiKey, baseURL string) *ExchangeRateProvider {
	return &ExchangeRateProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// GetMidRates calls ExchangeRate-API with USD as base and converts the rates to
// "how many RSD equals 1 unit of currency X":
//
//	midRate(X) = conversionRates["RSD"] / conversionRates["X"]
//
// Returns domain.ErrExchangeProviderUnavailable on any network/parse error.
func (p *ExchangeRateProvider) GetMidRates(ctx context.Context) (map[string]float64, error) {
	url := fmt.Sprintf("%s/%s/latest/USD", p.baseURL, p.apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, domain.ErrExchangeProviderUnavailable
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, domain.ErrExchangeProviderUnavailable
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, domain.ErrExchangeProviderUnavailable
	}

	var apiResp exchangeRateAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, domain.ErrExchangeProviderUnavailable
	}
	if apiResp.Result != "success" {
		return nil, domain.ErrExchangeProviderUnavailable
	}

	// USD→RSD gives us the RSD price of 1 USD.
	rsdPerUSD, ok := apiResp.ConversionRates["RSD"]
	if !ok || rsdPerUSD == 0 {
		return nil, domain.ErrExchangeProviderUnavailable
	}

	// For each supported currency, derive: 1 X = (USD→RSD) / (USD→X) RSD.
	result := make(map[string]float64, len(domain.SupportedExchangeCodes))
	for _, code := range domain.SupportedExchangeCodes {
		usdToX, exists := apiResp.ConversionRates[code]
		if !exists || usdToX == 0 {
			continue
		}
		result[code] = rsdPerUSD / usdToX
	}

	return result, nil
}
