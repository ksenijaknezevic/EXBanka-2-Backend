package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/service"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newOptionContractService(repo domain.InterbankRepository) *service.InterbankOptionContractService {
	return service.NewInterbankOptionContractService(repo, nil, ourRouting)
}

// mockAcceptCoordinator — mock za 2PC poravnanje + escrow akcija.
type mockAcceptCoordinator struct{ mock.Mock }

func (m *mockAcceptCoordinator) InitiateInterbankTransaction(ctx context.Context, tx domain.Transaction, initiatorUserID *int64) (*domain.InterbankTransaction, error) {
	args := m.Called(ctx, tx, initiatorUserID)
	var r *domain.InterbankTransaction
	if v := args.Get(0); v != nil {
		r = v.(*domain.InterbankTransaction)
	}
	return r, args.Error(1)
}

func (m *mockAcceptCoordinator) BlockShares(ctx context.Context, userID int64, ticker string, amount int32) error {
	return m.Called(ctx, userID, ticker, amount).Error(0)
}

func (m *mockAcceptCoordinator) ReleaseShares(ctx context.Context, userID int64, ticker string, amount int32) error {
	return m.Called(ctx, userID, ticker, amount).Error(0)
}

func activeNegotiation() *domain.InterbankNegotiation {
	return &domain.InterbankNegotiation{
		NegotiationRoutingNumber:  ourRouting,
		NegotiationForeignID:      "neg1",
		StockTicker:               "AAPL",
		SettlementDate:            time.Now().Add(24 * time.Hour),
		PriceCurrency:             "USD",
		PriceAmount:               decimal.NewFromFloat(100),
		PremiumCurrency:           "USD",
		PremiumAmount:             decimal.NewFromFloat(5),
		Amount:                    10,
		BuyerRoutingNumber:        222,
		BuyerID:                   "buyer1",
		SellerRoutingNumber:       ourRouting,
		SellerID:                  "seller1",
		LastModifiedRoutingNumber: 222,
		LastModifiedID:            "buyer1",
		IsOngoing:                 true,
		Status:                    "OPEN",
	}
}

// ─── AcceptNegotiation ────────────────────────────────────────────────────────

func TestAcceptNegotiation_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "x")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestAcceptNegotiation_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetNegotiationByID", ctx, ourRouting, "x").Return(nil, errors.New("db"))

	err := svc.AcceptNegotiation(ctx, ourRouting, "x")
	require.Error(t, err)
}

func TestAcceptNegotiation_Conflict_NotOngoing(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	n := activeNegotiation()
	n.IsOngoing = false
	n.Status = "CANCELLED"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.ErrorIs(t, err, domain.ErrInterbankConflict)
}

func TestAcceptNegotiation_AlreadyAccepted_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	n := activeNegotiation()
	n.IsOngoing = false
	n.Status = "ACCEPTED"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
}

