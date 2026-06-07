package worker

import (
	"context"
	"log"
	"strconv"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// InterbankExpiryRepo — uži pogled na repo (za testabilnost).
type InterbankExpiryRepo interface {
	ListExpiredActiveContracts(ctx context.Context, before time.Time) ([]domain.InterbankOptionContract, error)
	UpdateOptionContractStatus(ctx context.Context, routingNumber int64, foreignID, status string, usedAt *time.Time) error
}

// ShareReleaser — vraća blokirane akcije u javni režim (executor.ReleaseShares).
type ShareReleaser interface {
	ReleaseShares(ctx context.Context, userID int64, ticker string, amount int32) error
}

// InterbankOptionExpiryWorker zatvara istekle (settlementDate prošao) ACTIVE
// inter-bank opcione ugovore. Kada smo MI banka-prodavac, prodavčeve blokirane
// akcije se vraćaju u javni režim; premija OSTAJE prodavcu (nikad refund) —
// si-tx-proto §S10.
type InterbankOptionExpiryWorker struct {
	repo       InterbankExpiryRepo
	shares     ShareReleaser
	ourRouting int64
}

func NewInterbankOptionExpiryWorker(repo InterbankExpiryRepo, shares ShareReleaser, ourRouting int64) *InterbankOptionExpiryWorker {
	return &InterbankOptionExpiryWorker{repo: repo, shares: shares, ourRouting: ourRouting}
}

// ExpireDue obrađuje sve ugovore istekle do `now`. Vraća broj obrađenih ugovora.
func (w *InterbankOptionExpiryWorker) ExpireDue(ctx context.Context, now time.Time) (int, error) {
	contracts, err := w.repo.ListExpiredActiveContracts(ctx, now)
	if err != nil {
		return 0, err
	}
	for _, c := range contracts {
		// Samo kad smo MI prodavac držimo blokirane akcije → vraćamo ih.
		if c.SellerRoutingNumber == w.ourRouting {
			if uid, perr := strconv.ParseInt(c.SellerID, 10, 64); perr == nil {
				if rerr := w.shares.ReleaseShares(ctx, uid, c.StockTicker, c.Amount); rerr != nil {
					log.Printf("[worker] InterbankExpiry: oslobađanje akcija (neg %s) nije uspelo: %v — ne zatvaram ugovor",
						c.NegotiationForeignID, rerr)
					continue // ne markiraj EXPIRED ako akcije nisu vraćene (retry sledeći ciklus)
				}
			}
		}
		if uerr := w.repo.UpdateOptionContractStatus(ctx, c.NegotiationRoutingNumber, c.NegotiationForeignID, "EXPIRED", &now); uerr != nil {
			log.Printf("[worker] InterbankExpiry: status EXPIRED (neg %s) nije upisan: %v", c.NegotiationForeignID, uerr)
		}
	}
	return len(contracts), nil
}

// Start blokira dok se ctx ne otkaže. Prva provera 1 min posle starta, zatim 24h.
func (w *InterbankOptionExpiryWorker) Start(ctx context.Context) {
	log.Printf("[worker] InterbankOptionExpiryWorker started (interval≈24h)")
	t := time.NewTimer(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[worker] InterbankOptionExpiryWorker stopped")
			return
		case <-t.C:
			if n, err := w.ExpireDue(ctx, time.Now().UTC()); err != nil {
				log.Printf("[worker] InterbankExpiry: greška: %v", err)
			} else if n > 0 {
				log.Printf("[worker] InterbankExpiry: %d istekao/la inter-bank ugovor(a) zatvoreno", n)
			}
			t.Reset(24 * time.Hour)
		}
	}
}
