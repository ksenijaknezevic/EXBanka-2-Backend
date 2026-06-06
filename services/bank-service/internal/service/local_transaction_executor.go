// Package service — local_transaction_executor.go
//
// Validira i izvršava lokalni deo Transaction objekta po protokolu si-tx-proto.
// Jedan executor radi nad lokalnom GORM bazom (core_banking schema).
//
// Glavni delovi:
//
//	Prepare  — proveri balansiranost, postojeće račune, dovoljnost sredstava
//	           i rezerviše ih (rezervisana_sredstva ili public_shares).
//	Commit   — primeni rezervaciju kao stvarnu promenu balansa, knjiži u
//	           core_banking.transakcija (tip 'INTERBANK').
//	Rollback — oslobodi rezervisane resurse.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"banka-backend/services/bank-service/internal/domain"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// ExchangeRateSource — izvor kursne liste za auto-konverziju (menjačnica) kada
// korisnik nema račun u valuti dogovora. Zadovoljava ga *exchangeService.
type ExchangeRateSource interface {
	GetRates(ctx context.Context) ([]domain.ExchangeRate, error)
}

// LocalTransactionExecutor — zavisnost za Coordinator i /interbank handler.
type LocalTransactionExecutor struct {
	db            *gorm.DB
	repo          domain.InterbankRepository
	ourRouting    int64
	accountPrefix string             // prve 3 cifre brojeva računa naše banke
	rates         ExchangeRateSource // opciono: za PERSON+MONAS auto-konverziju (nil = bez konverzije)
}

// SetExchangeRates uključuje menjačnica auto-konverziju (kupovni/prodajni) za
// PERSON+MONAS poravnanje kada korisnik nema račun u valuti dogovora.
func (e *LocalTransactionExecutor) SetExchangeRates(src ExchangeRateSource) {
	e.rates = src
}

// NewLocalTransactionExecutor konstruktor.
//
// accountPrefix — npr. "265" — koristi se da se prepozna da li ACCOUNT-num
// pripada našoj banci. Ako je prazan, prefiks se izvodi iz routingNumber-a.
func NewLocalTransactionExecutor(
	db *gorm.DB,
	repo domain.InterbankRepository,
	ourRoutingNumber int64,
	accountPrefix string,
) *LocalTransactionExecutor {
	if accountPrefix == "" {
		accountPrefix = strconv.FormatInt(ourRoutingNumber, 10)
	}
	return &LocalTransactionExecutor{
		db:            db,
		repo:          repo,
		ourRouting:    ourRoutingNumber,
		accountPrefix: accountPrefix,
	}
}

// ─── Prepare ─────────────────────────────────────────────────────────────────

// Prepare validira transakciju po protokolu i rezerviše lokalne resurse.
// Vraća TransactionVote (YES ili NO + razlozi). Lokalno upisuje
// interbank_reservation entry-je po posting-u (samo za one koji se tiču nas).
func (e *LocalTransactionExecutor) Prepare(ctx context.Context, ibTxID int64, tx domain.Transaction) (domain.TransactionVote, error) {
	// 1) Balansiranost: zbir amount-a po (asset, currency/ticker) mora biti 0.
	//    Posting amount > 0 = credit (skida sa "od koga"), < 0 = debit?
	//    Po protokolu: credit/debit zaviste od znaka. Mi proveravamo grupisani sumiranje.
	if !isBalanced(tx.Postings) {
		return domain.TransactionVote{
			Vote:    domain.VoteNo,
			Reasons: []domain.NoVoteReason{{Reason: domain.NoReasonUnbalancedTx}},
		}, nil
	}

	noReasons := []domain.NoVoteReason{}

	// 2) Iteriraj kroz svaki posting i validiraj samo one koji su lokalni.
	for _, p := range tx.Postings {
		local, err := e.isLocalPosting(p)
		if err != nil {
			return domain.TransactionVote{}, err
		}
		if !local {
			continue
		}
		if reason := e.validatePosting(ctx, p); reason != nil {
			r := *reason
			r.Posting = pCopy(p)
			noReasons = append(noReasons, r)
		}
	}

	if len(noReasons) > 0 {
		return domain.TransactionVote{Vote: domain.VoteNo, Reasons: noReasons}, nil
	}

	// 3) Rezerviši resurse u jednoj DB transakciji.
	err := e.db.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		for i, p := range tx.Postings {
			local, _ := e.isLocalPosting(p)
			if !local {
				continue
			}
			if err := e.reservePosting(ctx, dbTx, ibTxID, i, p); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.TransactionVote{
			Vote:    domain.VoteNo,
			Reasons: []domain.NoVoteReason{{Reason: domain.NoReasonInsufficientAsset}},
		}, nil
	}

	return domain.TransactionVote{Vote: domain.VoteYes}, nil
}

