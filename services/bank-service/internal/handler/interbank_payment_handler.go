// Package handler — interbank_payment_handler.go
//
// Klijentski endpoint-i nad interbank modulom (autentifikacija JWT):
//
//	POST /bank/interbank/payments         — kreiranje međubankarskog plaćanja
//	GET  /bank/interbank/public-stocks    — agregirana lista (lokalno + peer banka)
//	POST /bank/interbank/negotiations             — kupac inicira pregovaranje (forward to peer)
//	PUT  /bank/interbank/negotiations/{routing}/{id}   — kontraponuda
//	GET  /bank/interbank/negotiations/{routing}/{id}   — stanje
//	DELETE /bank/interbank/negotiations/{routing}/{id} — povlačenje
//	POST /bank/interbank/negotiations/{routing}/{id}/accept — accept
//	GET  /bank/interbank/contracts        — moji opcioni ugovori
//	POST /bank/interbank/contracts/{routing}/{id}/exercise — izvršavanje opcije
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
	"banka-backend/services/bank-service/internal/transport"
	auth "banka-backend/shared/auth"

	"github.com/shopspring/decimal"
)

// InterbankPaymentHandler — JWT-autentikovan handler za interbank operacije.
type InterbankPaymentHandler struct {
	coordinator      *service.TransactionCoordinator
	otcSvc           *service.InterbankOTCService
	optionSvc        *service.InterbankOptionContractService
	client           domain.InterbankClient
	repo             domain.InterbankRepository
	jwtSecret        string
	ourRoutingNumber int64
	userClient       *transport.UserServiceClient
}

// NewInterbankPaymentHandler konstruktor.
func NewInterbankPaymentHandler(
	coordinator *service.TransactionCoordinator,
	otc *service.InterbankOTCService,
	option *service.InterbankOptionContractService,
	client domain.InterbankClient,
	repo domain.InterbankRepository,
	jwtSecret string,
	ourRoutingNumber int64,
	userClient *transport.UserServiceClient,
) *InterbankPaymentHandler {
	return &InterbankPaymentHandler{
		coordinator:      coordinator,
		otcSvc:           otc,
		optionSvc:        option,
		client:           client,
		repo:             repo,
		jwtSecret:        jwtSecret,
		ourRoutingNumber: ourRoutingNumber,
		userClient:       userClient,
	}
}

