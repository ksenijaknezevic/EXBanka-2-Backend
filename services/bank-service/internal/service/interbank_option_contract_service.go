// Package service — interbank_option_contract_service.go
//
// Servis za prihvatanje OTC ponude i izvršavanje opcionog ugovora preko
// si-tx-proto transaction protocol-a.
//
// Accept tok (po protokolu):
//  1. Buyer credit premium     ← prema Posting protokolu
//  2. Seller debit premium
//  3. Buyer debit optionContract
//  4. Seller credit optionContract
//
// Exercise tok:
//   - debit OPTION account za pricePerUnit * amount novca
//   - credit buyer za pricePerUnit * amount novca
//   - credit OPTION account za amount stocks
//   - debit buyer za amount stocks
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
)

// acceptCoordinator — zavisnosti za accept/exercise: 2PC poravnanje i escrow akcija.
// Zadovoljava ga *TransactionCoordinator; izdvojeno kao interfejs radi testiranja.
type acceptCoordinator interface {
	InitiateInterbankTransaction(ctx context.Context, tx domain.Transaction, initiatorUserID *int64) (*domain.InterbankTransaction, error)
	BlockShares(ctx context.Context, userID int64, ticker string, amount int32) error
	ReleaseShares(ctx context.Context, userID int64, ticker string, amount int32) error
}

// InterbankOptionContractService — kreiranje i izvršavanje opcionih ugovora.
type InterbankOptionContractService struct {
	repo             domain.InterbankRepository
	coordinator      acceptCoordinator
	ourRoutingNumber int64
}

// NewInterbankOptionContractService konstruktor.
func NewInterbankOptionContractService(
	repo domain.InterbankRepository,
	coordinator acceptCoordinator,
	ourRoutingNumber int64,
) *InterbankOptionContractService {
	return &InterbankOptionContractService{
		repo:             repo,
		coordinator:      coordinator,
		ourRoutingNumber: ourRoutingNumber,
	}
}

