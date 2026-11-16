CREATE TABLE IF NOT EXISTS public.security_tenants(
 tenant_id text PRIMARY KEY CHECK(tenant_id IN ('legacy','alpha','beta')),
 active boolean NOT NULL DEFAULT true
);
INSERT INTO public.security_tenants(tenant_id) VALUES('legacy'),('alpha'),('beta') ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS public.security_credentials(
 id text PRIMARY KEY,
 tenant_id text NOT NULL REFERENCES public.security_tenants(tenant_id),
 verifier bytea UNIQUE NOT NULL CHECK(octet_length(verifier)=32),
 scopes text[] NOT NULL CHECK(scopes <@ ARRAY['ingest','read','manage']::text[] AND cardinality(scopes)>0),
 revoked boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE SCHEMA IF NOT EXISTS tf_tenant_alpha;
CREATE SCHEMA IF NOT EXISTS tf_tenant_beta;