// authenticate izvlači userID i tip korisnika iz JWT-a.
func (h *InterbankPaymentHandler) authenticate(r *http.Request) (int64, string, error) {
	hdr := r.Header.Get("Authorization")
	if !strings.HasPrefix(hdr, "Bearer ") {
		return 0, "", errors.New("no token")
	}
	claims, err := auth.VerifyToken(strings.TrimPrefix(hdr, "Bearer "), h.jwtSecret)
	if err != nil {
		return 0, "", err
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	return id, claims.UserType, err
}

func (h *InterbankPaymentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	userID, userType, err := h.authenticate(r)
	if err != nil {
		writeIBError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := r.URL.Path
	switch {
	case path == "/bank/interbank/payments" && r.Method == http.MethodPost:
		h.handleCreatePayment(w, r, userID)
	case path == "/bank/interbank/public-stocks" && r.Method == http.MethodGet:
		h.handlePublicStocks(w, r)
	case path == "/bank/interbank/negotiations" && r.Method == http.MethodGet:
		h.handleListNegotiations(w, r, userID, userType)
	case path == "/bank/interbank/negotiations" && r.Method == http.MethodPost:
		h.handleCreateNegotiation(w, r, userID)
	case path == "/bank/interbank/contracts" && r.Method == http.MethodGet:
		h.handleListContracts(w, r, userID)
	case strings.HasPrefix(path, "/bank/interbank/negotiations/"):
		h.handleNegotiationsSubpath(w, r, userID)
	case strings.HasPrefix(path, "/bank/interbank/contracts/"):
		h.handleContractsSubpath(w, r, userID)
	default:
		writeIBError(w, http.StatusNotFound, "not found")
	}
}

// ─── POST /bank/interbank/payments ───────────────────────────────────────────

type createInterbankPaymentReq struct {
	SenderAccountID    int64           `json:"senderAccountId"`
	RecipientAccountNo string          `json:"recipientAccountNumber"`
	RecipientName      string          `json:"recipientName"`
	Amount             decimal.Decimal `json:"amount"`
	Currency           string          `json:"currency"`
	PaymentCode        string          `json:"paymentCode"`
	PaymentPurpose     string          `json:"paymentPurpose"`
	CallNumber         string          `json:"callNumber"`
	Message            string          `json:"message"`
}

type createInterbankPaymentRes struct {
	InterbankTxID            int64  `json:"interbankTxId"`
	TransactionRoutingNumber int64  `json:"transactionRoutingNumber"`
	TransactionForeignID     string `json:"transactionForeignId"`
	Status                   string `json:"status"`
	FailureReason            string `json:"failureReason,omitempty"`
}

func (h *InterbankPaymentHandler) handleCreatePayment(w http.ResponseWriter, r *http.Request, userID int64) {
	var req createInterbankPaymentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	if req.RecipientAccountNo == "" || req.Amount.IsZero() || req.Currency == "" {
		writeIBError(w, http.StatusBadRequest, "račun primaoca, iznos i valuta su obavezni")
		return
	}
	in := domain.InterbankPaymentInput{
		SenderAccountID:    req.SenderAccountID,
		SenderUserID:       userID,
		RecipientAccountNo: req.RecipientAccountNo,
		RecipientName:      req.RecipientName,
		Amount:             req.Amount,
		Currency:           req.Currency,
		PaymentCode:        req.PaymentCode,
		PaymentPurpose:     req.PaymentPurpose,
		CallNumber:         req.CallNumber,
		Message:            req.Message,
	}
	ibTx, err := h.coordinator.InitiateInterbankPayment(r.Context(), in)
	if err != nil {
		// I dalje vraćamo telo sa informacijom o tx zapisu radi UI.
		status := http.StatusInternalServerError
		if errors.Is(err, domain.ErrInterbankPeerNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		var failure string
		var routing int64
		var fid string
		var ibStatus string
		if ibTx != nil {
			routing = ibTx.TransactionRoutingNumber
			fid = ibTx.TransactionForeignID
			ibStatus = string(ibTx.Status)
			failure = ibTx.FailureReason
		}
		writeIBJSON(w, status, createInterbankPaymentRes{
			TransactionRoutingNumber: routing,
			TransactionForeignID:     fid,
			Status:                   ibStatus,
			FailureReason:            failure,
		})
		_ = err // logged
		return
	}
	writeIBJSON(w, http.StatusOK, createInterbankPaymentRes{
		InterbankTxID:            ibTx.ID,
		TransactionRoutingNumber: ibTx.TransactionRoutingNumber,
		TransactionForeignID:     ibTx.TransactionForeignID,
		Status:                   string(ibTx.Status),
	})
}

// ─── GET /bank/interbank/public-stocks ───────────────────────────────────────

type publicStocksDTO struct {
	Local  []domain.PublicStock `json:"local"`
	Remote []domain.PublicStock `json:"remote"`
}

func (h *InterbankPaymentHandler) handlePublicStocks(w http.ResponseWriter, r *http.Request) {
	local, err := h.repo.ListPublicStocks(r.Context())
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range local {
		for j := range local[i].Sellers {
			local[i].Sellers[j].Seller.RoutingNumber = h.ourRoutingNumber
		}
	}
	remote, _ := h.client.GetPublicStock(r.Context()) // best-effort; ignore err
	if remote == nil {
		remote = []domain.PublicStock{}
	}
	writeIBJSON(w, http.StatusOK, publicStocksDTO{Local: local, Remote: remote})
}

// ─── /bank/interbank/negotiations ────────────────────────────────────────────

type createNegotiationReq struct {
	Stock           string          `json:"ticker"`
	SettlementDate  string          `json:"settlementDate"`
	PriceCurrency   string          `json:"priceCurrency"`
	PriceAmount     decimal.Decimal `json:"priceAmount"`
	PremiumCurrency string          `json:"premiumCurrency"`
	PremiumAmount   decimal.Decimal `json:"premiumAmount"`
	BuyerRouting    int64           `json:"buyerRoutingNumber"`
	BuyerID         string          `json:"buyerId"`
	SellerRouting   int64           `json:"sellerRoutingNumber"`
	SellerID        string          `json:"sellerId"`
	Amount          int32           `json:"amount"`
}

func (h *InterbankPaymentHandler) handleCreateNegotiation(w http.ResponseWriter, r *http.Request, userID int64) {
	var req createNegotiationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	if req.BuyerRouting == 0 {
		req.BuyerRouting = h.ourRoutingNumber
		req.BuyerID = strconv.FormatInt(userID, 10)
	}
	offer := domain.OtcOffer{
		Stock:          domain.StockDescription{Ticker: req.Stock},
		SettlementDate: req.SettlementDate,
		PricePerUnit:   domain.MonetaryValue{Currency: req.PriceCurrency, Amount: req.PriceAmount},
		Premium:        domain.MonetaryValue{Currency: req.PremiumCurrency, Amount: req.PremiumAmount},
		BuyerID:        domain.ForeignBankId{RoutingNumber: req.BuyerRouting, ID: req.BuyerID},
		SellerID:       domain.ForeignBankId{RoutingNumber: req.SellerRouting, ID: req.SellerID},
		Amount:         req.Amount,
		LastModifiedBy: domain.ForeignBankId{RoutingNumber: req.BuyerRouting, ID: req.BuyerID},
	}

	// Ako je prodavac na drugoj banci → prosledi.
	if req.SellerRouting != h.ourRoutingNumber {
		id, err := h.client.CreateNegotiation(r.Context(), offer)
		if err != nil {
			writeIBError(w, http.StatusBadGateway, err.Error())
			return
		}
		// Persistuj buyer-side mirror pod AUTORITATIVNIM {sellerRouting, sellerId}
		// koji nam je prodavčeva banka vratila — da bi naš inbound handler razrešio
		// njene counter/get/cancel/accept pozive (umesto 404).
		if id != nil {
			if err := h.otcSvc.RecordRemoteNegotiation(r.Context(), *id, offer); err != nil {
				writeIBError(w, http.StatusInternalServerError, "pregovor kreiran kod prodavca ali lokalno čuvanje nije uspelo: "+err.Error())
				return
			}
		}
		writeIBJSON(w, http.StatusOK, id)
		return
	}
	// Inače, lokalno smo banka prodavca.
	id, err := h.otcSvc.CreateNegotiation(r.Context(), offer)
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, id)
}

// ─── GET /bank/interbank/negotiations (dolazne ponude koje mi hostujemo) ──────

type negotiationListItemDTO struct {
	NegotiationID  domain.ForeignBankId `json:"negotiationId"`
	Ticker         string               `json:"ticker"`
	Amount         int32                `json:"amount"`
	PricePerUnit   domain.MonetaryValue `json:"pricePerUnit"`
	Premium        domain.MonetaryValue `json:"premium"`
	SettlementDate string               `json:"settlementDate"`
	Buyer          domain.ForeignBankId `json:"buyer"`
	Seller         domain.ForeignBankId `json:"seller"`
	Status         string               `json:"status"`
	IsOngoing      bool                 `json:"isOngoing"`
	LastModifiedBy domain.ForeignBankId `json:"lastModifiedBy"`
	MyTurn         bool                 `json:"myTurn"`
}

// handleListNegotiations vraća OTC pregovore koje smo mi banka prodavca (host).
// CLIENT vidi samo pregovore gde je on prodavac; AGENT/SUPERVISOR vide sve.
func (h *InterbankPaymentHandler) handleListNegotiations(w http.ResponseWriter, r *http.Request, userID int64, userType string) {
	var sellerID *string
	if userType == "CLIENT" {
		s := strconv.FormatInt(userID, 10)
		sellerID = &s
	}
	rows, err := h.otcSvc.ListNegotiations(r.Context(), sellerID)
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]negotiationListItemDTO, 0, len(rows))
	for i := range rows {
		n := &rows[i]
		out = append(out, negotiationListItemDTO{
			NegotiationID:  domain.ForeignBankId{RoutingNumber: n.NegotiationRoutingNumber, ID: n.NegotiationForeignID},
			Ticker:         n.StockTicker,
			Amount:         n.Amount,
			PricePerUnit:   domain.MonetaryValue{Currency: n.PriceCurrency, Amount: n.PriceAmount},
			Premium:        domain.MonetaryValue{Currency: n.PremiumCurrency, Amount: n.PremiumAmount},
			SettlementDate: n.SettlementDate.Format(time.RFC3339),
			Buyer:          domain.ForeignBankId{RoutingNumber: n.BuyerRoutingNumber, ID: n.BuyerID},
			Seller:         domain.ForeignBankId{RoutingNumber: n.SellerRoutingNumber, ID: n.SellerID},
			Status:         n.Status,
			IsOngoing:      n.IsOngoing,
			LastModifiedBy: domain.ForeignBankId{RoutingNumber: n.LastModifiedRoutingNumber, ID: n.LastModifiedID},
			MyTurn:         n.IsOngoing && n.LastModifiedRoutingNumber != h.ourRoutingNumber,
		})
	}
	writeIBJSON(w, http.StatusOK, out)
}

