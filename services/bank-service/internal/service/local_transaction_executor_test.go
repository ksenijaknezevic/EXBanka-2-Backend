package service

// White-box tests for pure helpers in local_transaction_executor.go
// and basic Commit/Rollback with empty reservation sets.

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

// ─── isBalanced ───────────────────────────────────────────────────────────────

func TestIsBalanced_Empty(t *testing.T) {
	assert.True(t, isBalanced(nil))
}

func TestIsBalanced_SingleCurrencyBalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	buyer := &domain.ForeignBankId{RoutingNumber: 1, ID: "user1"}
	seller := &domain.ForeignBankId{RoutingNumber: 1, ID: "user2"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: decimal.NewFromFloat(-100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: seller}, Amount: decimal.NewFromFloat(100), Asset: usd},
	}
	assert.True(t, isBalanced(postings))
}

func TestIsBalanced_Unbalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	buyer := &domain.ForeignBankId{RoutingNumber: 1, ID: "user1"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: decimal.NewFromFloat(-50), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer}, Amount: decimal.NewFromFloat(100), Asset: usd},
	}
	assert.False(t, isBalanced(postings))
}

func TestIsBalanced_MultiAssetEachBalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	stock := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "AAPL"}}
	a := &domain.ForeignBankId{RoutingNumber: 1, ID: "a"}
	b := &domain.ForeignBankId{RoutingNumber: 1, ID: "b"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(-100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromFloat(100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(5), Asset: stock},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromFloat(-5), Asset: stock},
	}
	assert.True(t, isBalanced(postings))
}

func TestIsBalanced_MultiAssetOnlyOneUnbalanced(t *testing.T) {
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	rsd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "RSD"}}
	a := &domain.ForeignBankId{RoutingNumber: 1, ID: "a"}
	postings := []domain.Posting{
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(-100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(100), Asset: usd},
		{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromFloat(50), Asset: rsd},
		// RSD not balanced
	}
	assert.False(t, isBalanced(postings))
}

// ─── assetKey ─────────────────────────────────────────────────────────────────

func TestAssetKey_Monas(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "EUR"}}
	assert.Equal(t, "MONAS:EUR", assetKey(a))
}

func TestAssetKey_MonasNilMonAs(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeMonas}
	assert.Equal(t, "MONAS:?", assetKey(a))
}

func TestAssetKey_Stock(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "TSLA"}}
	assert.Equal(t, "STOCK:TSLA", assetKey(a))
}

func TestAssetKey_StockNilStock(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeStock}
	assert.Equal(t, "STOCK:?", assetKey(a))
}

func TestAssetKey_Option(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeOption, Option: &domain.OptionDescription{
		NegotiationID: domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
	}}
	key := assetKey(a)
	assert.Contains(t, key, "OPTION:")
	assert.Contains(t, key, "111")
	assert.Contains(t, key, "opt1")
}

func TestAssetKey_OptionNilOption(t *testing.T) {
	a := domain.Asset{Type: domain.AssetTypeOption}
	assert.Equal(t, "OPTION:?", assetKey(a))
}

// ─── MarshalTransaction / UnmarshalTransaction ────────────────────────────────

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	buyer := &domain.ForeignBankId{RoutingNumber: 111, ID: "buyer1"}
	seller := &domain.ForeignBankId{RoutingNumber: 222, ID: "seller1"}
	tx := domain.Transaction{
		TransactionID:  domain.ForeignBankId{RoutingNumber: 111, ID: "tx1"},
		Message:        "test transfer",
		PaymentCode:    "289",
		PaymentPurpose: "TEST",
		Postings: []domain.Posting{
			{
				Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer},
				Amount:  decimal.NewFromFloat(-100),
				Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
			},
			{
				Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: seller},
				Amount:  decimal.NewFromFloat(100),
				Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
			},
		},
	}

	payload, err := MarshalTransaction(tx)
	require.NoError(t, err)
	assert.NotEmpty(t, payload)

	restored, err := UnmarshalTransaction(payload)
	require.NoError(t, err)
	assert.Equal(t, tx.TransactionID, restored.TransactionID)
	assert.Equal(t, tx.Message, restored.Message)
	assert.Len(t, restored.Postings, 2)
}

func TestUnmarshalTransaction_InvalidJSON(t *testing.T) {
	_, err := UnmarshalTransaction("{not-json}")
	require.Error(t, err)
}

// ─── Commit with empty reservations ──────────────────────────────────────────

type mockInterbankRepoForExecutor struct{ mock.Mock }

