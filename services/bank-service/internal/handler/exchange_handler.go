// exchange_handler.go — HTTP handler za kursnu listu, konverziju i izvršenje transfera.
//
// Endpoints (plain HTTP, not gRPC — registered directly on http.ServeMux):
//   GET  /bank/exchange-rates                          → kursna lista
//   GET  /bank/exchange-rates?from=X&to=Y&amount=Z    → konverzija (informativno)
//   POST /bank/exchange-rates/execute                  → izvršenje konverzije između računa
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	auth "banka-backend/shared/auth"
	"banka-backend/services/bank-service/internal/domain"
)

// ExchangeRateHandler serves exchange rate endpoints over plain HTTP.
type ExchangeRateHandler struct {
	exchangeService domain.ExchangeService
	jwtSecret       string
}

// NewExchangeRateHandler creates a new ExchangeRateHandler.
func NewExchangeRateHandler(exchangeService domain.ExchangeService, jwtSecret string) *ExchangeRateHandler {
	return &ExchangeRateHandler{
		exchangeService: exchangeService,
		jwtSecret:       jwtSecret,
	}
}

// ServeHTTP routes requests based on path suffix and HTTP method:
//   GET  …/exchange-rates          → rates list or conversion
//   POST …/exchange-rates/execute  → execute transfer
func (h *ExchangeRateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Common JWT validation — required for all exchange endpoints.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		exchangeWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(authHeader, "Bearer "), h.jwtSecret)
	if err != nil {
		exchangeWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	path := strings.TrimRight(r.URL.Path, "/")
	isExecute := strings.HasSuffix(path, "/execute")

	switch {
	case isExecute && r.Method == http.MethodPost:
		h.handleExecute(w, r, claims)
	case !isExecute && r.Method == http.MethodGet:
		q := r.URL.Query()
		from, to, amountStr := q.Get("from"), q.Get("to"), q.Get("amount")
		if from != "" && to != "" && amountStr != "" {
			h.handleConvert(w, r, from, to, amountStr)
		} else {
			h.handleRatesList(w, r)
		}
	default:
		exchangeWriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// ─── Kursna lista ─────────────────────────────────────────────────────────────

type exchangeRateJSON struct {
	Oznaka   string  `json:"oznaka"`
	Naziv    string  `json:"naziv"`
	Kupovni  float64 `json:"kupovni"`
	Srednji  float64 `json:"srednji"`
	Prodajni float64 `json:"prodajni"`
}

type ratesListJSON struct {
	Rates []exchangeRateJSON `json:"rates"`
}

func (h *ExchangeRateHandler) handleRatesList(w http.ResponseWriter, r *http.Request) {
	rates, err := h.exchangeService.GetRates(r.Context())
	if err != nil {
		exchangeWriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kursna lista nije dostupna"})
		return
	}

	items := make([]exchangeRateJSON, 0, len(rates))
	for _, rate := range rates {
		items = append(items, exchangeRateJSON{
			Oznaka:   rate.Oznaka,
			Naziv:    rate.Naziv,
			Kupovni:  rate.Kupovni,
			Srednji:  rate.Srednji,
			Prodajni: rate.Prodajni,
		})
	}
	exchangeWriteJSON(w, http.StatusOK, ratesListJSON{Rates: items})
}

// ─── Konverzija (informativna) ────────────────────────────────────────────────

type convertJSON struct {
	Result    float64 `json:"result"`
	Bruto     float64 `json:"bruto"`
	Provizija float64 `json:"provizija"`
	ViaRSD    bool    `json:"viaRsd"`
	RateNote  string  `json:"rateNote"`
}

func (h *ExchangeRateHandler) handleConvert(w http.ResponseWriter, r *http.Request, from, to, amountStr string) {
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "neispravan iznos"})
		return
	}

	result, err := h.exchangeService.CalculateExchange(r.Context(), from, to, amount)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrExchangeInvalidAmount):
			exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeRateNotFound):
			exchangeWriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		default:
			exchangeWriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "konverzija trenutno nije dostupna"})
		}
		return
	}

	exchangeWriteJSON(w, http.StatusOK, convertJSON{
		Result:    result.Result,
		Bruto:     result.Bruto,
		Provizija: result.Provizija,
		ViaRSD:    result.ViaRSD,
		RateNote:  result.RateNote,
	})
}

// ─── Izvršenje konverzije ─────────────────────────────────────────────────────

type executeTransferRequest struct {
	SourceAccountID int64   `json:"sourceAccountId"`
	TargetAccountID int64   `json:"targetAccountId"`
	FromOznaka      string  `json:"fromOznaka"`
	ToOznaka        string  `json:"toOznaka"`
	Amount          float64 `json:"amount"`
}

type executeTransferResponse struct {
	ReferenceID     string  `json:"referenceId"`
	SourceAccountID int64   `json:"sourceAccountId"`
	TargetAccountID int64   `json:"targetAccountId"`
	FromOznaka      string  `json:"fromOznaka"`
	ToOznaka        string  `json:"toOznaka"`
	OriginalAmount  float64 `json:"originalAmount"`
	GrossAmount     float64 `json:"grossAmount"`
	Provizija       float64 `json:"provizija"`
	NetAmount       float64 `json:"netAmount"`
	ViaRSD          bool    `json:"viaRsd"`
	RateNote        string  `json:"rateNote"`
}

func (h *ExchangeRateHandler) handleExecute(w http.ResponseWriter, r *http.Request, claims *auth.AccessClaims) {
	var req executeTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "neispravno telo zahteva"})
		return
	}

	vlasnikID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		exchangeWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "neispravan korisnički ID u tokenu"})
		return
	}

	input := domain.ExchangeTransferInput{
		VlasnikID:       vlasnikID,
		SourceAccountID: req.SourceAccountID,
		TargetAccountID: req.TargetAccountID,
		FromOznaka:      req.FromOznaka,
		ToOznaka:        req.ToOznaka,
		Amount:          req.Amount,
	}

	result, err := h.exchangeService.ExecuteExchangeTransfer(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrExchangeInvalidAmount):
			exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeSameCurrency):
			exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeSameAccount):
			exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeWrongCurrency):
			exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeAccountInactive):
			exchangeWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeAccountNotOwned):
			exchangeWriteJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrExchangeInsufficientFunds):
			exchangeWriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		case errors.Is(err, domain.ErrAccountNotFound):
			exchangeWriteJSON(w, http.StatusNotFound, map[string]string{"error": "račun nije pronađen"})
		case errors.Is(err, domain.ErrExchangeRateNotFound):
			exchangeWriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		default:
			exchangeWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "greška pri izvršenju konverzije"})
		}
		return
	}

	exchangeWriteJSON(w, http.StatusOK, executeTransferResponse{
		ReferenceID:     result.ReferenceID,
		SourceAccountID: result.SourceAccountID,
		TargetAccountID: result.TargetAccountID,
		FromOznaka:      result.FromOznaka,
		ToOznaka:        result.ToOznaka,
		OriginalAmount:  result.OriginalAmount,
		GrossAmount:     result.GrossAmount,
		Provizija:       result.Provizija,
		NetAmount:       result.NetAmount,
		ViaRSD:          result.ViaRSD,
		RateNote:        result.RateNote,
	})
}

// exchangeWriteJSON writes a JSON response with the given status code.
func exchangeWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
