package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MeterHome/Meter-Node/console/internal/api"
	"github.com/MeterHome/Meter-Node/console/internal/model"
)

// PrincipalStore resolves operator API tokens.
type PrincipalStore struct{ s *Store }

// NewPrincipalStore builds the store.
func NewPrincipalStore(s *Store) *PrincipalStore { return &PrincipalStore{s: s} }

var _ api.PrincipalStore = (*PrincipalStore)(nil)

// PrincipalByTokenHash looks up a service token by the SHA-256 of its secret.
//
// Nil with a nil error means "no such token", which the middleware turns into a
// 401. Expiry and revocation are part of the WHERE clause rather than a check
// in Go so there is no window where a revoked token is read as valid and then
// rejected — and no second round trip on the hot path.
func (p *PrincipalStore) PrincipalByTokenHash(ctx context.Context, hash []byte) (*api.Principal, error) {
	var name string
	var role model.OperatorRole
	err := p.s.pool.QueryRow(ctx, `
		SELECT name, role FROM api_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())`, hash).Scan(&name, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: resolve api token: %w", err)
	}

	// last_used_at is best-effort and deliberately not part of the request's
	// success: an operator API call must not fail because a bookkeeping UPDATE
	// hit a lock. It is what tells an admin which tokens are dead and can be
	// revoked.
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = p.s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE token_hash = $1`, hash)
	}()

	return &api.Principal{Name: name, Role: role}, nil
}