// AcceptNegotiation — GET /negotiations/{routing}/{id}/accept.
//
// Izvršava je banka prodavca (ona hostuje pregovaranje). U tom trenutku, atomično
// koliko je moguće:
//  1. blokira prodavčeve akcije (escrow) — da ih ne može dvostruko prodati,
//  2. sinhrono naplaćuje premiju kupac → prodavac preko 2PC (si-tx-proto),
//  3. tek ako je premija naplaćena, kreira opcioni ugovor (ACTIVE) i zatvara
//     pregovaranje.
//
// Redosled (blokada pre premije) garantuje da premiju nikad ne naplatimo ako
// prodavac nema akcije; ako premija padne, blokada se kompenzuje (vraćanje akcija).
// Idempotentno: već prihvaćeno pregovaranje vraća OK bez efekata.
func (s *InterbankOptionContractService) AcceptNegotiation(ctx context.Context, routing int64, id string) error {
	n, err := s.repo.GetNegotiationByID(ctx, routing, id)
	if err != nil {
		return err
	}
	if n == nil {
		return domain.ErrInterbankNotFound
	}
	if n.Status == "ACCEPTED" {
		return nil
	}
	if !n.IsOngoing {
		return domain.ErrInterbankConflict
	}

	// Buyer-side: ako prodavac nije na našoj banci, strani prodavac je autoritativan i
	// poravnava premiju + blokira svoje akcije; premija nama stiže preko dolaznog 2PC.
	// Mi samo upišemo lokalni ugovor da bi naš kupac kasnije mogao da ga iskoristi.
	if n.SellerRoutingNumber != s.ourRoutingNumber {
		return s.finalizeAccept(ctx, n)
	}

	sellerUserID, err := strconv.ParseInt(n.SellerID, 10, 64)
	if err != nil {
		return fmt.Errorf("nevalidan sellerId %q: %w", n.SellerID, err)
	}

	// 1) Blokiraj prodavčeve akcije (escrow). Nedovoljno akcija → odbij pre premije.
	if err := s.coordinator.BlockShares(ctx, sellerUserID, n.StockTicker, n.Amount); err != nil {
		return fmt.Errorf("blokada akcija prodavca: %w", err)
	}

	// 2) Naplati premiju sinhrono preko 2PC (kupac PERSON −premium / prodavac PERSON +premium).
	ibTx, err := s.coordinator.InitiateInterbankTransaction(ctx, s.buildAcceptTransaction(n), &sellerUserID)
	committed := err == nil && ibTx != nil && ibTx.Status == domain.TxStatusCommitted
	if !committed {
		// Premija nije naplaćena → kompenzuj blokadu, pregovaranje ostaje otvoreno.
		if relErr := s.coordinator.ReleaseShares(ctx, sellerUserID, n.StockTicker, n.Amount); relErr != nil {
			return fmt.Errorf("naplata premije nije uspela; kompenzacija blokade takođe (RECONCILE): premija=%v release=%w", err, relErr)
		}
		if err != nil {
			// Prevedi najčešći NoVoteReason u jasnu, korisničku poruku (FE je prikazuje).
			if strings.Contains(err.Error(), string(domain.NoReasonInsufficientAsset)) {
				return fmt.Errorf("kupac nema dovoljno sredstava za premiju")
			}
			return fmt.Errorf("druga banka je odbila naplatu premije: %w", err)
		}
		return fmt.Errorf("naplata premije nije izvršena (premija nije naplaćena)")
	}

	// 3) Premija prošla + akcije blokirane → kreiraj ugovor (ACTIVE) i zatvori pregovaranje.
	if err := s.repo.CreateOptionContract(ctx, contractFromNegotiation(n)); err != nil {
		// Kritično: premija naplaćena + akcije blokirane, ali ugovor nije upisan.
		return fmt.Errorf("premija naplaćena ali kreiranje ugovora nije uspelo (potrebna reconciliacija): %w", err)
	}
	n.IsOngoing = false
	n.Status = "ACCEPTED"
	if err := s.repo.UpdateNegotiation(ctx, n); err != nil {
		return fmt.Errorf("zatvaranje pregovaranja: %w", err)
	}
	return nil
}

// finalizeAccept — buyer-side: upiše lokalni ugovor (ACTIVE) i zatvori pregovaranje.
// Bez blokade akcija i premije lokalno — to obavlja strani (autoritativni) prodavac.
// Idempotentno preko provere statusa u AcceptNegotiation.
func (s *InterbankOptionContractService) finalizeAccept(ctx context.Context, n *domain.InterbankNegotiation) error {
	if err := s.repo.CreateOptionContract(ctx, contractFromNegotiation(n)); err != nil {
		return fmt.Errorf("kreiranje lokalnog opcionog ugovora: %w", err)
	}
	n.IsOngoing = false
	n.Status = "ACCEPTED"
	if err := s.repo.UpdateNegotiation(ctx, n); err != nil {
		return fmt.Errorf("zatvaranje pregovaranja: %w", err)
	}
	return nil
}

// contractFromNegotiation — ugovor (ACTIVE) iz prihvaćenog pregovaranja.
func contractFromNegotiation(n *domain.InterbankNegotiation) *domain.InterbankOptionContract {
	return &domain.InterbankOptionContract{
		NegotiationRoutingNumber: n.NegotiationRoutingNumber,
		NegotiationForeignID:     n.NegotiationForeignID,
		StockTicker:              n.StockTicker,
		PriceCurrency:            n.PriceCurrency,
		PriceAmount:              n.PriceAmount,
		PremiumCurrency:          n.PremiumCurrency,
		PremiumAmount:            n.PremiumAmount,
		SettlementDate:           n.SettlementDate,
		Amount:                   n.Amount,
		BuyerRoutingNumber:       n.BuyerRoutingNumber,
		BuyerID:                  n.BuyerID,
		SellerRoutingNumber:      n.SellerRoutingNumber,
		SellerID:                 n.SellerID,
		Status:                   "ACTIVE",
	}
}

