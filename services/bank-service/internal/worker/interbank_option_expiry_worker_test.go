package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"banka-backend/services/bank-service/internal/domain"
)

type fakeExpiryRepo struct {
	contracts []domain.InterbankOptionContract
	expired   []string // foreignID-ovi označeni EXPIRED
}

func (f *fakeExpiryRepo) ListExpiredActiveContracts(ctx context.Context, before time.Time) ([]domain.InterbankOptionContract, error) {
	return f.contracts, nil
}

func (f *fakeExpiryRepo) UpdateOptionContractStatus(ctx context.Context, routingNumber int64, foreignID, status string, usedAt *time.Time) error {
	if status == "EXPIRED" {
		f.expired = append(f.expired, foreignID)
	}
	return nil
}

type fakeReleaser struct {
	calls []string // "userID:ticker:amount"
}

func (f *fakeReleaser) ReleaseShares(ctx context.Context, userID int64, ticker string, amount int32) error {
	f.calls = append(f.calls, fmt.Sprintf("%d:%s:%d", userID, ticker, amount))
	return nil
}

// Kad smo MI banka-prodavac: po isteku se prodavčeve blokirane akcije VRAĆAJU
// i ugovor se zatvara (EXPIRED). Premija ostaje prodavcu (ništa se ne refundira).
func TestInterbankExpiry_SellerSide_ReleasesAndExpires(t *testing.T) {
	repo := &fakeExpiryRepo{contracts: []domain.InterbankOptionContract{{
		NegotiationRoutingNumber: 265, NegotiationForeignID: "neg1",
		SellerRoutingNumber: 265, SellerID: "2", StockTicker: "AAPL", Amount: 5, Status: "ACTIVE",
	}}}
	rel := &fakeReleaser{}
	w := NewInterbankOptionExpiryWorker(repo, rel, 265)

	n, err := w.ExpireDue(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []string{"2:AAPL:5"}, rel.calls, "prodavčeve akcije moraju biti vraćene")
	assert.Equal(t, []string{"neg1"}, repo.expired, "ugovor mora biti EXPIRED")
}

// Kad smo MI kupac (prodavac je druga banka): ništa ne oslobađamo lokalno
// (akcije drži banka-prodavac), samo zatvaramo ugovor.
func TestInterbankExpiry_BuyerSide_ExpiresWithoutRelease(t *testing.T) {
	repo := &fakeExpiryRepo{contracts: []domain.InterbankOptionContract{{
		NegotiationRoutingNumber: 222, NegotiationForeignID: "neg2",
		SellerRoutingNumber: 222, SellerID: "C-2", StockTicker: "AMZN", Amount: 4, Status: "ACTIVE",
	}}}
	rel := &fakeReleaser{}
	w := NewInterbankOptionExpiryWorker(repo, rel, 265)

	_, err := w.ExpireDue(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Empty(t, rel.calls, "ne vraćamo akcije kad nismo prodavac")
	assert.Equal(t, []string{"neg2"}, repo.expired)
}
