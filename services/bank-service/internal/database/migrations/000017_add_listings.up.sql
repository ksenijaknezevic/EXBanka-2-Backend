-- 000017_add_listings.up.sql
-- Tabele za hartije od vrednosti (Listings)

CREATE TABLE core_banking.listing (
    id            BIGINT          GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    ticker        VARCHAR(20)     NOT NULL UNIQUE,
    name          VARCHAR(255)    NOT NULL,
    exchange_id   BIGINT          NOT NULL REFERENCES core_banking.exchange(id),
    listing_type  VARCHAR(10)     NOT NULL
        CONSTRAINT listing_type_check
            CHECK (listing_type IN ('STOCK', 'FOREX', 'FUTURE', 'OPTION')),
    last_refresh  TIMESTAMPTZ,
    price         NUMERIC(18, 6)  NOT NULL DEFAULT 0,
    ask           NUMERIC(18, 6)  NOT NULL DEFAULT 0,
    bid           NUMERIC(18, 6)  NOT NULL DEFAULT 0,
    volume        BIGINT          NOT NULL DEFAULT 0,
    details_json  TEXT            NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_listing_type    ON core_banking.listing (listing_type);
CREATE INDEX idx_listing_ticker  ON core_banking.listing (ticker);
CREATE INDEX idx_listing_exchange ON core_banking.listing (exchange_id);

CREATE TABLE core_banking.listing_daily_price_info (
    id           BIGINT         GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    listing_id   BIGINT         NOT NULL REFERENCES core_banking.listing(id),
    date         DATE           NOT NULL,
    price        NUMERIC(18, 6) NOT NULL DEFAULT 0,
    ask_high     NUMERIC(18, 6) NOT NULL DEFAULT 0,
    bid_low      NUMERIC(18, 6) NOT NULL DEFAULT 0,
    price_change NUMERIC(18, 6) NOT NULL DEFAULT 0,
    volume       BIGINT         NOT NULL DEFAULT 0,
    CONSTRAINT uq_listing_daily UNIQUE (listing_id, date)
);

CREATE INDEX idx_ldpi_listing_date ON core_banking.listing_daily_price_info (listing_id, date DESC);
