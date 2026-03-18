package handler

// exchange_handler.go — direktni HTTP handler za konverziju valuta između računa.
// Endpoint: POST /bank/client/exchange-transfers
//
// Registrovan direktno na http.ServeMux (pored gRPC-Gateway) jer proto schema
// ne sadrži polje convertedIznos potrebno za cross-currency prenos.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	auth "banka-backend/shared/auth"
	"banka-backend/services/bank-service/internal/domain"
)

// ExchangeTransferHandler kreira nalog konverzije valuta između dva računa istog korisnika.
type ExchangeTransferHandler struct {
	paymentService domain.PaymentService
	jwtSecret      string
}

// NewExchangeTransferHandler kreira novi handler za konverziju valuta.
func NewExchangeTransferHandler(paymentService domain.PaymentService, jwtSecret string) *ExchangeTransferHandler {
	return &ExchangeTransferHandler{
		paymentService: paymentService,
		jwtSecret:      jwtSecret,
	}
}

type exchangeTransferRequest struct {
	IdempotencyKey   string  `json:"idempotencyKey"`
	SourceAccountId  int64   `json:"sourceAccountId"`
	TargetAccountId  int64   `json:"targetAccountId"`
	Amount           float64 `json:"amount"`           // iznos koji se skida sa izvornog računa
	ConvertedAmount  float64 `json:"convertedAmount"`  // iznos koji se upisuje na ciljni račun
	SvrhaPlacanja    string  `json:"svrhaPlacanja"`
}

type exchangeTransferResponse struct {
	IntentId   int64  `json:"intentId"`
	ActionId   int64  `json:"actionId"`
	BrojNaloga string `json:"brojNaloga"`
	Status     string `json:"status"`
}

// ServeHTTP obrađuje POST /bank/client/exchange-transfers zahteve.
func (h *ExchangeTransferHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Dozvoli samo POST metodu.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Postavi CORS header-e (isti origin kao i gRPC-Gateway).
	w.Header().Set("Content-Type", "application/json")

	// Validacija JWT tokena iz Authorization headera.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	claims, err := auth.VerifyToken(token, h.jwtSecret)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if claims.UserType != "CLIENT" {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Dekodiraj telo zahteva.
	var req exchangeTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if req.IdempotencyKey == "" {
		writeJSONError(w, http.StatusBadRequest, "idempotencyKey je obavezan")
		return
	}
	if req.Amount <= 0 {
		writeJSONError(w, http.StatusBadRequest, "amount mora biti veći od 0")
		return
	}
	if req.ConvertedAmount <= 0 {
		writeJSONError(w, http.StatusBadRequest, "convertedAmount mora biti veći od 0")
		return
	}
	if req.SourceAccountId == req.TargetAccountId {
		writeJSONError(w, http.StatusBadRequest, "izvorni i ciljni račun moraju biti različiti")
		return
	}

	svrha := req.SvrhaPlacanja
	if svrha == "" {
		svrha = "Konverzija valuta"
	}

	input := domain.CreateTransferIntentInput{
		IdempotencyKey:    req.IdempotencyKey,
		RacunPlatioceID:   req.SourceAccountId,
		RacunPrimaocaID:   req.TargetAccountId,
		Iznos:             req.Amount,
		ConvertedIznos:    req.ConvertedAmount,
		SvrhaPlacanja:     svrha,
		InitiatedByUserID: userID,
	}

	intent, actionID, err := h.paymentService.CreateTransferIntent(r.Context(), input)
	if err != nil {
		status := http.StatusInternalServerError
		msg := err.Error()
		switch {
		case errors.Is(err, domain.ErrSameAccount),
			errors.Is(err, domain.ErrAccountNotOwned),
			errors.Is(err, domain.ErrRecipientAccountInvalid),
			errors.Is(err, domain.ErrInsufficientFunds),
			errors.Is(err, domain.ErrDailyLimitExceeded),
			errors.Is(err, domain.ErrMonthlyLimitExceeded):
			status = http.StatusBadRequest
		}
		writeJSONError(w, status, msg)
		return
	}

	resp := exchangeTransferResponse{
		IntentId:   intent.ID,
		ActionId:   actionID,
		BrojNaloga: intent.BrojNaloga,
		Status:     intent.Status,
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": msg})
}