// isLocalPosting odlučuje da li ovaj posting tretira ova banka.
func (e *LocalTransactionExecutor) isLocalPosting(p domain.Posting) (bool, error) {
	switch p.Account.Type {
	case domain.AccountKindAccount:
		if p.Account.Num == nil {
			return false, fmt.Errorf("ACCOUNT posting bez num polja")
		}
		num := *p.Account.Num
		if len(num) >= len(e.accountPrefix) && num[:len(e.accountPrefix)] == e.accountPrefix {
			return true, nil
		}
		return false, nil
	case domain.AccountKindPerson, domain.AccountKindOption:
		if p.Account.ID == nil {
			return false, fmt.Errorf("%s posting bez id polja", p.Account.Type)
		}
		return p.Account.ID.RoutingNumber == e.ourRouting, nil
	}
	return false, fmt.Errorf("nepoznat AccountKind: %s", p.Account.Type)
}

// validatePosting vraća NoVoteReason ako posting nije izvršiv lokalno; nil inače.
func (e *LocalTransactionExecutor) validatePosting(ctx context.Context, p domain.Posting) *domain.NoVoteReason {
	switch p.Account.Type {
	case domain.AccountKindAccount:
		// Proveri postojanje racuna i — ako je amount negativan (skidamo sa
		// ovog računa) — dovoljnost sredstava.
		var rac struct {
			ID                  int64           `gorm:"column:id"`
			ValutaOznaka        string          `gorm:"column:valuta_oznaka"`
			StanjeRacuna        decimal.Decimal `gorm:"column:stanje_racuna"`
			RezervovanaSredstva decimal.Decimal `gorm:"column:rezervisana_sredstva"`
			Status              string          `gorm:"column:status"`
		}
		err := e.db.WithContext(ctx).Raw(`
			SELECT r.id, v.oznaka AS valuta_oznaka, r.stanje_racuna, r.rezervisana_sredstva, r.status
			FROM core_banking.racun r
			JOIN core_banking.valuta v ON v.id = r.id_valute
			WHERE r.broj_racuna = ?
		`, *p.Account.Num).Scan(&rac).Error
		if err != nil || rac.ID == 0 || rac.Status != "AKTIVAN" {
			return &domain.NoVoteReason{Reason: domain.NoReasonNoSuchAccount}
		}
		// Provera asset tipa: za ACCOUNT podržavamo samo MONAS sa odgovarajućom valutom.
		if p.Asset.Type != domain.AssetTypeMonas || p.Asset.MonAs == nil {
			return &domain.NoVoteReason{Reason: domain.NoReasonUnacceptableAsset}
		}
		if p.Asset.MonAs.Currency != rac.ValutaOznaka {
			return &domain.NoVoteReason{Reason: domain.NoReasonUnacceptableAsset}
		}
		// Negativan amount = skidanje sa računa → mora biti dovoljno sredstava.
		if p.Amount.IsNegative() {
			abs := p.Amount.Abs()
			avail := rac.StanjeRacuna.Sub(rac.RezervovanaSredstva)
			if avail.LessThan(abs) {
				return &domain.NoVoteReason{Reason: domain.NoReasonInsufficientAsset}
			}
		}
		return nil

	case domain.AccountKindPerson:
		// PERSON: za MONAS razrešavamo stvarni račun (id_vlasnika + valuta) i
		// proveravamo sredstva za debit; za STOCK dovoljnost akcija u javnom režimu.
		switch p.Asset.Type {
		case domain.AssetTypeMonas:
			if p.Asset.MonAs == nil {
				return &domain.NoVoteReason{Reason: domain.NoReasonNoSuchAsset}
			}
			acc, found := e.resolvePersonAccount(ctx, e.db, p.Account.ID.ID, p.Asset.MonAs.Currency)
			if !found {
				return &domain.NoVoteReason{Reason: domain.NoReasonNoSuchAccount}
			}
			// Negativan amount = skidanje (premija/strike) → dovoljnost sredstava u
			// valuti računa (uz menjačnica konverziju ako se valute razlikuju).
			if p.Amount.IsNegative() {
				converted, cerr := e.convertForSettlement(ctx, p.Amount, p.Asset.MonAs.Currency, acc.Valuta)
				if cerr != nil {
					return &domain.NoVoteReason{Reason: domain.NoReasonUnacceptableAsset}
				}
				avail := acc.Stanje.Sub(acc.Rezervisana)
				if avail.LessThan(converted.Abs()) {
					return &domain.NoVoteReason{Reason: domain.NoReasonInsufficientAsset}
				}
			}
			return nil
		case domain.AssetTypeStock:
			if p.Asset.Stock == nil || p.Asset.Stock.Ticker == "" {
				return &domain.NoVoteReason{Reason: domain.NoReasonNoSuchAsset}
			}
			// Negativan amount na PERSON = prodavac mora imati toliko akcija u javnom režimu.
			if p.Amount.IsNegative() {
				userID, err := strconv.ParseInt(p.Account.ID.ID, 10, 64)
				if err != nil {
					return &domain.NoVoteReason{Reason: domain.NoReasonNoSuchAccount}
				}
				var qty int64
				e.db.WithContext(ctx).Raw(`
					SELECT COALESCE(SUM(ps.quantity), 0)
					FROM core_banking.public_shares ps
					JOIN core_banking.listing l ON l.id = ps.listing_id
					WHERE ps.user_id = ? AND l.ticker = ?
				`, userID, p.Asset.Stock.Ticker).Scan(&qty)
				want := p.Amount.Abs().IntPart()
				if qty < want {
					return &domain.NoVoteReason{Reason: domain.NoReasonInsufficientAsset}
				}
			}
			return nil
		case domain.AssetTypeOption:
			return nil
		}

	case domain.AccountKindOption:
		// OPTION pseudo-account.
		if p.Account.ID == nil {
			return &domain.NoVoteReason{Reason: domain.NoReasonOptionNegotiationNotFound}
		}
		// Izdavanje opcije (accept, §3.6): OPTION nalog + OPTION asset. Ugovor još
		// ne postoji (kreira se ACTIVE na 2PC commit-u) → ne tražimo postojeći
		// ACTIVE, samo nenulti amount. Exercise (MONAS/STOCK) zadržava staru proveru.
		if p.Asset.Type == domain.AssetTypeOption {
			if p.Amount.IsZero() {
				return &domain.NoVoteReason{Reason: domain.NoReasonOptionAmountIncorrect}
			}
			return nil
		}
		c, err := e.repo.GetOptionContract(ctx, p.Account.ID.RoutingNumber, p.Account.ID.ID)
		if err != nil || c == nil {
			return &domain.NoVoteReason{Reason: domain.NoReasonOptionNegotiationNotFound}
		}
		if c.Status != "ACTIVE" {
			return &domain.NoVoteReason{Reason: domain.NoReasonOptionUsedOrExpired}
		}
		if !c.SettlementDate.After(time.Now().UTC()) {
			return &domain.NoVoteReason{Reason: domain.NoReasonOptionUsedOrExpired}
		}
		// Amount validacija: |amount| mora biti deljiv sa valjanim brojem akcija
		// — pojednostavljeno: dozvoljavamo bilo koji nenulti decimal i stocks count
		// proveravamo na commit-u.
		if p.Amount.IsZero() {
			return &domain.NoVoteReason{Reason: domain.NoReasonOptionAmountIncorrect}
		}
		return nil
	}
	return nil
}

