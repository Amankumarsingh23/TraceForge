package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"time"
	"traceforge/internal/tenant"
)

//go:embed migration.sql
var migration string
var ErrInvalid = errors.New("invalid credential")
var ErrUnavailable = errors.New("authorization unavailable")

type Identity struct {
	Tenant       string   `json:"tenant"`
	Scopes       []string `json:"scopes"`
	CredentialID string   `json:"credential_id"`
}

func (i Identity) Allows(scope string) bool {
	for _, s := range i.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

type Authenticator interface {
	Authenticate(context.Context, string) (Identity, error)
}
type Registry struct{ pool *pgxpool.Pool }

func Open(ctx context.Context) (*Registry, error) {
	cfg, e := pgxpool.ParseConfig("user=traceforge dbname=traceforge host=127.0.0.1 sslmode=disable")
	if e != nil {
		return nil, ErrUnavailable
	}
	if h := os.Getenv("POSTGRES_HOST"); h != "" {
		cfg.ConnConfig.Host = h
	}
	cfg.ConnConfig.Password = os.Getenv("POSTGRES_PASSWORD")
	if cfg.ConnConfig.Password == "" {
		return nil, ErrUnavailable
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.ConnectTimeout = time.Second
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "500"
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, ErrUnavailable
	}
	return &Registry{pool}, nil
}
func (r *Registry) Close()                            { r.pool.Close() }
func (r *Registry) Migrate(ctx context.Context) error { _, e := r.pool.Exec(ctx, migration); return e }
func (r *Registry) Issue(ctx context.Context, id, scope string) (string, string, error) {
	if !tenant.Valid(id) || (scope != "ingest" && scope != "read" && scope != "manage") {
		return "", "", ErrInvalid
	}
	random := make([]byte, 32)
	if _, e := rand.Read(random); e != nil {
		return "", "", e
	}
	key := "tf_" + hex.EncodeToString(random)
	hash := sha256.Sum256([]byte(key))
	cid := hex.EncodeToString(hash[:12])
	_, e := r.pool.Exec(ctx, `INSERT INTO public.security_credentials(id,tenant_id,verifier,scopes) VALUES($1,$2,$3,$4)`, cid, id, hash[:], []string{scope})
	return key, cid, e
}
func (r *Registry) Revoke(ctx context.Context, id, owner string) error {
	tag, e := r.pool.Exec(ctx, `UPDATE public.security_credentials SET revoked=true WHERE id=$1 AND tenant_id=$2`, id, owner)
	if e != nil {
		return ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return ErrInvalid
	}
	return nil
}
func (r *Registry) Authenticate(ctx context.Context, key string) (Identity, error) {
	var i Identity
	if len(key) != 67 || !strings.HasPrefix(key, "tf_") {
		return i, ErrInvalid
	}
	if _, e := hex.DecodeString(key[3:]); e != nil {
		return i, ErrInvalid
	}
	hash := sha256.Sum256([]byte(key))
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	var stored []byte
	e := r.pool.QueryRow(ctx, `SELECT c.id,c.tenant_id,c.scopes,c.verifier FROM public.security_credentials c JOIN public.security_tenants t USING(tenant_id) WHERE c.verifier=$1 AND NOT c.revoked AND t.active`, hash[:]).Scan(&i.CredentialID, &i.Tenant, &i.Scopes, &stored)
	if errors.Is(e, pgx.ErrNoRows) {
		return Identity{}, ErrInvalid
	}
	if e != nil {
		return Identity{}, ErrUnavailable
	}
	if !tenant.Valid(i.Tenant) || subtle.ConstantTimeCompare(hash[:], stored) != 1 {
		return Identity{}, ErrInvalid
	}
	return i, nil
}