func (m *mockInterbankRepoForExecutor) GetIncomingByIdempotence(ctx context.Context, rn int64, lk string) (*domain.InterbankMessageLog, error) {
	args := m.Called(ctx, rn, lk)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) GetOutgoingByIdempotence(ctx context.Context, rn int64, lk string) (*domain.InterbankMessageLog, error) {
	args := m.Called(ctx, rn, lk)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateMessage(ctx context.Context, msg *domain.InterbankMessageLog) error {
	return m.Called(ctx, msg).Error(0)
}
func (m *mockInterbankRepoForExecutor) UpdateMessage(ctx context.Context, msg *domain.InterbankMessageLog) error {
	return m.Called(ctx, msg).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListPendingOutgoing(ctx context.Context, limit int) ([]domain.InterbankMessageLog, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankMessageLog), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateTransaction(ctx context.Context, t *domain.InterbankTransaction) error {
	return m.Called(ctx, t).Error(0)
}
func (m *mockInterbankRepoForExecutor) GetTransactionByForeignID(ctx context.Context, rn int64, fid string) (*domain.InterbankTransaction, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankTransaction), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) UpdateTransactionStatus(ctx context.Context, id int64, status domain.InterbankTxStatus, step, reason string) error {
	return m.Called(ctx, id, status, step, reason).Error(0)
}
func (m *mockInterbankRepoForExecutor) CreateReservation(ctx context.Context, r *domain.InterbankReservation) error {
	return m.Called(ctx, r).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListExpiredActiveContracts(ctx context.Context, before time.Time) ([]domain.InterbankOptionContract, error) {
	return nil, nil
}

func (m *mockInterbankRepoForExecutor) ListReservationsByTx(ctx context.Context, txID int64) ([]domain.InterbankReservation, error) {
	args := m.Called(ctx, txID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankReservation), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	return m.Called(ctx, n).Error(0)
}
func (m *mockInterbankRepoForExecutor) GetNegotiationByID(ctx context.Context, rn int64, fid string) (*domain.InterbankNegotiation, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankNegotiation), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) UpdateNegotiation(ctx context.Context, n *domain.InterbankNegotiation) error {
	return m.Called(ctx, n).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListNegotiations(ctx context.Context, filter domain.ListInterbankNegotiationsFilter) ([]domain.InterbankNegotiation, error) {
	args := m.Called(ctx, filter)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankNegotiation), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) ListPublicStocks(ctx context.Context) ([]domain.PublicStock, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.PublicStock), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) CreateOptionContract(ctx context.Context, c *domain.InterbankOptionContract) error {
	return m.Called(ctx, c).Error(0)
}
func (m *mockInterbankRepoForExecutor) GetOptionContract(ctx context.Context, rn int64, fid string) (*domain.InterbankOptionContract, error) {
	args := m.Called(ctx, rn, fid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.InterbankOptionContract), args.Error(1)
}
func (m *mockInterbankRepoForExecutor) UpdateOptionContractStatus(ctx context.Context, rn int64, fid, status string, usedAt *time.Time) error {
	return m.Called(ctx, rn, fid, status, usedAt).Error(0)
}
func (m *mockInterbankRepoForExecutor) ListContractsForUser(ctx context.Context, rn int64, userID string) ([]domain.InterbankOptionContract, error) {
	args := m.Called(ctx, rn, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.InterbankOptionContract), args.Error(1)
}

func TestCommit_EmptyReservations(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(42)).Return([]domain.InterbankReservation{}, nil)

	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(42), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 42)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_ReservationsListError(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(42)).Return(nil, assert.AnError)

	err := e.Commit(ctx, 42)
	require.Error(t, err)
}

func TestRollback_EmptyReservations(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(77)).Return([]domain.InterbankReservation{}, nil)

	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(77), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Rollback(ctx, 77)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestRollback_ReservationsListError(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")

	ctx := context.Background()
	repo.On("ListReservationsByTx", ctx, int64(77)).Return(nil, assert.AnError)

	err := e.Rollback(ctx, 77)
	require.Error(t, err)
}

// ─── isLocalPosting ───────────────────────────────────────────────────────────

func TestIsLocalPosting_PersonLocal(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "user1"},
		},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.True(t, local)
}

func TestIsLocalPosting_PersonRemote(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 999, ID: "user1"},
		},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.False(t, local)
}

func TestIsLocalPosting_AccountLocal(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	num := "265-001-01234"
	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.True(t, local)
}

func TestIsLocalPosting_AccountForeign(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	num := "111-001-01234"
	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
	}
	local, err := e.isLocalPosting(p)
	require.NoError(t, err)
	assert.False(t, local)
}

func TestIsLocalPosting_AccountNoNum_Error(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{Account: domain.TxAccount{Type: domain.AccountKindAccount}}
	_, err := e.isLocalPosting(p)
	require.Error(t, err)
}

