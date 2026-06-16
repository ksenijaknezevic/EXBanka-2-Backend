-- Prvo ukloni EXPIRED vrednosti da stari CHECK ne padne.
UPDATE core_banking.otc_offers        SET status = 'DEACTIVATED' WHERE status = 'EXPIRED';
UPDATE core_banking.otc_offer_history SET action = 'DECLINED'    WHERE action = 'EXPIRED';

ALTER TABLE core_banking.otc_offers
    DROP CONSTRAINT IF EXISTS otc_offers_status_check;
ALTER TABLE core_banking.otc_offers
    ADD CONSTRAINT otc_offers_status_check
    CHECK (status IN ('PENDING','ACCEPTED','REJECTED','DEACTIVATED'));

ALTER TABLE core_banking.otc_offer_history
    DROP CONSTRAINT IF EXISTS otc_offer_history_action_check;
ALTER TABLE core_banking.otc_offer_history
    ADD CONSTRAINT otc_offer_history_action_check
    CHECK (action IN ('CREATED','COUNTER','ACCEPTED','DECLINED'));
