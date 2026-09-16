-- Runtime gate for S4-D provider quarantine. Provider operational state stays
-- private; new-work paths receive only this boolean SECURITY DEFINER function.
CREATE FUNCTION supply.provider_accepts_new_work(provider text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
SELECT provider ~ '^[A-Za-z0-9_-]{1,128}$'
   AND NOT EXISTS(SELECT 1 FROM supply.provider_admin_states s WHERE s.provider_id=provider AND s.state='quarantined')
$$;
REVOKE ALL ON FUNCTION supply.provider_accepts_new_work(text) FROM PUBLIC;
