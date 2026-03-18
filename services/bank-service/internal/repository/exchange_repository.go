// exchange_repository.go — atomic exchange transfer execution.
//
// Implements domain.ExchangeTransferRepository. Reuses racunModel and
// transakcijaModel from account_repository.go (same package).
package repository

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type exchangeTransferRepository struct {
	db *gorm.DB
}

// NewExchangeTransferRepository creates a new ExchangeTransferRepository.
func NewExchangeTransferRepository(db *gorm.DB) domain.ExchangeTransferRepository {
	return &exchangeTransferRepository{db: db}
}

// exchangeAccountInfo is the pre-flight read projection (no lock).
type exchangeAccountInfo struct {
	ID                  int64   `gorm:"column:id"`
	BrojRacuna          string  `gorm:"column:broj_racuna"`
	IDVlasnika          int64   `gorm:"column:id_vlasnika"`
	StanjeRacuna        float64 `gorm:"column:stanje_racuna"`
	RezervovanaSredstva float64 `gorm:"column:rezervisana_sredstva"`
	Status              string  `gorm:"column:status"`
	ValutaOznaka        string  `gorm:"column:valuta_oznaka"`
}

// fetchAccountInfo queries a single account with its currency oznaka.
func (r *exchangeTransferRepository) fetchAccountInfo(ctx context.Context, db *gorm.DB, accountID int64) (*exchangeAccountInfo, error) {
	var row exchangeAccountInfo
	err := db.WithContext(ctx).Raw(`
		SELECT
			ra.id,
			ra.broj_racuna,
			ra.id_vlasnika,
			ra.stanje_racuna,
			ra.rezervisana_sredstva,
			ra.status,
			v.oznaka AS valuta_oznaka
		FROM core_banking.racun ra
		JOIN core_banking.valuta v ON v.id = ra.id_valute
		WHERE ra.id = ?
	`, accountID).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		return nil, domain.ErrAccountNotFound
	}
	return &row, nil
}

