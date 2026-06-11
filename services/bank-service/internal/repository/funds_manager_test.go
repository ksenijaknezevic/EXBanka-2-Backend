package repository

import (
	"context"
	"testing"

	tradingworker "banka-backend/services/bank-service/internal/trading/worker"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// ClampPublicSharesToHoldings caps a holder's OTC-published shares (public_shares) at
// what they still own after a sale: it removes ONLY the amount that now exceeds
// holdings (FIFO). Selling shares that were never published does not reduce the public
// count — published stays put until holdings drop below it.

func TestClampPublicSharesToHoldings_NoChangeWhenWithinHoldings(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), true)

	// Owns 7, published 5 → 5 ≤ 7, nothing to trim (no UPDATE/DELETE).
	mock.ExpectQuery("FROM core_banking.orders").
		WillReturnRows(sqlmock.NewRows([]string{"net"}).AddRow(int64(7)))
	mock.ExpectQuery("FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int64(5)))

	if err := f.ClampPublicSharesToHoldings(ctx, 42, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestClampPublicSharesToHoldings_TrimsExcessToHoldings(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), true)

	// Owned dropped to 4, published 5 → trim the 1 excess (5 → 4) via UPDATE.
	mock.ExpectQuery("FROM core_banking.orders").
		WillReturnRows(sqlmock.NewRows([]string{"net"}).AddRow(int64(4)))
	mock.ExpectQuery("FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int64(5)))
	mock.ExpectExec("UPDATE core_banking.public_shares SET quantity").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ClampPublicSharesToHoldings(ctx, 42, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestClampPublicSharesToHoldings_RemovesAllWhenSoldOut(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), true)

	// Sold everything → owned 0, published 5 → delete the row (CHECK quantity > 0).
	// Guards the sell-all-then-rebuy case: published must not resurrect after rebuy.
	mock.ExpectQuery("FROM core_banking.orders").
		WillReturnRows(sqlmock.NewRows([]string{"net"}).AddRow(int64(0)))
	mock.ExpectQuery("FROM core_banking.public_shares").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(1), int64(5)))
	mock.ExpectExec("DELETE FROM core_banking.public_shares").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ClampPublicSharesToHoldings(ctx, 42, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestClampPublicSharesToHoldings_EmployeePooledScope(t *testing.T) {
	gormDB, mock := newRepoGormDB(t)
	f := &fundsManager{db: gormDB}
	ctx := tradingworker.WithIsClient(context.Background(), false) // employee/actuary

	// Pooled actuary holdings + public_shares — both queries must span actuary_info.
	// Owned 2, published 5 → trim 3 (5 → 2) via UPDATE.
	mock.ExpectQuery("actuary_info").
		WillReturnRows(sqlmock.NewRows([]string{"net"}).AddRow(int64(2)))
	mock.ExpectQuery("actuary_info").
		WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(int64(9), int64(5)))
	mock.ExpectExec("UPDATE core_banking.public_shares SET quantity").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := f.ClampPublicSharesToHoldings(ctx, 99, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}