func TestIsLocalPosting_PersonNoID_Error(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{Account: domain.TxAccount{Type: domain.AccountKindPerson}}
	_, err := e.isLocalPosting(p)
	require.Error(t, err)
}

func TestIsLocalPosting_UnknownKind_Error(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")

	p := domain.Posting{Account: domain.TxAccount{Type: "UNKNOWN"}}
	_, err := e.isLocalPosting(p)
	require.Error(t, err)
}

// ─── pCopy / ptrTime ──────────────────────────────────────────────────────────

func TestPCopy(t *testing.T) {
	buyer := &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"}
	original := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: buyer},
		Amount:  decimal.NewFromFloat(50),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	cp := pCopy(original)
	require.NotNil(t, cp)
	assert.Equal(t, original.Amount, cp.Amount)
	assert.Equal(t, original.Account.Type, cp.Account.Type)
}

func TestPtrTime(t *testing.T) {
	now := time.Now().UTC()
	p := ptrTime(now)
	require.NotNil(t, p)
	assert.Equal(t, now, *p)
}

// ─── validatePosting — PERSON + MONAS (no DB) ─────────────────────────────────

// E1: PERSON + MONAS sada razrešava stvarni račun (id_vlasnika + valuta) i
// proverava sredstva za debit. Credit zahteva samo da račun postoji.

func personMonasPosting(routing int64, id string, amount float64, currency string) domain.Posting {
	return domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: &domain.ForeignBankId{RoutingNumber: routing, ID: id}},
		Amount:  decimal.NewFromFloat(amount),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: currency}},
	}
}

func personAccountRows(mock sqlmock.Sqlmock, brojRacuna, stanje, rezervisana string) *sqlmock.Rows {
	// Valuta računa = USD (== valuta postinga u ovim testovima) → bez konverzije.
	return mock.NewRows([]string{"broj_racuna", "stanje_racuna", "rezervisana_sredstva", "valuta_oznaka"}).
		AddRow(brojRacuna, stanje, rezervisana, "USD")
}

func TestValidatePosting_PersonMonas_CreditValid(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(personAccountRows(dbMock, "265001000001", "1000", "0"))

	reason := e.validatePosting(ctx, personMonasPosting(111, "1", 100, "USD"))
	assert.Nil(t, reason)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestValidatePosting_PersonMonas_DebitSufficient(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(personAccountRows(dbMock, "265001000001", "1000", "0"))

	reason := e.validatePosting(ctx, personMonasPosting(111, "1", -100, "USD"))
	assert.Nil(t, reason)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestValidatePosting_PersonMonas_DebitInsufficient(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(personAccountRows(dbMock, "265001000001", "50", "0"))

	reason := e.validatePosting(ctx, personMonasPosting(111, "1", -100, "USD"))
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonInsufficientAsset, reason.Reason)
}

func TestValidatePosting_PersonMonas_NoAccount(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(
		dbMock.NewRows([]string{"broj_racuna", "stanje_racuna", "rezervisana_sredstva", "valuta_oznaka"}))

	reason := e.validatePosting(ctx, personMonasPosting(111, "1", -100, "USD"))
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonNoSuchAccount, reason.Reason)
}

func TestValidatePosting_PersonMonas_NilMonAs(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(100),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas}, // nil MonAs
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonNoSuchAsset, reason.Reason)
}

func TestValidatePosting_PersonStock_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	// Positive amount = no DB check needed
	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: "AAPL"}},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

func TestValidatePosting_PersonStock_EmptyTicker(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeStock, Stock: &domain.StockDescription{Ticker: ""}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonNoSuchAsset, reason.Reason)
}

// §3.6 izdavanje: OPTION nalog + OPTION asset (accept) prolazi BEZ postojećeg
// ACTIVE ugovora (ugovor se kreira na 2PC commit-u).
func TestValidatePosting_OptionIssuance_NoContractRequired(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindOption, ID: &domain.ForeignBankId{RoutingNumber: 111, ID: "neg1"}},
		Amount:  decimal.NewFromInt(-1),
		Asset: domain.Asset{Type: domain.AssetTypeOption, Option: &domain.OptionDescription{
			NegotiationID: domain.ForeignBankId{RoutingNumber: 111, ID: "neg1"},
			Stock:         domain.StockDescription{Ticker: "AAPL"},
			Amount:        decimal.NewFromInt(10),
		}},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

func TestValidatePosting_OptionIssuance_ZeroAmount(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindOption, ID: &domain.ForeignBankId{RoutingNumber: 111, ID: "neg1"}},
		Amount:  decimal.Zero,
		Asset:   domain.Asset{Type: domain.AssetTypeOption, Option: &domain.OptionDescription{NegotiationID: domain.ForeignBankId{RoutingNumber: 111, ID: "neg1"}}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionAmountIncorrect, reason.Reason)
}