func (h *InterbankPaymentHandler) handleNegotiationsSubpath(w http.ResponseWriter, r *http.Request, userID int64) {
	rest := strings.TrimPrefix(r.URL.Path, "/bank/interbank/negotiations/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		writeIBError(w, http.StatusBadRequest, "očekuje /bank/interbank/negotiations/{routing}/{id}")
		return
	}
	routing, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan routingNumber")
		return
	}
	id := parts[1]
	if len(parts) == 3 && parts[2] == "accept" {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.handleAcceptNegotiation(w, r, routing, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetNegotiation(w, r, routing, id)
	case http.MethodPut:
		h.handleCounterNegotiation(w, r, routing, id, userID)
	case http.MethodDelete:
		h.handleCancelNegotiation(w, r, routing, id)
	default:
		writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *InterbankPaymentHandler) handleGetNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if routing == h.ourRoutingNumber {
		n, err := h.otcSvc.GetNegotiation(r.Context(), routing, id)
		if err != nil {
			if errors.Is(err, domain.ErrInterbankNotFound) {
				writeIBError(w, http.StatusNotFound, "not found")
				return
			}
			writeIBError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, n)
		return
	}
	// Forward ka peer banci
	n, err := h.client.GetNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id})
	if err != nil {
		if errors.Is(err, domain.ErrInterbankNotFound) {
			writeIBError(w, http.StatusNotFound, "not found")
			return
		}
		writeIBError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, n)
}