func TestAcceptNegotiation_CrossBank_HappyPath(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation() // buyer na banci 222
	n.SellerID = "1"         // naš prodavac (numerički user id)
	n.BuyerID = "2"

	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	// 1) blokada akcija prodavca
	coord.On("BlockShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)
	// 2) premium 2PC → COMMITTED
	coord.On("InitiateInterbankTransaction", ctx, mock.Anything, mock.Anything).
		Return(&domain.InterbankTransaction{Status: domain.TxStatusCommitted}, nil)
	// 3) ugovor ACTIVE + zatvaranje pregovaranja
	repo.On("CreateOptionContract", ctx, mock.MatchedBy(func(c *domain.InterbankOptionContract) bool {
		return c.Status == "ACTIVE" && c.StockTicker == "AAPL" && c.Amount == 10
	})).Return(nil)
	repo.On("UpdateNegotiation", ctx, mock.MatchedBy(func(nn *domain.InterbankNegotiation) bool {
		return !nn.IsOngoing && nn.Status == "ACCEPTED"
	})).Return(nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
	coord.AssertExpectations(t)
	repo.AssertExpectations(t)
}

// §3.6: accept NEW_TX mora imati 4 postinga — premium par + option-issuance par
// (OPTION{naš,negId} −1 i kupac PERSON +1, asset OPTION). Bez option-legova peer
// gate odbija premiju sa UNACCEPTABLE_ASSET.
func TestAcceptNegotiation_SendsFourPostingsWithOptionLegs(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation()
	n.SellerID = "1"
	n.BuyerID = "2"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	coord.On("BlockShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)
	coord.On("InitiateInterbankTransaction", ctx, mock.MatchedBy(func(tx domain.Transaction) bool {
		if len(tx.Postings) != 4 {
			return false
		}
		monas, option := 0, 0
		optMinusOne, buyerPlusOneOption := false, false
		for _, p := range tx.Postings {
			switch p.Asset.Type {
			case domain.AssetTypeMonas:
				monas++
			case domain.AssetTypeOption:
				option++
				if p.Account.Type == domain.AccountKindOption && p.Amount.Equal(decimal.NewFromInt(-1)) {
					optMinusOne = true
				}
				if p.Account.Type == domain.AccountKindPerson && p.Account.ID != nil && p.Account.ID.ID == "2" && p.Amount.Equal(decimal.NewFromInt(1)) {
					buyerPlusOneOption = true
				}
			}
		}
		return monas == 2 && option == 2 && optMinusOne && buyerPlusOneOption
	}), mock.Anything).Return(&domain.InterbankTransaction{Status: domain.TxStatusCommitted}, nil)
	repo.On("CreateOptionContract", ctx, mock.Anything).Return(nil)
	repo.On("UpdateNegotiation", ctx, mock.Anything).Return(nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.NoError(t, err)
	coord.AssertExpectations(t)
}

func TestAcceptNegotiation_PremiumFails_ReleasesShares(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation()
	n.SellerID = "1"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	coord.On("BlockShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)
	// premium nije naplaćen (ROLLED_BACK)
	coord.On("InitiateInterbankTransaction", ctx, mock.Anything, mock.Anything).
		Return(&domain.InterbankTransaction{Status: domain.TxStatusRolledBack}, nil)
	// kompenzacija: akcije se vraćaju
	coord.On("ReleaseShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.Error(t, err)
	// ugovor NIJE kreiran, pregovaranje NIJE zatvoreno
	repo.AssertNotCalled(t, "CreateOptionContract", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "UpdateNegotiation", mock.Anything, mock.Anything)
	coord.AssertExpectations(t)
}

// Kad banka kupca vrati INSUFFICIENT_ASSET, vraćamo jasnu korisničku poruku
// (FE je prikazuje umesto generičke „interna greška").
func TestAcceptNegotiation_PremiumRejected_InsufficientAsset(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation()
	n.SellerID = "1"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	coord.On("BlockShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)
	coord.On("InitiateInterbankTransaction", ctx, mock.Anything, mock.Anything).
		Return((*domain.InterbankTransaction)(nil), errors.New("interbank tx: druga banka odbila: INSUFFICIENT_ASSET"))
	coord.On("ReleaseShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.Error(t, err)
	assert.Equal(t, "kupac nema dovoljno sredstava za premiju", err.Error())
	repo.AssertNotCalled(t, "CreateOptionContract", mock.Anything, mock.Anything)
}

func TestAcceptNegotiation_InsufficientShares_NoPremium(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation()
	n.SellerID = "1"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	// blokada padne → premija se ne ni pokušava
	coord.On("BlockShares", ctx, int64(1), "AAPL", int32(10)).Return(errors.New("nedovoljno akcija"))

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.Error(t, err)
	coord.AssertNotCalled(t, "InitiateInterbankTransaction", mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateOptionContract", mock.Anything, mock.Anything)
	coord.AssertExpectations(t)
}

func TestAcceptNegotiation_CreateContractError(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation()
	n.SellerID = "1"
	repo.On("GetNegotiationByID", ctx, ourRouting, "neg1").Return(n, nil)
	coord.On("BlockShares", ctx, int64(1), "AAPL", int32(10)).Return(nil)
	coord.On("InitiateInterbankTransaction", ctx, mock.Anything, mock.Anything).
		Return(&domain.InterbankTransaction{Status: domain.TxStatusCommitted}, nil)
	// premija prošla, ali upis ugovora pukne → kritična greška (reconciliacija)
	repo.On("CreateOptionContract", ctx, mock.Anything).Return(errors.New("db"))

	err := svc.AcceptNegotiation(ctx, ourRouting, "neg1")
	require.Error(t, err)
	coord.AssertExpectations(t)
}

// Buyer-side accept: strani prodavac (222) hostuje i već je poravnao premiju (preko 2PC).
// Naša strana samo kreira lokalni ugovor (ACTIVE) — bez blokade akcija i bez premium 2PC lokalno.
func TestAcceptNegotiation_BuyerSide_CreatesLocalContract(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	n := activeNegotiation()
	n.NegotiationRoutingNumber = 222
	n.NegotiationForeignID = "rneg1"
	n.SellerRoutingNumber = 222
	n.SellerID = "S-9"
	n.BuyerRoutingNumber = ourRouting
	n.BuyerID = "1"

	repo.On("GetNegotiationByID", ctx, int64(222), "rneg1").Return(n, nil)
	repo.On("CreateOptionContract", ctx, mock.MatchedBy(func(c *domain.InterbankOptionContract) bool {
		return c.Status == "ACTIVE" && c.NegotiationRoutingNumber == 222 && c.BuyerRoutingNumber == ourRouting && c.SellerRoutingNumber == 222
	})).Return(nil)
	repo.On("UpdateNegotiation", ctx, mock.MatchedBy(func(nn *domain.InterbankNegotiation) bool {
		return !nn.IsOngoing && nn.Status == "ACCEPTED"
	})).Return(nil)

	err := svc.AcceptNegotiation(ctx, 222, "rneg1")
	require.NoError(t, err)
	coord.AssertNotCalled(t, "BlockShares", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	coord.AssertNotCalled(t, "InitiateInterbankTransaction", mock.Anything, mock.Anything, mock.Anything)
	repo.AssertExpectations(t)
}

// ─── ListContracts ────────────────────────────────────────────────────────────

func TestListContracts_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	contracts := []domain.InterbankOptionContract{
		{ID: 1, StockTicker: "AAPL"},
		{ID: 2, StockTicker: "GOOG"},
	}
	repo.On("ListContractsForUser", ctx, ourRouting, "42").Return(contracts, nil)

	list, err := svc.ListContracts(ctx, 42)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestListContracts_Error(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("ListContractsForUser", ctx, ourRouting, "42").Return(nil, errors.New("db"))

	_, err := svc.ListContracts(ctx, 42)
	require.Error(t, err)
}

// ─── GetContract ──────────────────────────────────────────────────────────────

func TestGetContract_OK(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := &domain.InterbankOptionContract{ID: 5, StockTicker: "TSLA", Status: "ACTIVE"}
	repo.On("GetOptionContract", ctx, ourRouting, "contract1").Return(c, nil)

	got, err := svc.GetContract(ctx, ourRouting, "contract1")
	require.NoError(t, err)
	assert.Equal(t, "TSLA", got.StockTicker)
}

func TestGetContract_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "x").Return(nil, nil)

	_, err := svc.GetContract(ctx, ourRouting, "x")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestGetContract_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "x").Return(nil, errors.New("db"))

	_, err := svc.GetContract(ctx, ourRouting, "x")
	require.Error(t, err)
}

// ─── ExerciseContract — validation paths (coordinator is nil, never reached) ──

func activeContract() *domain.InterbankOptionContract {
	return &domain.InterbankOptionContract{
		ID:                       1,
		BuyerRoutingNumber:       ourRouting,
		BuyerID:                  "42",
		Status:                   "ACTIVE",
		SettlementDate:           time.Now().Add(48 * time.Hour),
		NegotiationRoutingNumber: ourRouting,
		NegotiationForeignID:     "neg1",
		StockTicker:              "AAPL",
		Amount:                   10,
		PriceCurrency:            "USD",
		PriceAmount:              decimal.NewFromInt(100),
	}
}

func TestExerciseContract_RepoError(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "c1").Return(nil, errors.New("db"))

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c1")
	require.Error(t, err)
}

func TestExerciseContract_NotFound(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	repo.On("GetOptionContract", ctx, ourRouting, "c2").Return(nil, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c2")
	require.ErrorIs(t, err, domain.ErrInterbankNotFound)
}

func TestExerciseContract_BuyerOnOtherBank(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract()
	c.BuyerRoutingNumber = 999 // not our bank
	repo.On("GetOptionContract", ctx, ourRouting, "c3").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kupac")
}

func TestExerciseContract_NotBuyer(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract() // BuyerID = "42"
	repo.On("GetOptionContract", ctx, ourRouting, "c4").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 99, ourRouting, "c4") // wrong caller
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kupac")
}

func TestExerciseContract_NotActive(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract()
	c.Status = "EXERCISED"
	repo.On("GetOptionContract", ctx, ourRouting, "c5").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ACTIVE")
}

func TestExerciseContract_SettlementDatePassed(t *testing.T) {
	repo := &mockInterbankRepo{}
	ctx := context.Background()
	svc := newOptionContractService(repo)

	c := activeContract()
	c.SettlementDate = time.Now().Add(-24 * time.Hour) // past
	repo.On("GetOptionContract", ctx, ourRouting, "c6").Return(c, nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c6")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "settlementDate")
}

// Exercise smer: call opcija → kupac PLAĆA novac (−) i PRIMA akcije (+);
// na uspešan 2PC ugovor postaje EXERCISED.
func TestExerciseContract_BuyerPaysCashReceivesStock_AndMarksExercised(t *testing.T) {
	repo := &mockInterbankRepo{}
	coord := &mockAcceptCoordinator{}
	ctx := context.Background()
	svc := service.NewInterbankOptionContractService(repo, coord, ourRouting)

	c := activeContract() // BuyerRoutingNumber=ourRouting, BuyerID="42", AAPL, Amount=10, Price=100 USD
	repo.On("GetOptionContract", ctx, ourRouting, "c1").Return(c, nil)
	coord.On("InitiateInterbankTransaction", ctx, mock.MatchedBy(func(tx domain.Transaction) bool {
		var buyerCash, buyerStock decimal.Decimal
		for _, p := range tx.Postings {
			if p.Account.Type == domain.AccountKindPerson && p.Account.ID != nil && p.Account.ID.ID == "42" {
				if p.Asset.Type == domain.AssetTypeMonas {
					buyerCash = p.Amount
				}
				if p.Asset.Type == domain.AssetTypeStock {
					buyerStock = p.Amount
				}
			}
		}
		// kupac plaća novac (−1000) i prima akcije (+10)
		return buyerCash.Equal(decimal.NewFromInt(-1000)) && buyerStock.Equal(decimal.NewFromInt(10))
	}), mock.Anything).Return(&domain.InterbankTransaction{Status: domain.TxStatusCommitted}, nil)
	repo.On("UpdateOptionContractStatus", ctx, ourRouting, "c1", "EXERCISED", mock.Anything).Return(nil)

	_, err := svc.ExerciseContract(ctx, 42, ourRouting, "c1")
	require.NoError(t, err)
	coord.AssertExpectations(t)
	repo.AssertExpectations(t)
}
