package repository

// Regression guard: ListNegotiations mora vraćati pregovore u kojima je naša banka
// STRANA (kupac ili prodavac), uključujući buyer-side (koje hostuje druga banka).
// Raniji bug: filtriralo se samo po negotiation_routing_number = naš → buyer-side
// (neg_rn = strani) je ispadao, pa korisnik nije video kontraponude.

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"banka-backend/services/bank-service/internal/domain"
)

func newRepoGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	require.NoError(t, err)
	return gdb, mock
}

func TestListNegotiations_PartyBased_ReturnsBuyerSide(t *testing.T) {
	db, mock := newRepoGormDB(t)
	repo := NewInterbankRepository(db)
	ctx := context.Background()
	uid := "4"

	cols := []string{
		"id", "negotiation_routing_number", "negotiation_foreign_id", "stock_ticker", "settlement_date",
		"price_currency", "price_amount", "premium_currency", "premium_amount", "amount",
		"buyer_routing_number", "buyer_id", "seller_routing_number", "seller_id",
		"last_modified_routing_number", "last_modified_id", "is_ongoing", "status", "created_at", "updated_at",
	}
	now := time.Now()
	// Buyer-side red: pregovor hostuje banka 222, ali smo MI (265) kupac.
	rows := sqlmock.NewRows(cols).AddRow(
		int64(1), int64(222), "c6f539e7", "AAPL", now,
		"USD", "100", "USD", "5", int32(10),
		int64(265), "4", int64(222), "C-2",
		int64(222), "C-2", true, "OPEN", now, now,
	)
	// Query MORA imati party-based BAZU: "... OR seller_routing_number = ..." (van
	// per-strana AND klauzule). Host-based baza (negotiation_routing_number = ?) ovo
	// ne sadrži, pa bi pala — što je tačno regresija koju čuvamo.
	mock.ExpectQuery("OR seller_routing_number =").WillReturnRows(rows)

	out, err := repo.ListNegotiations(ctx, domain.ListInterbankNegotiationsFilter{
		OurRoutingNumber: 265,
		ClientUserID:     &uid,
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, int64(222), out[0].NegotiationRoutingNumber) // host = druga banka
	assert.Equal(t, int64(265), out[0].BuyerRoutingNumber)       // mi smo kupac
	assert.Equal(t, "4", out[0].BuyerID)
	require.NoError(t, mock.ExpectationsWereMet())
}
