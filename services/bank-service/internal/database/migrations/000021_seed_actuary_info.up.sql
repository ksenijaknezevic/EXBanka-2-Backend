-- =============================================================================
-- Migration: 000021_seed_actuary_info
-- Service:   bank-service
-- Schema:    core_banking
--
-- Ubacuje SUPERVISOR redove u actuary_info za:
--   employee_id = 1  →  admin@raf.rs       (user_id iz user-service migration 000001)
--   employee_id = 3  →  kknezevic4622rn@raf.rs (user_id iz user-service migration 000007)
--
-- Supervisori nemaju limit i ne zahtevaju odobrenje za narudžbine.
-- =============================================================================

INSERT INTO core_banking.actuary_info (employee_id, actuary_type, "limit", used_limit, need_approval)
VALUES
    (1, 'SUPERVISOR', 0, 0, FALSE),
    (3, 'SUPERVISOR', 0, 0, FALSE)
ON CONFLICT (employee_id) DO NOTHING;
