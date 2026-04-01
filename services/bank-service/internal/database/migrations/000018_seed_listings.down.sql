-- 000018_seed_listings.down.sql
DELETE FROM core_banking.listing_daily_price_info
WHERE listing_id IN (
    SELECT id FROM core_banking.listing WHERE ticker IN ('AAPL', 'MSFT')
);

DELETE FROM core_banking.listing WHERE ticker IN ('AAPL', 'MSFT');