func (h *InterbankPaymentHandler) handleCounterNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string, userID int64) {
	var req createNegotiationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan JSON: "+err.Error())
		return
	}
	offer := domain.OtcOffer{
		Stock:          domain.StockDescription{Ticker: req.Stock},
		SettlementDate: req.SettlementDate,
		PricePerUnit:   domain.MonetaryValue{Currency: req.PriceCurrency, Amount: req.PriceAmount},
		Premium:        domain.MonetaryValue{Currency: req.PremiumCurrency, Amount: req.PremiumAmount},
		BuyerID:        domain.ForeignBankId{RoutingNumber: req.BuyerRouting, ID: req.BuyerID},
		SellerID:       domain.ForeignBankId{RoutingNumber: req.SellerRouting, ID: req.SellerID},
		Amount:         req.Amount,
		LastModifiedBy: domain.ForeignBankId{RoutingNumber: h.ourRoutingNumber, ID: strconv.FormatInt(userID, 10)},
	}
	if routing == h.ourRoutingNumber {
		if err := h.otcSvc.CounterNegotiation(r.Context(), routing, id, offer); err != nil {
			h.writeNegotiationErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.client.CounterNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}, offer); err != nil {
		h.writeNegotiationErr(w, err)
		return
	}
	// Ažuriraj buyer-side mirror (lastModifiedBy = mi) → ispravna turn-logika za
	// sledeću kontraponudu prodavčeve banke.
	if err := h.otcSvc.RecordRemoteNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}, offer); err != nil {
		writeIBError(w, http.StatusInternalServerError, "kontraponuda poslata ali lokalno ažuriranje nije uspelo: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankPaymentHandler) handleCancelNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if routing == h.ourRoutingNumber {
		if err := h.otcSvc.CancelNegotiation(r.Context(), routing, id); err != nil {
			h.writeNegotiationErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.client.CancelNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}); err != nil {
		h.writeNegotiationErr(w, err)
		return
	}
	// Best-effort: zatvori i buyer-side mirror.
	if err := h.otcSvc.CancelNegotiation(r.Context(), routing, id); err != nil && !errors.Is(err, domain.ErrInterbankNotFound) {
		writeIBError(w, http.StatusInternalServerError, "otkazano kod prodavca ali lokalno ažuriranje nije uspelo: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankPaymentHandler) handleAcceptNegotiation(w http.ResponseWriter, r *http.Request, routing int64, id string) {
	if routing == h.ourRoutingNumber {
		if err := h.optionSvc.AcceptNegotiation(r.Context(), routing, id); err != nil {
			h.writeNegotiationErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.client.AcceptNegotiation(r.Context(), domain.ForeignBankId{RoutingNumber: routing, ID: id}); err != nil {
		h.writeNegotiationErr(w, err)
		return
	}
	// Strani prodavac je finalizovao (premija stiže preko dolaznog 2PC) → upiši
	// lokalni ugovor na našoj (kupčevoj) strani da kupac može kasnije da ga iskoristi.
	if err := h.optionSvc.AcceptNegotiation(r.Context(), routing, id); err != nil {
		writeIBError(w, http.StatusInternalServerError, "prihvaćeno kod prodavca ali lokalni ugovor nije upisan: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InterbankPaymentHandler) writeNegotiationErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInterbankNotFound):
		writeIBError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrInterbankConflict):
		writeIBError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrInterbankPeerNotConfigured):
		writeIBError(w, http.StatusServiceUnavailable, "peer bank URL nije konfigurisan")
	default:
		writeIBError(w, http.StatusInternalServerError, err.Error())
	}
}

// ─── /bank/interbank/contracts ───────────────────────────────────────────────

func (h *InterbankPaymentHandler) handleListContracts(w http.ResponseWriter, r *http.Request, userID int64) {
	rows, err := h.optionSvc.ListContracts(r.Context(), userID)
	if err != nil {
		writeIBError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeIBJSON(w, http.StatusOK, rows)
}

func (h *InterbankPaymentHandler) handleContractsSubpath(w http.ResponseWriter, r *http.Request, userID int64) {
	rest := strings.TrimPrefix(r.URL.Path, "/bank/interbank/contracts/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		writeIBError(w, http.StatusBadRequest, "očekuje /bank/interbank/contracts/{routing}/{id}[/exercise]")
		return
	}
	routing, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeIBError(w, http.StatusBadRequest, "nevalidan routingNumber")
		return
	}
	id := parts[1]

	if len(parts) == 3 && parts[2] == "exercise" {
		if r.Method != http.MethodPost {
			writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ibTx, err := h.optionSvc.ExerciseContract(r.Context(), userID, routing, id)
		if err != nil {
			writeIBError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, map[string]interface{}{
			"interbankTxId": ibTx.ID,
			"status":        ibTx.Status,
		})
		return
	}
	if r.Method == http.MethodGet {
		c, err := h.optionSvc.GetContract(r.Context(), routing, id)
		if err != nil {
			if errors.Is(err, domain.ErrInterbankNotFound) {
				writeIBError(w, http.StatusNotFound, "not found")
				return
			}
			writeIBError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeIBJSON(w, http.StatusOK, c)
		return
	}
	writeIBError(w, http.StatusMethodNotAllowed, "method not allowed")
	_ = fmt.Sprintf // unused
}
