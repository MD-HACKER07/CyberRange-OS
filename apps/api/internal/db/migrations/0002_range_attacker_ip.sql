-- Persist the attacker container's IP and the session's isolated subnet so
-- the "click to attack" flow can generate realistic, correlated SIEM alerts
-- (src_ip = the real attacker IP, dst_ip = the real target IP) without
-- re-deriving them from Docker on every request.
ALTER TABLE range_sessions ADD COLUMN IF NOT EXISTS attacker_ip TEXT NOT NULL DEFAULT '';
ALTER TABLE range_sessions ADD COLUMN IF NOT EXISTS subnet TEXT NOT NULL DEFAULT '';