// Exercise (OPTION nalog + MONAS asset) i dalje zahteva postojeći ugovor.
func TestValidatePosting_OptionExercise_RequiresContract(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	repo.On("GetOptionContract", mock.Anything, int64(111), "neg1").Return(nil, nil)
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindOption, ID: &domain.ForeignBankId{RoutingNumber: 111, ID: "neg1"}},
		Amount:  decimal.NewFromInt(100),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionNegotiationNotFound, reason.Reason)
}

func TestValidatePosting_PersonOption_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindPerson,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "u1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeOption},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

func TestValidatePosting_OptionAccount_NilID(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindOption},
		Amount:  decimal.NewFromFloat(5),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionNegotiationNotFound, reason.Reason)
}

func TestValidatePosting_OptionAccount_ContractNotFound(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(nil, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionNegotiationNotFound, reason.Reason)
}

func TestValidatePosting_OptionAccount_NotActive(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "EXERCISED",
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionUsedOrExpired, reason.Reason)
}

func TestValidatePosting_OptionAccount_Expired(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "ACTIVE",
		SettlementDate: time.Now().Add(-24 * time.Hour), // past
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionUsedOrExpired, reason.Reason)
}

func TestValidatePosting_OptionAccount_ZeroAmount(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "ACTIVE",
		SettlementDate: time.Now().Add(24 * time.Hour),
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(0), // zero
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonOptionAmountIncorrect, reason.Reason)
}

func TestValidatePosting_OptionAccount_Valid(t *testing.T) {
	db, _ := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	contract := &domain.InterbankOptionContract{
		ID:             1,
		Status:         "ACTIVE",
		SettlementDate: time.Now().Add(48 * time.Hour),
	}
	repo.On("GetOptionContract", ctx, int64(111), "opt1").Return(contract, nil)

	p := domain.Posting{
		Account: domain.TxAccount{
			Type: domain.AccountKindOption,
			ID:   &domain.ForeignBankId{RoutingNumber: 111, ID: "opt1"},
		},
		Amount: decimal.NewFromFloat(5),
		Asset:  domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	assert.Nil(t, reason)
}

// ─── Prepare ──────────────────────────────────────────────────────────────────

func TestPrepare_Unbalanced(t *testing.T) {
	db, _ := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteNo, vote.Vote)
	require.Len(t, vote.Reasons, 1)
	assert.Equal(t, domain.NoReasonUnbalancedTx, vote.Reasons[0].Reason)
}

func TestPrepare_AllRemote_VoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	a := &domain.ForeignBankId{RoutingNumber: 222, ID: "u1"}
	b := &domain.ForeignBankId{RoutingNumber: 222, ID: "u2"}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: a}, Amount: decimal.NewFromInt(-100), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: b}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}
	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestPrepare_LocalAccountNotFound_VoteNo(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num1 := "265-001-00001"
	num2 := "265-001-00002"
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num1}, Amount: decimal.NewFromInt(-50), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num2}, Amount: decimal.NewFromInt(50), Asset: usd},
		},
	}
	// Both SELECTs return empty rows → account not found → VoteNo (no db.Transaction call)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT").WillReturnRows(dbMock.NewRows(cols))
	dbMock.ExpectQuery("SELECT").WillReturnRows(dbMock.NewRows(cols))

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteNo, vote.Vote)
	assert.NotEmpty(t, vote.Reasons)
}

func TestPrepare_LocalAccountSufficientFunds_VoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num1 := "265001000001" // sender (negative)
	num2 := "265001000002" // receiver (positive)
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num1}, Amount: decimal.NewFromInt(-100), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num2}, Amount: decimal.NewFromInt(100), Asset: usd},
		},
	}

	// validatePosting: sender account (status=AKTIVAN, USD, stanje=500, rez=0)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "USD", "500", "0", "AKTIVAN"))
	// validatePosting: receiver account (status=AKTIVAN, USD)
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(2), "USD", "200", "0", "AKTIVAN"))

	// reservation loop
	dbMock.ExpectBegin()
	// sender: UPDATE rezervisana_sredstva (negative amount)
	dbMock.ExpectExec("UPDATE core_banking.racun SET rezervisana_sredstva").WillReturnResult(sqlmock.NewResult(1, 1))
	// CreateReservation for sender and receiver
	repo.On("CreateReservation", ctx, mock.Anything).Return(nil).Times(2)
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestPrepare_LocalAccount_InsufficientFunds_VoteNo(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	num1 := "265001000010"
	num2 := "265001000011"
	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num1}, Amount: decimal.NewFromInt(-1000), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num2}, Amount: decimal.NewFromInt(1000), Asset: usd},
		},
	}

	// sender: only 50 available (stanje=50, rez=0)
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "USD", "50", "0", "AKTIVAN"))
	// receiver: ok
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(2), "USD", "200", "0", "AKTIVAN"))

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteNo, vote.Vote)
	assert.NotEmpty(t, vote.Reasons)
}

