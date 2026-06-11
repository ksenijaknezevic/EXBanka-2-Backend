package repository

import (
	"context"
	"testing"

	tradingworker "banka-backend/services/bank-service/internal/trading/worker"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// ReducePublicSharesAfterSell keeps a seller's OTC-published shares (public_shares)
// in sync with their holdings after a SELL fill: it FIFO-decrements the published
// quantity by the amount just sold, capped at what is currently published.

func TestReducePublicSharesAfterSell_ClientPartialReduce(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), true)

	// Client published 9 shares of listing 7; one row.
	mock.ExpectQuery("SELECT id, quantity FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int64(9)))
	// Sold 8 → row drops 9 → 1 (UPDATE, not DELETE; CHECK quantity > 0).
	mock.ExpectExec("UPDATE core_banking.public_shares SET quantity").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ReducePublicSharesAfterSell(ctx, 42, 7, 8); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestReducePublicSharesAfterSell_OversellCapsWithoutError(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), true)

	// Published only 9, but 12 were sold (e.g. some shares were never published).
	mock.ExpectQuery("SELECT id, quantity FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int64(9)))
	// Whole row consumed → DELETE. Remaining 3 has nothing left to take → no error.
	mock.ExpectExec("DELETE FROM core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ReducePublicSharesAfterSell(ctx, 42, 7, 12); err != nil {
		t.Fatalf("oversell must not error (soft cap), got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestReducePublicSharesAfterSell_FIFOAcrossRows(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), true)

	// Two published lots: oldest 2, then 5. Sell 3 → oldest fully consumed (DELETE),
	// then 1 taken from the next (UPDATE 5 → 4).
	mock.ExpectQuery("SELECT id, quantity FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).
			AddRow(int64(1), int64(2)).
			AddRow(int64(2), int64(5)))
	mock.ExpectExec("DELETE FROM core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE core_banking.public_shares SET quantity").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ReducePublicSharesAfterSell(ctx, 42, 7, 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestReducePublicSharesAfterSell_EmployeePooledScope(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), false) // employee/actuary

	// An employee sells from the shared actuary portfolio → reduction must span the
	// pooled actuary rows (actuary_info), mirroring the employee portfolio aggregation,
	// not just the selling user's own rows.
	mock.ExpectQuery("actuary_info").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(3), int64(5)))
	mock.ExpectExec("DELETE FROM core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ReducePublicSharesAfterSell(ctx, 99, 7, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}