// ExecuteTransfer validates both accounts and atomically debits source + credits target.
//
// Validation order:
//  1. Account existence (pre-flight, unlocked)
//  2. Ownership (both accounts must belong to VlasnikID)
//  3. Active status
//  4. Currency match (src.ValutaOznaka == input.FromOznaka, tgt == ToOznaka)
//  5. Available funds (optimistic pre-check, then re-checked under lock)
//
// Execution (within a DB transaction with SELECT FOR UPDATE):
//   - Locks both accounts in deterministic id ASC order (prevents deadlock)
//   - Re-validates available funds after acquiring lock
//   - Debits source by input.Amount
//   - Credits target by conversion.Result (net amount)
//   - Inserts ISPLATA transakcija on source, UPLATA on target
func (r *exchangeTransferRepository) ExecuteTransfer(
	ctx context.Context,
	input domain.ExchangeTransferInput,
	conversion domain.ExchangeConversionResult,
) (*domain.ExchangeTransferResult, error) {

	// ── Pre-flight validation (no lock) ──────────────────────────────────────
	src, err := r.fetchAccountInfo(ctx, r.db, input.SourceAccountID)
	if err != nil {
		return nil, err
	}
	tgt, err := r.fetchAccountInfo(ctx, r.db, input.TargetAccountID)
	if err != nil {
		return nil, err
	}

	if src.IDVlasnika != input.VlasnikID {
		return nil, domain.ErrExchangeAccountNotOwned
	}
	if tgt.IDVlasnika != input.VlasnikID {
		return nil, domain.ErrExchangeAccountNotOwned
	}
	if src.Status != "AKTIVAN" {
		return nil, domain.ErrExchangeAccountInactive
	}
	if tgt.Status != "AKTIVAN" {
		return nil, domain.ErrExchangeAccountInactive
	}
	if src.ValutaOznaka != input.FromOznaka {
		return nil, domain.ErrExchangeWrongCurrency
	}
	if tgt.ValutaOznaka != input.ToOznaka {
		return nil, domain.ErrExchangeWrongCurrency
	}
	if src.StanjeRacuna-src.RezervovanaSredstva < input.Amount {
		return nil, domain.ErrExchangeInsufficientFunds
	}

	// ── Atomic execution ─────────────────────────────────────────────────────
	var result *domain.ExchangeTransferResult

	txErr := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock both accounts in deterministic id ASC order to prevent deadlock.
		first, second := input.SourceAccountID, input.TargetAccountID
		if input.TargetAccountID < input.SourceAccountID {
			first, second = input.TargetAccountID, input.SourceAccountID
		}

		var locked []racunModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id IN (?, ?)", first, second).
			Order("id ASC").
			Find(&locked).Error; err != nil {
			return err
		}
		if len(locked) != 2 {
			return domain.ErrAccountNotFound
		}

		// Identify source and target from locked rows.
		var srcLocked, tgtLocked *racunModel
		for i := range locked {
			switch locked[i].ID {
			case input.SourceAccountID:
				srcLocked = &locked[i]
			case input.TargetAccountID:
				tgtLocked = &locked[i]
			}
		}
		if srcLocked == nil || tgtLocked == nil {
			return domain.ErrAccountNotFound
		}

		// Re-validate funds after acquiring lock.
		availableNow := srcLocked.StanjeRacuna - srcLocked.RezervovanaSredstva
		if availableNow < input.Amount {
			return domain.ErrExchangeInsufficientFunds
		}

		now := time.Now().UTC()
		opis := fmt.Sprintf(
			"Konverzija: %.4g %s → %.4g %s",
			input.Amount, input.FromOznaka,
			conversion.Result, input.ToOznaka,
		)

		// Debit source account.
		if err := tx.Model(&racunModel{}).
			Where("id = ?", input.SourceAccountID).
			Update("stanje_racuna", gorm.Expr("stanje_racuna - ?", input.Amount)).Error; err != nil {
			return fmt.Errorf("debit source: %w", err)
		}

		// Credit target account.
		if err := tx.Model(&racunModel{}).
			Where("id = ?", input.TargetAccountID).
			Update("stanje_racuna", gorm.Expr("stanje_racuna + ?", conversion.Result)).Error; err != nil {
			return fmt.Errorf("credit target: %w", err)
		}

		// Insert ISPLATA transakcija on source account.
		srcTx := &transakcijaModel{
			RacunID:          input.SourceAccountID,
			TipTransakcije:   "ISPLATA",
			Iznos:            input.Amount,
			Opis:             opis,
			VremeIzvrsavanja: now,
			Status:           "IZVRSEN",
		}
		if err := tx.Create(srcTx).Error; err != nil {
			return fmt.Errorf("insert source transaction: %w", err)
		}

		// Insert UPLATA transakcija on target account.
		tgtTx := &transakcijaModel{
			RacunID:          input.TargetAccountID,
			TipTransakcije:   "UPLATA",
			Iznos:            conversion.Result,
			Opis:             opis,
			VremeIzvrsavanja: now,
			Status:           "IZVRSEN",
		}
		if err := tx.Create(tgtTx).Error; err != nil {
			return fmt.Errorf("insert target transaction: %w", err)
		}

		referenceID := fmt.Sprintf("KNV-%s-%06d",
			now.Format("20060102150405"),
			100000+rand.Intn(900000),
		)

		result = &domain.ExchangeTransferResult{
			ReferenceID:     referenceID,
			SourceAccountID: input.SourceAccountID,
			TargetAccountID: input.TargetAccountID,
			FromOznaka:      input.FromOznaka,
			ToOznaka:        input.ToOznaka,
			OriginalAmount:  input.Amount,
			GrossAmount:     conversion.Bruto,
			Provizija:       conversion.Provizija,
			NetAmount:       conversion.Result,
			ViaRSD:          conversion.ViaRSD,
			RateNote:        conversion.RateNote,
		}
		return nil
	})

	if txErr != nil {
		return nil, txErr
	}
	return result, nil
}