func TestValidatePosting_Account_CurrencyMismatch(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	num := "265001000020"
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "EUR", "500", "0", "AKTIVAN"))

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
		Amount:  decimal.NewFromFloat(-100),
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}, // mismatch
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonUnacceptableAsset, reason.Reason)
}

func TestValidatePosting_Account_InsufficientFunds(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	num := "265001000021"
	cols := []string{"id", "valuta_oznaka", "stanje_racuna", "rezervisana_sredstva", "status"}
	dbMock.ExpectQuery("SELECT r.id").WillReturnRows(
		dbMock.NewRows(cols).AddRow(int64(1), "USD", "50", "0", "AKTIVAN"))

	p := domain.Posting{
		Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num},
		Amount:  decimal.NewFromFloat(-200), // want 200, only have 50
		Asset:   domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}},
	}
	reason := e.validatePosting(ctx, p)
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonInsufficientAsset, reason.Reason)
}

func TestPrepare_EmptyTx_VoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 99, domain.Transaction{})
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── Commit with non-empty reservations ──────────────────────────────────────

func TestCommit_MonasNegativeReservation(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000001"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 10,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-100),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(10)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(42)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(10), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 10)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_MonasPositiveReservation(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000002"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 11,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(100),
		AccountNum:             &num,
		Reserved:               false,
	}
	repo.On("ListReservationsByTx", ctx, int64(11)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(43)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(11), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 11)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_MonasNegative_WriteTxRowRacunNotFound(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000003"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 12,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-50),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(12)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	// writeTxRow: racun not found (id=0)
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(0)))
	dbMock.ExpectRollback()

	err := e.Commit(ctx, 12)
	require.Error(t, err)
}

// ─── Rollback with non-empty reservations ────────────────────────────────────

func TestRollback_ReservedMonasNegative(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000004"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 20,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-75),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(20)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(20), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Rollback(ctx, 20)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestRollback_NotReserved_Skipped(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000005"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 21,
		AccountKind:            domain.AccountKindAccount,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-75),
		AccountNum:             &num,
		Reserved:               false, // not reserved — skip
	}
	repo.On("ListReservationsByTx", ctx, int64(21)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	repo.On("UpdateTransactionStatus", ctx, int64(21), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Rollback(ctx, 21)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── E1: Prepare/Commit za PERSON + MONAS ─────────────────────────────────────

func TestPrepare_LocalPersonMonasDebit_ReservesVoteYes(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	usd := domain.Asset{Type: domain.AssetTypeMonas, MonAs: &domain.MonetaryAsset{Currency: "USD"}}
	localBuyer := &domain.ForeignBankId{RoutingNumber: 111, ID: "1"}   // naš kupac → debit (lokalno)
	remoteSeller := &domain.ForeignBankId{RoutingNumber: 222, ID: "2"} // prodavac na drugoj banci → skip
	tx := domain.Transaction{
		Postings: []domain.Posting{
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: localBuyer}, Amount: decimal.NewFromInt(-5), Asset: usd},
			{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: remoteSeller}, Amount: decimal.NewFromInt(5), Asset: usd},
		},
	}

	// validate: razreši lokalni račun kupca
	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(personAccountRows(dbMock, "265001000001", "1000", "0"))
	// reserve loop: razreši opet + UPDATE rezervisana
	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(personAccountRows(dbMock, "265001000001", "1000", "0"))
	dbMock.ExpectExec("UPDATE core_banking.racun SET rezervisana_sredstva").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("CreateReservation", ctx, mock.Anything).Return(nil).Times(1)
	dbMock.ExpectCommit()

	vote, err := e.Prepare(ctx, 1, tx)
	require.NoError(t, err)
	assert.Equal(t, domain.VoteYes, vote.Vote)
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_PersonMonasNegative(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000001"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 30,
		AccountKind:            domain.AccountKindPerson,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-5),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(30)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(42)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(30), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	require.NoError(t, e.Commit(ctx, 30))
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestCommit_PersonMonasPositive(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000002"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 31,
		AccountKind:            domain.AccountKindPerson,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(5),
		AccountNum:             &num,
		Reserved:               false,
	}
	repo.On("ListReservationsByTx", ctx, int64(31)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(43)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(31), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	require.NoError(t, e.Commit(ctx, 31))
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── Escrow: BlockShares / ReleaseShares ──────────────────────────────────────

func TestBlockShares_FIFO(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT ps.id, ps.quantity").WillReturnRows(
		dbMock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int32(4)).AddRow(int64(2), int32(10)))
	// uzmi 4 iz reda 1 → 0 → DELETE; uzmi 1 iz reda 2 → 9 → UPDATE
	dbMock.ExpectExec("DELETE FROM core_banking.public_shares").WillReturnResult(sqlmock.NewResult(0, 1))
	dbMock.ExpectExec("UPDATE core_banking.public_shares SET quantity").WillReturnResult(sqlmock.NewResult(0, 1))
	dbMock.ExpectCommit()

	require.NoError(t, e.BlockShares(ctx, 1, "AAPL", 5))
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestBlockShares_Insufficient(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT ps.id, ps.quantity").WillReturnRows(
		dbMock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int32(3)))
	dbMock.ExpectExec("DELETE FROM core_banking.public_shares").WillReturnResult(sqlmock.NewResult(0, 1))
	dbMock.ExpectRollback()

	err := e.BlockShares(ctx, 1, "AAPL", 5)
	require.Error(t, err)
}