func (e *LocalTransactionExecutor) reservePosting(ctx context.Context, dbTx *gorm.DB, ibTxID int64, idx int, p domain.Posting) error {
	res := domain.InterbankReservation{
		InterbankTransactionID: ibTxID,
		PostingIndex:           idx,
		AccountKind:            p.Account.Type,
		Amount:                 p.Amount,
		AssetType:              p.Asset.Type,
	}
	switch p.Account.Type {
	case domain.AccountKindAccount:
		res.AccountNum = p.Account.Num
	case domain.AccountKindPerson, domain.AccountKindOption:
		rn := p.Account.ID.RoutingNumber
		fid := p.Account.ID.ID
		res.ForeignRoutingNumber = &rn
		res.ForeignID = &fid
	}
	switch p.Asset.Type {
	case domain.AssetTypeMonas:
		if p.Asset.MonAs != nil {
			c := p.Asset.MonAs.Currency
			res.AssetCurrency = &c
		}
	case domain.AssetTypeStock:
		if p.Asset.Stock != nil {
			t := p.Asset.Stock.Ticker
			res.AssetTicker = &t
		}
	case domain.AssetTypeOption:
		if p.Asset.Option != nil {
			rn := p.Asset.Option.NegotiationID.RoutingNumber
			fid := p.Asset.Option.NegotiationID.ID
			res.AssetNegotiationRouting = &rn
			res.AssetNegotiationLocalID = &fid
			t := p.Asset.Option.Stock.Ticker
			res.AssetTicker = &t
		}
	}

	// Stvarna rezervacija na racunu / public_shares.
	if p.Account.Type == domain.AccountKindAccount && p.Asset.Type == domain.AssetTypeMonas && p.Amount.IsNegative() {
		abs := p.Amount.Abs()
		if err := dbTx.Exec(`
			UPDATE core_banking.racun
			SET rezervisana_sredstva = rezervisana_sredstva + ?
			WHERE broj_racuna = ?
		`, abs, *p.Account.Num).Error; err != nil {
			return fmt.Errorf("rezervacija sredstava: %w", err)
		}
		res.Reserved = true
	}

	// PERSON + STOCK negativ: spustimo public_shares.quantity (FIFO).
	if p.Account.Type == domain.AccountKindPerson && p.Asset.Type == domain.AssetTypeStock && p.Amount.IsNegative() {
		userID, _ := strconv.ParseInt(p.Account.ID.ID, 10, 64)
		if err := decrementPublicShares(dbTx, userID, p.Asset.Stock.Ticker, p.Amount.Abs().IntPart()); err != nil {
			return err
		}
		res.Reserved = true
	}

	// PERSON + MONAS: razreši račun korisnika i konvertuj iznos u valutu računa
	// (menjačnica kupovni/prodajni) ako se razlikuje od valute dogovora. AccountNum
	// + konvertovani iznos se pamte da bi Commit/Rollback knjižili tačno u valuti računa.
	if p.Account.Type == domain.AccountKindPerson && p.Asset.Type == domain.AssetTypeMonas {
		acc, found := e.resolvePersonAccount(ctx, dbTx, p.Account.ID.ID, p.Asset.MonAs.Currency)
		if !found {
			return fmt.Errorf("rezervacija: aktivan račun korisnika %s nije pronađen", p.Account.ID.ID)
		}
		converted, cerr := e.convertForSettlement(ctx, p.Amount, p.Asset.MonAs.Currency, acc.Valuta)
		if cerr != nil {
			return fmt.Errorf("konverzija premije: %w", cerr)
		}
		res.AccountNum = &acc.BrojRacuna
		res.Amount = converted // knjiži se u valuti računa (posle konverzije)
		if converted.IsNegative() {
			if err := dbTx.Exec(`
				UPDATE core_banking.racun
				SET rezervisana_sredstva = rezervisana_sredstva + ?
				WHERE broj_racuna = ?
			`, converted.Abs(), acc.BrojRacuna).Error; err != nil {
				return fmt.Errorf("rezervacija premije: %w", err)
			}
			res.Reserved = true
		}
	}

	return e.repo.CreateReservation(ctx, &res)
}

