package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/stretchr/testify/assert"
)

type fakeOfferExpiryRepo struct {
	gotCutoff time.Time
	called    int
	ret       []domain.OTCOffer
	err       error
}

func (f *fakeOfferExpiryRepo) ExpireStalePendingOffers(ctx context.Context, cutoff time.Time) ([]domain.OTCOffer, error) {
	f.called++
	f.gotCutoff = cutoff
	return f.ret, f.err
}

type fakeNotifier struct {
	*NoOpOTCNotifier // ostale metode = no-op
	expired          []domain.OTCOffer
}

func (f *fakeNotifier) NotifyOfferExpired(offer domain.OTCOffer) {
	f.expired = append(f.expired, offer)
}

func TestOTCOfferExpiry_NotifiesEachExpiredOffer(t *testing.T) {
	repo := &fakeOfferExpiryRepo{ret: []domain.OTCOffer{{ID: 1}, {ID: 2}}}
	notif := &fakeNotifier{}
	w := NewOTCOfferExpiryWorker(repo, notif, 7*24*time.Hour)

	w.run(context.Background())

	assert.Equal(t, 1, repo.called)
	assert.Len(t, notif.expired, 2)
	assert.Equal(t, int64(1), notif.expired[0].ID)
	assert.Equal(t, int64(2), notif.expired[1].ID)
	assert.WithinDuration(t, time.Now().UTC().Add(-7*24*time.Hour), repo.gotCutoff, time.Minute)
}

func TestOTCOfferExpiry_NoOffers_NoNotifications(t *testing.T) {
	repo := &fakeOfferExpiryRepo{ret: nil}
	notif := &fakeNotifier{}
	w := NewOTCOfferExpiryWorker(repo, notif, 7*24*time.Hour)

	w.run(context.Background())

	assert.Empty(t, notif.expired)
}

func TestOTCOfferExpiry_RepoError_NoNotificationsNoPanic(t *testing.T) {
	repo := &fakeOfferExpiryRepo{err: errors.New("db down")}
	notif := &fakeNotifier{}
	w := NewOTCOfferExpiryWorker(repo, notif, 7*24*time.Hour)

	w.run(context.Background())

	assert.Empty(t, notif.expired)
}