func TestRollback_PersonMonasNegative(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	num := "265001000009"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 40,
		AccountKind:            domain.AccountKindPerson,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(-5),
		AccountNum:             &num,
		Reserved:               true,
	}
	repo.On("ListReservationsByTx", ctx, int64(40)).Return([]domain.InterbankReservation{resv}, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateTransactionStatus", ctx, int64(40), domain.TxStatusRolledBack, "ROLLED_BACK", "").Return(nil)
	dbMock.ExpectCommit()

	require.NoError(t, e.Rollback(ctx, 40))
	require.NoError(t, dbMock.ExpectationsWereMet())
}

func TestReleaseShares_Inserts(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	ctx := context.Background()

	dbMock.ExpectQuery("SELECT id FROM core_banking.listing WHERE ticker").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(7)))
	dbMock.ExpectExec("INSERT INTO core_banking.public_shares").WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, e.ReleaseShares(ctx, 1, "AAPL", 5))
	require.NoError(t, dbMock.ExpectationsWereMet())
}

// ─── Menjačnica auto-konverzija (PERSON+MONAS, valuta dogovora != valuta računa) ──

type mockRateSource struct{ rates []domain.ExchangeRate }

func (m *mockRateSource) GetRates(_ context.Context) ([]domain.ExchangeRate, error) {
	return m.rates, nil
}

func usdRates() *mockRateSource {
	return &mockRateSource{rates: []domain.ExchangeRate{{Oznaka: "USD", Kupovni: 100, Srednji: 105, Prodajni: 110}}}
}

func TestConvertForSettlement_SameCurrency_NoChange(t *testing.T) {
	e := NewLocalTransactionExecutor(nil, &mockInterbankRepoForExecutor{}, 111, "265")
	out, err := e.convertForSettlement(context.Background(), decimal.NewFromInt(5), "USD", "USD")
	require.NoError(t, err)
	assert.True(t, out.Equal(decimal.NewFromInt(5)))
}

func TestConvertForSettlement_CreditUsesKupovni(t *testing.T) {
	e := NewLocalTransactionExecutor(nil, &mockInterbankRepoForExecutor{}, 111, "265")
	e.SetExchangeRates(usdRates())
	// credit (+5 USD) → RSD po KUPOVNOM (100) = +500
	out, err := e.convertForSettlement(context.Background(), decimal.NewFromInt(5), "USD", "RSD")
	require.NoError(t, err)
	assert.True(t, out.Equal(decimal.NewFromInt(500)), "got %s", out)
}

func TestConvertForSettlement_DebitUsesProdajni(t *testing.T) {
	e := NewLocalTransactionExecutor(nil, &mockInterbankRepoForExecutor{}, 111, "265")
	e.SetExchangeRates(usdRates())
	// debit (-5 USD) → RSD po PRODAJNOM (110) = -550
	out, err := e.convertForSettlement(context.Background(), decimal.NewFromInt(-5), "USD", "RSD")
	require.NoError(t, err)
	assert.True(t, out.Equal(decimal.NewFromInt(-550)), "got %s", out)
}

func TestConvertForSettlement_NoRateSource_Errors(t *testing.T) {
	e := NewLocalTransactionExecutor(nil, &mockInterbankRepoForExecutor{}, 111, "265")
	_, err := e.convertForSettlement(context.Background(), decimal.NewFromInt(5), "USD", "RSD")
	require.Error(t, err)
}