// ─── Commit ──────────────────────────────────────────────────────────────────

// Commit primenjuje rezervacije: skida rezervisana_sredstva i smanjuje
// stanje_racuna; za STOCK postings: zapis o transferu vlasništva (nije
// modelovan u portfolio tabeli ovde — log se piše u transakcija). Knjiženja
// se upisuju u core_banking.transakcija sa tip='INTERBANK'.
func (e *LocalTransactionExecutor) Commit(ctx context.Context, ibTxID int64) error {
	resvs, err := e.repo.ListReservationsByTx(ctx, ibTxID)
	if err != nil {
		return err
	}

	return e.db.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		for _, r := range resvs {
			if (r.AccountKind == domain.AccountKindAccount || r.AccountKind == domain.AccountKindPerson) && r.AssetType == domain.AssetTypeMonas && r.AccountNum != nil {
				// Negativan iznos = skidanje, pozitivan = doplata.
				if r.Amount.IsNegative() {
					abs := r.Amount.Abs()
					if err := dbTx.Exec(`
						UPDATE core_banking.racun
						SET rezervisana_sredstva = rezervisana_sredstva - ?,
						    stanje_racuna        = stanje_racuna - ?
						WHERE broj_racuna = ?
					`, abs, abs, *r.AccountNum).Error; err != nil {
						return err
					}
					if err := writeTxRow(dbTx, *r.AccountNum, "INTERBANK", abs.Neg(), "Interbank ISPLATA"); err != nil {
						return err
					}
				} else if r.Amount.IsPositive() {
					if err := dbTx.Exec(`
						UPDATE core_banking.racun
						SET stanje_racuna = stanje_racuna + ?
						WHERE broj_racuna = ?
					`, r.Amount, *r.AccountNum).Error; err != nil {
						return err
					}
					if err := writeTxRow(dbTx, *r.AccountNum, "INTERBANK", r.Amount, "Interbank UPLATA"); err != nil {
						return err
					}
				}
			}
			// PERSON + STOCK pozitivni: kupac dobija akcije → idu u PORTFOLIO
			// (sintetički DONE BUY nalog, isto kao lokalni OTC exercise), NE u javni
			// režim. Korisnik ih kasnije sam objavljuje (/bank/portfolio/publish).
			if r.AccountKind == domain.AccountKindPerson && r.AssetType == domain.AssetTypeStock && r.Amount.IsPositive() {
				userID, _ := strconv.ParseInt(*r.ForeignID, 10, 64)
				ticker := *r.AssetTicker
				var listingID int64
				dbTx.Raw(`SELECT id FROM core_banking.listing WHERE ticker = ?`, ticker).Scan(&listingID)
				var accountID int64
				dbTx.Raw(`SELECT id FROM core_banking.racun WHERE id_vlasnika = ? AND status = 'AKTIVAN' ORDER BY id LIMIT 1`, userID).Scan(&accountID)
				if listingID > 0 && accountID > 0 {
					if err := dbTx.Exec(`
						INSERT INTO core_banking.orders
						  (user_id, account_id, listing_id, order_type, direction, quantity, contract_size,
						   status, is_done, remaining_portions, after_hours, all_or_none, margin, last_modified, created_at)
						VALUES (?, ?, ?, 'MARKET', 'BUY', ?, 1, 'DONE', TRUE, 0, FALSE, FALSE, FALSE, now(), now())
					`, userID, accountID, listingID, r.Amount.IntPart()).Error; err != nil {
						return err
					}
				}
			}
			// OPTION account na EXERCISE (MONAS/STOCK leg) → opcija iskorišćena.
			// Na IZDAVANJU (OPTION asset, accept) status se NE dira — ugovor se
			// kreira ACTIVE post-commit u AcceptNegotiation.
			if r.AccountKind == domain.AccountKindOption && r.AssetType != domain.AssetTypeOption {
				if err := e.repo.UpdateOptionContractStatus(ctx, *r.ForeignRoutingNumber, *r.ForeignID, "EXERCISED", ptrTime(time.Now().UTC())); err != nil {
					return err
				}
			}
		}
		return e.repo.UpdateTransactionStatus(ctx, ibTxID, domain.TxStatusCommitted, "COMMITTED", "")
	})
}

