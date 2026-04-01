-- =============================================================================
-- Migration: 000020_seed_forex_futures
-- Service:   bank-service
-- Schema:    core_banking
--
-- Seeder za FOREX i FUTURE listing-e potrebne za testiranje:
--   FOREX:  EUR/USD, GBP/USD
--   FUTURE: CLJ26 (Crude Oil, April 2026), SIH26 (Silver, March 2026)
-- =============================================================================

-- ─── FOREX listings ──────────────────────────────────────────────────────────

INSERT INTO core_banking.listing (ticker, name, exchange_id, listing_type, price, ask, bid, volume, details_json)
SELECT
    t.ticker,
    t.name,
    e.id AS exchange_id,
    'FOREX'::VARCHAR AS listing_type,
    t.price,
    t.ask,
    t.bid,
    t.volume,
    t.details_json::TEXT
FROM (VALUES
    ('EUR/USD', 'Euro / US Dollar',           1.0850, 1.0852, 1.0848, 5000000,
     '{"base_currency":"EUR","quote_currency":"USD","contract_size":1000,"liquidity":"High"}'),
    ('GBP/USD', 'British Pound / US Dollar',  1.2700, 1.2703, 1.2697, 3000000,
     '{"base_currency":"GBP","quote_currency":"USD","contract_size":1000,"liquidity":"High"}')
) AS t(ticker, name, price, ask, bid, volume, details_json)
CROSS JOIN (
    SELECT id FROM core_banking.exchange WHERE mic_code = 'XNAS' LIMIT 1
) e
ON CONFLICT (ticker) DO NOTHING;

-- ─── FUTURE listings ─────────────────────────────────────────────────────────

INSERT INTO core_banking.listing (ticker, name, exchange_id, listing_type, price, ask, bid, volume, details_json)
SELECT
    t.ticker,
    t.name,
    e.id AS exchange_id,
    'FUTURE'::VARCHAR AS listing_type,
    t.price,
    t.ask,
    t.bid,
    t.volume,
    t.details_json::TEXT
FROM (VALUES
    ('CLJ26', 'Crude Oil Futures (Apr 2026)',  72.50, 72.58, 72.42, 800000,
     '{"contract_size":1000,"contract_unit":"Barrel","settlement_date":"2026-04-01"}'),
    ('SIH26', 'Silver Futures (Mar 2026)',     24.30, 24.35, 24.25, 400000,
     '{"contract_size":5000,"contract_unit":"Troy Ounce","settlement_date":"2026-03-01"}')
) AS t(ticker, name, price, ask, bid, volume, details_json)
CROSS JOIN (
    SELECT id FROM core_banking.exchange WHERE mic_code = 'XCBO' LIMIT 1
) e
ON CONFLICT (ticker) DO NOTHING;