func TestValidatePosting_PersonMonas_DebitConvertsToRSD_Sufficient(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	e.SetExchangeRates(usdRates())
	ctx := context.Background()
	// račun RSD, stanje 600; debit 5 USD → 550 RSD (prodajni 110) ≤ 600 → OK
	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(
		dbMock.NewRows([]string{"broj_racuna", "stanje_racuna", "rezervisana_sredstva", "valuta_oznaka"}).
			AddRow("265001000001", "600", "0", "RSD"))
	assert.Nil(t, e.validatePosting(ctx, personMonasPosting(111, "1", -5, "USD")))
}

func TestValidatePosting_PersonMonas_DebitConvertsToRSD_Insufficient(t *testing.T) {
	db, dbMock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 111, "265")
	e.SetExchangeRates(usdRates())
	ctx := context.Background()
	// račun RSD, stanje 500; debit 5 USD → 550 RSD > 500 → INSUFFICIENT
	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(
		dbMock.NewRows([]string{"broj_racuna", "stanje_racuna", "rezervisana_sredstva", "valuta_oznaka"}).
			AddRow("265001000001", "500", "0", "RSD"))
	reason := e.validatePosting(ctx, personMonasPosting(111, "1", -5, "USD"))
	require.NotNil(t, reason)
	assert.Equal(t, domain.NoReasonInsufficientAsset, reason.Reason)
}

// Regresija: routingNumber (npr. 265) i prefiks broja računa (npr. "666") su
// RAZLIČITI. Ranije se accountPrefix izvodio iz routinga → "666…" račun se
// tretirao kao tuđ → kod odlaznog plaćanja debit se NIJE knjižio kod nas, a
// COMMIT_TX se slao Banci 2 (oni upisali +iznos). Ovaj test čuva razdvajanje.
func TestIsLocalPosting_AccountPrefixDiffersFromRouting(t *testing.T) {
	e := NewLocalTransactionExecutor(nil, nil, 265, "666")

	acc := func(num string) domain.Posting {
		return domain.Posting{Account: domain.TxAccount{Type: domain.AccountKindAccount, Num: &num}}
	}
	person := func(rn int64) domain.Posting {
		return domain.Posting{Account: domain.TxAccount{Type: domain.AccountKindPerson, ID: &domain.ForeignBankId{RoutingNumber: rn, ID: "x"}}}
	}

	local, err := e.isLocalPosting(acc("666000111960466030"))
	require.NoError(t, err)
	assert.True(t, local, "666… račun mora biti lokalan (naš)")

	local, err = e.isLocalPosting(acc("222000131234567812"))
	require.NoError(t, err)
	assert.False(t, local, "222… (Banka 2) mora biti tuđ")

	// PERSON/OPTION koriste routing, ne prefiks broja računa.
	local, _ = e.isLocalPosting(person(265))
	assert.True(t, local, "PERSON{265} je naš")
	local, _ = e.isLocalPosting(person(222))
	assert.False(t, local, "PERSON{222} je tuđ")
}

// #3 (S9): kad smo MI banka-prodavac i strani kupac iskoristi opciju, OPTION+MONAS
// pozitivan leg (strike) mora KREDITIRATI prodavčev račun — ranije je Commit za
// OPTION nalog samo flipovao status (prodavac nije dobijao novac).
func TestCommit_OptionMonasPositive_CreditsSeller(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 111, "265")
	ctx := context.Background()

	rn := int64(111)
	fid := "neg1"
	cur := "USD"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 20,
		AccountKind:            domain.AccountKindOption,
		AssetType:              domain.AssetTypeMonas,
		Amount:                 decimal.NewFromInt(200), // strike ukupno; prodavac prima
		ForeignRoutingNumber:   &rn,
		ForeignID:              &fid,
		AssetCurrency:          &cur,
	}
	repo.On("ListReservationsByTx", ctx, int64(20)).Return([]domain.InterbankReservation{resv}, nil)
	// Mi smo prodavac: SellerRoutingNumber == naš routing (111).
	contract := &domain.InterbankOptionContract{
		NegotiationRoutingNumber: 111, NegotiationForeignID: "neg1",
		SellerRoutingNumber: 111, SellerID: "2", PriceCurrency: "USD",
	}
	repo.On("GetOptionContract", ctx, int64(111), "neg1").Return(contract, nil)

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("SELECT r.broj_racuna").WillReturnRows(personAccountRows(dbMock, "265111", "1000", "0"))
	dbMock.ExpectExec("UPDATE core_banking.racun").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectQuery("SELECT id FROM core_banking.racun WHERE broj_racuna").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(50)))
	dbMock.ExpectExec("INSERT INTO core_banking.transakcija").WillReturnResult(sqlmock.NewResult(1, 1))
	repo.On("UpdateOptionContractStatus", ctx, int64(111), "neg1", "EXERCISED", mock.Anything).Return(nil)
	repo.On("UpdateTransactionStatus", ctx, int64(20), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)
	dbMock.ExpectCommit()

	err := e.Commit(ctx, 20)
	require.NoError(t, err)
	require.NoError(t, dbMock.ExpectationsWereMet())
	repo.AssertCalled(t, "GetOptionContract", ctx, int64(111), "neg1")
}