// ─── Rollback ────────────────────────────────────────────────────────────────

// Rollback oslobađa rezervisane resurse i postavlja status transakcije na ROLLED_BACK.
func (e *LocalTransactionExecutor) Rollback(ctx context.Context, ibTxID int64) error {
	resvs, err := e.repo.ListReservationsByTx(ctx, ibTxID)
	if err != nil {
		return err
	}
	return e.db.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		for _, r := range resvs {
			if !r.Reserved {
				continue
			}
			if (r.AccountKind == domain.AccountKindAccount || r.AccountKind == domain.AccountKindPerson) && r.AssetType == domain.AssetTypeMonas && r.AccountNum != nil && r.Amount.IsNegative() {
				abs := r.Amount.Abs()
				if err := dbTx.Exec(`
					UPDATE core_banking.racun
					SET rezervisana_sredstva = rezervisana_sredstva - ?
					WHERE broj_racuna = ?
				`, abs, *r.AccountNum).Error; err != nil {
					return err
				}
			}
			if r.AccountKind == domain.AccountKindPerson && r.AssetType == domain.AssetTypeStock && r.Amount.IsNegative() {
				// Vrati količinu u public_shares (kreiraj novi red kao snapshot).
				userID, _ := strconv.ParseInt(*r.ForeignID, 10, 64)
				ticker := *r.AssetTicker
				var listingID int64
				dbTx.Raw(`SELECT id FROM core_banking.listing WHERE ticker = ?`, ticker).Scan(&listingID)
				if listingID > 0 {
					if err := dbTx.Exec(`
						INSERT INTO core_banking.public_shares (listing_id, user_id, quantity)
						VALUES (?, ?, ?)
					`, listingID, userID, r.Amount.Abs().IntPart()).Error; err != nil {
						return err
					}
				}
			}
		}
		return e.repo.UpdateTransactionStatus(ctx, ibTxID, domain.TxStatusRolledBack, "ROLLED_BACK", "")
	})
}

