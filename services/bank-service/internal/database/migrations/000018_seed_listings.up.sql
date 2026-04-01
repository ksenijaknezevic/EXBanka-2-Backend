-- 000018_seed_listings.up.sql
-- Seed podaci za testiranje: AAPL (Apple) i MSFT (Microsoft) na NASDAQ berzi.
-- exchange_id se dinamički dohvata po mic_code='XNAS' da ne zavisi od rednog broja.
-- details_json = '{}' za obične akcije (nema dodatnih polja).

INSERT INTO core_banking.listing (ticker, name, exchange_id, listing_type, price, ask, bid, volume, details_json)
SELECT
    t.ticker,
    t.name,
    e.id AS exchange_id,
    'STOCK'::VARCHAR AS listing_type,
    t.price,
    t.ask,
    t.bid,
    t.volume,
    '{}'::TEXT AS details_json
FROM (VALUES
    ('AAPL',  'Apple Inc.',         195.00, 195.20, 194.80, 55000000),
    ('MSFT',  'Microsoft Corp.',    415.00, 415.30, 414.70, 20000000)
) AS t(ticker, name, price, ask, bid, volume)
CROSS JOIN (
    SELECT id FROM core_banking.exchange WHERE mic_code = 'XNAS' LIMIT 1
) e
ON CONFLICT (ticker) DO NOTHING;