// Aktuari/zaposleni nemaju lični račun — trguju preko bankinog POSLOVNI računa.
// Kada se PERSON odnosi na aktuara bez ličnog računa, razrešavanje mora da padne
// nazad na bankin POSLOVNI račun u traženoj valuti (inače ceo interbank OTC tok
// — premija, strike, isporuka akcija — pada za supervizora/agenta).
func TestResolvePersonAccount_ActuaryFallsBackToBusinessAccount(t *testing.T) {
	db, mock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 265, "265")
	ctx := context.Background()

	cols := []string{"broj_racuna", "stanje_racuna", "rezervisana_sredstva", "valuta_oznaka"}
	// 1) Aktuar nema lični račun (id_vlasnika lookup prazan).
	mock.ExpectQuery("r.id_vlasnika").WillReturnRows(mock.NewRows(cols))
	// 2) Jeste aktuar.
	mock.ExpectQuery("actuary_info").WillReturnRows(mock.NewRows([]string{"exists"}).AddRow(true))
	// 3) Bankin POSLOVNI USD račun.
	mock.ExpectQuery("vrsta_racuna").WillReturnRows(
		mock.NewRows(cols).AddRow("666000122200000008", "100000", "0", "USD"))

	acc, found := e.resolvePersonAccount(ctx, db, "3", "USD")
	if !found || acc.BrojRacuna != "666000122200000008" {
		t.Fatalf("expected bank POSLOVNI account, got found=%v acc=%+v", found, acc)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

// Klijent sa ličnim računom: ponašanje NEPROMENJENO (bez actuary_info/POSLOVNI upita).
func TestResolvePersonAccount_ClientUsesOwnAccount(t *testing.T) {
	db, mock := newGormDB(t)
	e := NewLocalTransactionExecutor(db, &mockInterbankRepoForExecutor{}, 265, "265")
	ctx := context.Background()

	mock.ExpectQuery("r.id_vlasnika").WillReturnRows(
		personAccountRows(mock, "666000121453782013", "5000", "0"))

	acc, found := e.resolvePersonAccount(ctx, db, "4", "USD")
	if !found || acc.BrojRacuna != "666000121453782013" {
		t.Fatalf("expected client own account, got found=%v acc=%+v", found, acc)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

// Kada aktuar (bez ličnog računa) kupi akcije kroz OTC exercise, sintetički DONE BUY
// nalog mora da se kreira na bankin POSLOVNI račun (inače se akcije ne pojave u
// portfoliju zaposlenog).
func TestCommit_BuyerStock_ActuaryGetsSharesViaBusinessAccount(t *testing.T) {
	db, dbMock := newGormDB(t)
	repo := &mockInterbankRepoForExecutor{}
	e := NewLocalTransactionExecutor(db, repo, 265, "265")
	ctx := context.Background()

	fid := "3" // aktuar (employee_id 3) — nema lični račun
	tkr := "GOOG"
	resv := domain.InterbankReservation{
		InterbankTransactionID: 90,
		AccountKind:            domain.AccountKindPerson,
		AssetType:              domain.AssetTypeStock,
		Amount:                 decimal.NewFromInt(6),
		ForeignID:              &fid,
		AssetTicker:            &tkr,
	}
	repo.On("ListReservationsByTx", ctx, int64(90)).Return([]domain.InterbankReservation{resv}, nil)
	repo.On("UpdateTransactionStatus", ctx, int64(90), domain.TxStatusCommitted, "COMMITTED", "").Return(nil)

	dbMock.ExpectBegin()
	dbMock.ExpectQuery("FROM core_banking.listing WHERE ticker").
		WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(7)))
	// Aktuar nema lični račun.
	dbMock.ExpectQuery("id_vlasnika").WillReturnRows(dbMock.NewRows([]string{"id"}))
	dbMock.ExpectQuery("actuary_info").WillReturnRows(dbMock.NewRows([]string{"exists"}).AddRow(true))
	// Bankin POSLOVNI račun.
	dbMock.ExpectQuery("vrsta_racuna").WillReturnRows(dbMock.NewRows([]string{"id"}).AddRow(int64(4)))
	dbMock.ExpectExec("INSERT INTO core_banking.orders").WillReturnResult(sqlmock.NewResult(1, 1))
	dbMock.ExpectCommit()

	if err := e.Commit(ctx, 90); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	require.NoError(t, dbMock.ExpectationsWereMet())
}
