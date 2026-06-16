-- Dodaje terminalni status EXPIRED za OTC ponude (automatski istek po TTL/neaktivnosti).
ALTER TABLE core_banking.otc_offers
    DROP CONSTRAINT IF EXISTS otc_offers_status_check;
ALTER TABLE core_banking.otc_offers
    ADD CONSTRAINT otc_offers_status_check
    CHECK (status IN ('PENDING','ACCEPTED','REJECTED','DEACTIVATED','EXPIRED'));

ALTER TABLE core_banking.otc_offer_history
    DROP CONSTRAINT IF EXISTS otc_offer_history_action_check;
ALTER TABLE core_banking.otc_offer_history
    ADD CONSTRAINT otc_offer_history_action_check
    CHECK (action IN ('CREATED','COUNTER','ACCEPTED','DECLINED','EXPIRED'));