// buildAcceptTransaction — balansirana accept transakcija po si-tx-proto §3.6:
//
//	premium par (kupac PERSON −premium / prodavac PERSON +premium) +
//	option-issuance par (OPTION{naš,negId} −1 / kupac PERSON +1, asset OPTION).
//
// Bez option-legova premium nije vezan za OptionAsset → peer gate odbija sa
// UNACCEPTABLE_ASSET (anti-fraud). Ugovor se aktivira (ACTIVE) na 2PC commit.
func (s *InterbankOptionContractService) buildAcceptTransaction(n *domain.InterbankNegotiation) domain.Transaction {
	buyer := &domain.ForeignBankId{RoutingNumber: n.BuyerRoutingNumber, ID: n.BuyerID}
	seller := &domain.ForeignBankId{RoutingNumber: n.SellerRoutingNumber, ID: n.SellerID}
	premiumAsset := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: n.PremiumCurrency}}

	optID := domain.ForeignBankId{RoutingNumber: s.ourRoutingNumber, ID: n.NegotiationForeignID}
	optionAsset := domain.Asset{Type: domain.AssetTypeOption, Option: &domain.OptionDescription{
		NegotiationID:  optID,
		Stock:          domain.StockDescription{Ticker: n.StockTicker},
		PricePerUnit:   domain.MonetaryValue{Currency: n.PriceCurrency, Amount: n.PriceAmount},
		SettlementDate: n.SettlementDate.UTC().Format(time.RFC3339),
		Amount:         n.Amount,
	}}
	one := decimal.NewFromInt(1)

	return domain.Transaction{
		Postings: []domain.Posting{
			// premium par
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: n.PremiumAmount.Neg(), Asset: premiumAsset},
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: seller}, Amount: n.PremiumAmount, Asset: premiumAsset},
			// option-issuance par (§3.6): izdavanje opcije vezuje premiju za OptionAsset
			{Account: domain.TxAccount{Type: domain.AccountKindOption, ID: &optID}, Amount: one.Neg(), Asset: optionAsset},
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: one, Asset: optionAsset},
		},
		TransactionID:  domain.ForeignBankId{RoutingNumber: s.ourRoutingNumber, ID: NewLocalKey()},
		Message:        "OTC accept " + n.NegotiationForeignID,
		PaymentCode:    "289",
		PaymentPurpose: "OTC_PREMIUM",
	}
}

// ListContracts — vraća sve aktivne (i istorijske) ugovore za korisnika.
func (s *InterbankOptionContractService) ListContracts(ctx context.Context, userID int64) ([]domain.InterbankOptionContract, error) {
	return s.repo.ListContractsForUser(ctx, s.ourRoutingNumber, strconv.FormatInt(userID, 10))
}

// GetContract — jedan ugovor.
func (s *InterbankOptionContractService) GetContract(ctx context.Context, routing int64, id string) (*domain.InterbankOptionContract, error) {
	c, err := s.repo.GetOptionContract(ctx, routing, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, domain.ErrInterbankNotFound
	}
	return c, nil
}

