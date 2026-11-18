-- Turn on Row-Level Security for every table in public, with no policies.
--
-- Supabase serves the public schema through its REST API (PostgREST) to the
-- anon and authenticated roles. junkie never uses that API (the Go server
-- talks to Postgres directly), but without RLS anyone holding the project's
-- anon key could read or rewrite every table, password hashes included.
-- RLS with no policies denies those roles every row.
--
-- The app is unaffected: it connects as the role that ran these migrations,
-- which owns the tables, and a table's owner skips RLS (nothing here uses
-- FORCE ROW LEVEL SECURITY). Tables owned by anyone else are left alone, so
-- this can never fail a boot or lock the app out of a table.
--
-- It loops instead of naming tables, and migrations re-run on every boot, so
-- any table added later gets the same treatment without remembering to.
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.relname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relkind IN ('r', 'p')
          AND NOT c.relrowsecurity
          AND pg_get_userbyid(c.relowner) = current_user
    LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t.relname);
    END LOOP;
END
$$;