// ─── Escrow akcija (blokada na accept, oslobađanje na exercise/istek) ─────────

// BlockShares skida `amount` akcija sa korisnikovih public_shares (FIFO).
// Koristi se na accept-u OTC ponude da se akcije prodavca zaključaju za ugovor.
func (e *LocalTransactionExecutor) BlockShares(ctx context.Context, userID int64, ticker string, amount int32) error {
	return e.db.WithContext(ctx).Transaction(func(dbTx *gorm.DB) error {
		return decrementPublicShares(dbTx, userID, ticker, int64(amount))
	})
}

// ReleaseShares vraća `amount` akcija u public_shares korisnika (kompenzacija
// neuspele premije ili oslobađanje na istek ugovora).
func (e *LocalTransactionExecutor) ReleaseShares(ctx context.Context, userID int64, ticker string, amount int32) error {
	var listingID int64
	if err := e.db.WithContext(ctx).Raw(`SELECT id FROM core_banking.listing WHERE ticker = ?`, ticker).Scan(&listingID).Error; err != nil {
		return err
	}
	if listingID == 0 {
		return fmt.Errorf("listing ne postoji za ticker: %s", ticker)
	}
	return e.db.WithContext(ctx).Exec(`
		INSERT INTO core_banking.public_shares (listing_id, user_id, quantity)
		VALUES (?, ?, ?)
	`, listingID, userID, amount).Error
}

