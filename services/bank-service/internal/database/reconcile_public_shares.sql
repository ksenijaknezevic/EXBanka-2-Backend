-- reconcile_public_shares.sql
--
-- JEDNOKRATNO čišćenje postojećih podataka. NIJE auto-migracija (pokreni ručno).
--
-- Problem: do ove ispravke, berzanska prodaja nije umanjivala core_banking.public_shares
-- (samo OTC tokovi jesu), pa "javno objavljeno" može biti veće od stvarnog poseda
-- (npr. posed 2, javno 9). Engine od sada umanjuje public_shares na svakom SELL fillu,
-- ali postojeći "napumpani" redovi se ne isprave sami dok se ne rasprodaju.
--
-- Ova skripta KAPIRA objavljenu količinu na trenutni posed (javno ≤ posed). Tačan broj
-- koji je prodat se ne može rekonstruisati, pa je kapiranje na posed najbezbednija
-- vrednost koja se može povratiti; od tada engine održava tačno stanje.
--
-- Opseg prati agregaciju u portfoliju (getMyPortfolio):
--   * klijenti (ne-aktuari): kapirano po (user_id, listing_id) na sopstveni neto posed
--   * aktuari:               kapirano po listing_id na zajednički (pooled) neto posed
--
-- Idempotentno: kad je jednom konzistentno, ponovno pokretanje ništa ne menja.
--
-- ───────────────────────────────────────────────────────────────────────────────
-- KORAK 1 — DRY RUN (samo čitanje). Pokreni i pregledaj šta bi se promenilo.
-- ───────────────────────────────────────────────────────────────────────────────
WITH net AS (
    -- Neto posed po (user, listing) — isto pravilo kao getMyPortfolio.
    SELECT user_id, listing_id,
        SUM(CASE
            WHEN direction = 'BUY'  AND status = 'DONE'     AND is_done THEN quantity
            WHEN direction = 'BUY'  AND status = 'CANCELED'             THEN (quantity - remaining_portions)
            WHEN direction = 'SELL' AND status = 'DONE'     AND is_done THEN -quantity
            WHEN direction = 'SELL' AND status = 'CANCELED'             THEN -(quantity - remaining_portions)
            ELSE 0
        END) AS qty
    FROM core_banking.orders
    GROUP BY user_id, listing_id
),
actuary_net AS (
    SELECT listing_id, SUM(qty) AS qty
    FROM net
    WHERE user_id IN (SELECT employee_id FROM core_banking.actuary_info)
    GROUP BY listing_id
),
scoped AS (
    -- Klijentski redovi: posed = sopstveni neto; particija po (user, listing).
    SELECT ps.id, ps.quantity, ps.created_at,
           ps.user_id AS pk_user, ps.listing_id AS pk_listing,
           GREATEST(COALESCE(n.qty, 0), 0) AS holdings
    FROM core_banking.public_shares ps
    LEFT JOIN net n ON n.user_id = ps.user_id AND n.listing_id = ps.listing_id
    WHERE ps.user_id NOT IN (SELECT employee_id FROM core_banking.actuary_info)

    UNION ALL

    -- Aktuarski redovi: posed = pooled aktuarski neto; particija po listing-u.
    SELECT ps.id, ps.quantity, ps.created_at,
           0 AS pk_user, ps.listing_id AS pk_listing,
           GREATEST(COALESCE(an.qty, 0), 0) AS holdings
    FROM core_banking.public_shares ps
    LEFT JOIN actuary_net an ON an.listing_id = ps.listing_id
    WHERE ps.user_id IN (SELECT employee_id FROM core_banking.actuary_info)
),
ranked AS (
    SELECT id, quantity, holdings,
        SUM(quantity) OVER (PARTITION BY pk_user, pk_listing
                            ORDER BY created_at, id
                            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS cum_incl
    FROM scoped
)
SELECT id,
       quantity AS old_qty,
       GREATEST(0, LEAST(quantity, holdings - (cum_incl - quantity)))::int AS new_qty,
       quantity - GREATEST(0, LEAST(quantity, holdings - (cum_incl - quantity)))::int AS reduce_by
FROM ranked
WHERE GREATEST(0, LEAST(quantity, holdings - (cum_incl - quantity)))::int < quantity
ORDER BY id;

-- ───────────────────────────────────────────────────────────────────────────────
-- KORAK 2 — PRIMENA (upisi). Pokreni tek kad si zadovoljan KORAK 1 izlazom.
-- Sve je u transakciji: na kraju COMMIT (ili ROLLBACK da odustaneš).
-- ───────────────────────────────────────────────────────────────────────────────
BEGIN;

CREATE TEMP TABLE ps_reconcile ON COMMIT DROP AS
WITH net AS (
    SELECT user_id, listing_id,
        SUM(CASE
            WHEN direction = 'BUY'  AND status = 'DONE'     AND is_done THEN quantity
            WHEN direction = 'BUY'  AND status = 'CANCELED'             THEN (quantity - remaining_portions)
            WHEN direction = 'SELL' AND status = 'DONE'     AND is_done THEN -quantity
            WHEN direction = 'SELL' AND status = 'CANCELED'             THEN -(quantity - remaining_portions)
            ELSE 0
        END) AS qty
    FROM core_banking.orders
    GROUP BY user_id, listing_id
),
actuary_net AS (
    SELECT listing_id, SUM(qty) AS qty
    FROM net
    WHERE user_id IN (SELECT employee_id FROM core_banking.actuary_info)
    GROUP BY listing_id
),
scoped AS (
    SELECT ps.id, ps.quantity, ps.created_at,
           ps.user_id AS pk_user, ps.listing_id AS pk_listing,
           GREATEST(COALESCE(n.qty, 0), 0) AS holdings
    FROM core_banking.public_shares ps
    LEFT JOIN net n ON n.user_id = ps.user_id AND n.listing_id = ps.listing_id
    WHERE ps.user_id NOT IN (SELECT employee_id FROM core_banking.actuary_info)
    UNION ALL
    SELECT ps.id, ps.quantity, ps.created_at,
           0 AS pk_user, ps.listing_id AS pk_listing,
           GREATEST(COALESCE(an.qty, 0), 0) AS holdings
    FROM core_banking.public_shares ps
    LEFT JOIN actuary_net an ON an.listing_id = ps.listing_id
    WHERE ps.user_id IN (SELECT employee_id FROM core_banking.actuary_info)
),
ranked AS (
    SELECT id, quantity, holdings,
        SUM(quantity) OVER (PARTITION BY pk_user, pk_listing
                            ORDER BY created_at, id
                            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS cum_incl
    FROM scoped
)
SELECT id,
       GREATEST(0, LEAST(quantity, holdings - (cum_incl - quantity)))::int AS new_qty
FROM ranked;

-- Ispražnjeni redovi se brišu (CHECK quantity > 0 ne dozvoljava 0).
DELETE FROM core_banking.public_shares
WHERE id IN (SELECT id FROM ps_reconcile WHERE new_qty = 0);

-- Delimično prekoračeni redovi se umanjuju.
UPDATE core_banking.public_shares ps
SET quantity = r.new_qty
FROM ps_reconcile r
WHERE ps.id = r.id AND r.new_qty > 0 AND r.new_qty < ps.quantity;

COMMIT;