// ExerciseContract — kupac koristi opciju. Konstruiše Transaction po
// protokolu i pokreće 2-phase commit.
func (s *InterbankOptionContractService) ExerciseContract(ctx context.Context, callerUserID int64, routing int64, id string) (*domain.InterbankTransaction, error) {
	c, err := s.repo.GetOptionContract(ctx, routing, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, domain.ErrInterbankNotFound
	}
	if c.BuyerRoutingNumber != s.ourRoutingNumber {
		return nil, fmt.Errorf("kupac nije na ovoj banci")
	}
	if c.BuyerID != strconv.FormatInt(callerUserID, 10) {
		return nil, fmt.Errorf("samo kupac može iskoristiti ugovor")
	}
	if c.Status != "ACTIVE" {
		return nil, fmt.Errorf("opcija nije ACTIVE: %s", c.Status)
	}
	if !c.SettlementDate.After(time.Now().UTC()) {
		return nil, fmt.Errorf("settlementDate je prošao")
	}

	// Konstruiši Transaction — CALL opcija: kupac kupuje akcije po strike ceni.
	//   buyer  PERSON  −cash, +akcije   (plaća strike × količina, prima akcije)
	//   OPTION (negotiation) +cash, −akcije (prodavac/escrow prima novac, isporučuje akcije)
	// Balansirano po asset-u (cash i stock zbir = 0).
	totalCash := c.PriceAmount.Mul(decimal.NewFromInt(int64(c.Amount)))
	amountStock := decimal.NewFromInt(int64(c.Amount))

	optAccount := &domain.ForeignBankId{
		RoutingNumber: c.NegotiationRoutingNumber,
		ID:            c.NegotiationForeignID,
	}
	buyerPerson := &domain.ForeignBankId{
		RoutingNumber: c.BuyerRoutingNumber,
		ID:            c.BuyerID,
	}

	cashAsset := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: c.PriceCurrency}}
	stockAsset := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: c.StockTicker}}

	postings := []domain.Posting{
		{
			Account: domain.TxAccount{Type: domain.AccountKindOption, ID: optAccount},
			Amount:  totalCash, // OPTION/prodavac prima novac
			Asset:   cashAsset,
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyerPerson},
			Amount:  totalCash.Neg(), // kupac plaća novac
			Asset:   cashAsset,
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindOption, ID: optAccount},
			Amount:  amountStock.Neg(), // OPTION/prodavac isporučuje akcije
			Asset:   stockAsset,
		},
		{
			Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyerPerson},
			Amount:  amountStock, // kupac prima akcije
			Asset:   stockAsset,
		},
	}

	transaction := domain.Transaction{
		Postings: postings,
		TransactionID: domain.ForeignBankId{
			RoutingNumber: s.ourRoutingNumber,
			ID:            NewLocalKey(),
		},
		Message:        "Exercise OTC option " + c.NegotiationForeignID,
		PaymentCode:    "289",
		PaymentPurpose: "OTC_EXERCISE",
	}

	// Iskoristi coordinator infrastrukturu (2PC, analogno InitiateInterbankPayment).
	ibTx, err := s.coordinator.InitiateInterbankTransaction(ctx, transaction, &callerUserID)
	if err != nil {
		return ibTx, friendlyOTCError(err)
	}
	// Na uspešan 2PC, označi ugovor iskorišćenim.
	if ibTx != nil && ibTx.Status == domain.TxStatusCommitted {
		now := time.Now().UTC()
		if uerr := s.repo.UpdateOptionContractStatus(ctx, routing, id, "EXERCISED", &now); uerr != nil {
			return ibTx, fmt.Errorf("opcija iskorišćena ali status nije upisan (reconcile): %w", uerr)
		}
	}
	return ibTx, nil
}

// friendlyOTCError prevodi NoVoteReason kodove (iz 2PC odbijanja) u jasne
// korisničke poruke za exercise; nepoznate greške vraća nepromenjene.
func friendlyOTCError(err error) error {
	if err == nil {
		return nil
	}
	m := err.Error()
	switch {
	case strings.Contains(m, string(domain.NoReasonNoSuchAccount)):
		return fmt.Errorf("nemate odgovarajući račun za plaćanje opcije (strike)")
	case strings.Contains(m, string(domain.NoReasonOptionUsedOrExpired)):
		return fmt.Errorf("opcija je već iskorišćena ili je rok istekao")
	case strings.Contains(m, string(domain.NoReasonInsufficientAsset)):
		return fmt.Errorf("nemate dovoljno sredstava za izvršenje opcije")
	case strings.Contains(m, string(domain.NoReasonNoSuchAsset)), strings.Contains(m, string(domain.NoReasonUnacceptableAsset)):
		return fmt.Errorf("nepodržan ili nepoznat asset u transakciji")
	}
	return err
}