// personAccount — razrešeni račun korisnika za PERSON+MONAS knjiženja.
type personAccount struct {
	BrojRacuna  string          `gorm:"column:broj_racuna"`
	Stanje      decimal.Decimal `gorm:"column:stanje_racuna"`
	Rezervisana decimal.Decimal `gorm:"column:rezervisana_sredstva"`
	Valuta      string          `gorm:"column:valuta_oznaka"`
}

// resolvePersonAccount bira račun korisnika za poravnanje u datoj valuti dogovora:
// prednost ima račun u TAČNOJ valuti (bez konverzije); inače RSD; inače prvi aktivan.
// Vraća (račun + njegova valuta, true) ako postoji ijedan aktivan račun.
func (e *LocalTransactionExecutor) resolvePersonAccount(ctx context.Context, db *gorm.DB, personID, currency string) (personAccount, bool) {
	var acc personAccount
	uid, err := strconv.ParseInt(personID, 10, 64)
	if err != nil {
		return acc, false
	}
	db.WithContext(ctx).Raw(`
		SELECT r.broj_racuna, r.stanje_racuna, r.rezervisana_sredstva, v.oznaka AS valuta_oznaka
		FROM core_banking.racun r
		JOIN core_banking.valuta v ON v.id = r.id_valute
		WHERE r.id_vlasnika = ? AND r.status = 'AKTIVAN'
		ORDER BY (v.oznaka = ?) DESC, (v.oznaka = 'RSD') DESC, r.id ASC
		LIMIT 1
	`, uid, currency).Scan(&acc)
	return acc, acc.BrojRacuna != ""
}

// convertForSettlement konvertuje SIGNED iznos iz valute dogovora (fromCur) u
// valutu računa (toCur) preko menjačnice (kupovni/prodajni, pivot RSD):
//   - credit (pozitivan): strana valuta → RSD po KUPOVNOM
//   - debit  (negativan): strana valuta → RSD po PRODAJNOM
//
// Ako su valute iste → 1:1. Ako nema izvora kursa → greška.
func (e *LocalTransactionExecutor) convertForSettlement(ctx context.Context, amount decimal.Decimal, fromCur, toCur string) (decimal.Decimal, error) {
	if fromCur == toCur {
		return amount, nil
	}
	if e.rates == nil {
		return decimal.Zero, fmt.Errorf("konverzija %s→%s nedostupna (nema izvora kursa)", fromCur, toCur)
	}
	rates, err := e.rates.GetRates(ctx)
	if err != nil {
		return decimal.Zero, fmt.Errorf("kurs nedostupan: %w", err)
	}
	byCode := make(map[string]domain.ExchangeRate, len(rates))
	for _, r := range rates {
		byCode[r.Oznaka] = r
	}
	isCredit := amount.IsPositive()
	mag := amount.Abs()

	// Korak 1: obaveza u fromCur → RSD vrednost.
	rsd := mag
	if fromCur != "RSD" {
		r, ok := byCode[fromCur]
		if !ok || r.Kupovni <= 0 || r.Prodajni <= 0 {
			return decimal.Zero, fmt.Errorf("nema kursa za %s", fromCur)
		}
		rate := r.Kupovni // banka kupuje stranu valutu (credit korisniku)
		if !isCredit {
			rate = r.Prodajni // banka prodaje stranu valutu (debit korisniku)
		}
		rsd = mag.Mul(decimal.NewFromFloat(rate))
	}

	// Korak 2: RSD vrednost → toCur.
	out := rsd
	if toCur != "RSD" {
		r, ok := byCode[toCur]
		if !ok || r.Kupovni <= 0 || r.Prodajni <= 0 {
			return decimal.Zero, fmt.Errorf("nema kursa za %s", toCur)
		}
		div := r.Prodajni // korisnik prima toCur → banka prodaje po prodajnom
		if !isCredit {
			div = r.Kupovni // korisnik plaća toCur → banka kupuje po kupovnom
		}
		out = rsd.Div(decimal.NewFromFloat(div))
	}

	if !isCredit {
		return out.Neg(), nil
	}
	return out, nil
}

