package worker

import (
	"context"
	"log"
	"time"

	"banka-backend/services/bank-service/internal/domain"
)

// OfferExpiryRepo je uski deo OTCRepository potreban ovom workeru (lakše testiranje).
type OfferExpiryRepo interface {
	ExpireStalePendingOffers(ctx context.Context, inactivityCutoff time.Time) ([]domain.OTCOffer, error)
}

// OTCOfferExpiryWorker jednom dnevno prevodi PENDING ponude neaktivne duže od ttl
// (ili sa prošlim settlement_date) u status EXPIRED i obaveštava obe strane.
type OTCOfferExpiryWorker struct {
	repo     OfferExpiryRepo
	notifier OTCNotifier
	ttl      time.Duration
}

func NewOTCOfferExpiryWorker(repo OfferExpiryRepo, notifier OTCNotifier, ttl time.Duration) *OTCOfferExpiryWorker {
	return &OTCOfferExpiryWorker{repo: repo, notifier: notifier, ttl: ttl}
}

// Start blokira dok se ctx ne otkaže. Prva provera 1 min po startu, zatim svakih 24h.
func (w *OTCOfferExpiryWorker) Start(ctx context.Context) {
	log.Printf("[worker] OTCOfferExpiryWorker started (ttl=%s, interval≈24h)", w.ttl)
	t := time.NewTimer(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[worker] OTCOfferExpiryWorker stopped")
			return
		case <-t.C:
			w.run(ctx)
			t.Reset(24 * time.Hour)
		}
	}
}

func (w *OTCOfferExpiryWorker) run(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-w.ttl)
	expired, err := w.repo.ExpireStalePendingOffers(ctx, cutoff)
	if err != nil {
		log.Printf("[worker] OTCOfferExpiry: greška pri isteku ponuda: %v", err)
		return
	}
	for _, o := range expired {
		w.notifier.NotifyOfferExpired(o)
	}
	if len(expired) > 0 {
		log.Printf("[worker] OTCOfferExpiry: %d ponuda(e) označeno kao EXPIRED", len(expired))
	}
}