// decrementPublicShares FIFO-smanjuje korisnikove public_shares za `amount`
// akcija datog tickera (najstariji redovi prvi). Greška ako nema dovoljno.
func decrementPublicShares(dbTx *gorm.DB, userID int64, ticker string, amount int64) error {
	var rows []struct {
		ID       int64 `gorm:"column:id"`
		Quantity int32 `gorm:"column:quantity"`
	}
	if err := dbTx.Raw(`
		SELECT ps.id, ps.quantity
		FROM core_banking.public_shares ps
		JOIN core_banking.listing l ON l.id = ps.listing_id
		WHERE ps.user_id = ? AND l.ticker = ?
		ORDER BY ps.created_at ASC
		FOR UPDATE
	`, userID, ticker).Scan(&rows).Error; err != nil {
		return fmt.Errorf("zaključavanje public_shares: %w", err)
	}
	remaining := amount
	for _, row := range rows {
		if remaining <= 0 {
			break
		}
		take := int64(row.Quantity)
		if take > remaining {
			take = remaining
		}
		newQty := int64(row.Quantity) - take
		if newQty == 0 {
			if err := dbTx.Exec(`DELETE FROM core_banking.public_shares WHERE id = ?`, row.ID).Error; err != nil {
				return err
			}
		} else {
			if err := dbTx.Exec(`UPDATE core_banking.public_shares SET quantity = ? WHERE id = ?`, newQty, row.ID).Error; err != nil {
				return err
			}
		}
		remaining -= take
	}
	if remaining > 0 {
		return fmt.Errorf("nedovoljno akcija u javnom režimu")
	}
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// isBalanced — zbir amount-a po (assetKey) mora biti 0.
func isBalanced(postings []domain.Posting) bool {
	sums := map[string]decimal.Decimal{}
	for _, p := range postings {
		key := assetKey(p.Asset)
		sums[key] = sums[key].Add(p.Amount)
	}
	for _, v := range sums {
		if !v.IsZero() {
			return false
		}
	}
	return true
}

func assetKey(a domain.Asset) string {
	switch a.Type {
	case domain.AssetTypeMonas:
		if a.MonAs != nil {
			return "MONAS:" + a.MonAs.Currency
		}
	case domain.AssetTypeStock:
		if a.Stock != nil {
			return "STOCK:" + a.Stock.Ticker
		}
	case domain.AssetTypeOption:
		if a.Option != nil {
			return "OPTION:" + strconv.FormatInt(a.Option.NegotiationID.RoutingNumber, 10) + ":" + a.Option.NegotiationID.ID
		}
	}
	return string(a.Type) + ":?"
}

func pCopy(p domain.Posting) *domain.Posting {
	cp := p
	return &cp
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

// writeTxRow upisuje knjiženje u core_banking.transakcija (tip 'INTERBANK').
func writeTxRow(dbTx *gorm.DB, brojRacuna, tip string, amount decimal.Decimal, opis string) error {
	var racunID int64
	if err := dbTx.Raw(`SELECT id FROM core_banking.racun WHERE broj_racuna = ?`, brojRacuna).Scan(&racunID).Error; err != nil {
		return err
	}
	if racunID == 0 {
		return fmt.Errorf("racun ne postoji: %s", brojRacuna)
	}
	return dbTx.Exec(`
		INSERT INTO core_banking.transakcija (racun_id, tip_transakcije, iznos, opis, status)
		VALUES (?, ?, ?, ?, 'IZVRSEN')
	`, racunID, tip, amount, opis).Error
}

// MarshalTransaction — utility za spoljnji kod (coordinator).
func MarshalTransaction(tx domain.Transaction) (string, error) {
	b, err := json.Marshal(tx)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalTransaction — utility za handler.
func UnmarshalTransaction(payload string) (domain.Transaction, error) {
	var tx domain.Transaction
	if err := json.Unmarshal([]byte(payload), &tx); err != nil {
		return domain.Transaction{}, err
	}
	return tx, nil
}
