package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/meterhome/meternode-console/internal/keys"
	"github.com/meterhome/meternode-console/internal/model"
)

// LoadOrCreateSigningKey resolves the controller's command-signing identity.
//
// Continuity of this key is a fleet-wide correctness property, not an
// operational detail: agents pin its public half at enrollment and verify every
// command against it forever. A controller that mints a fresh key on each boot
// has silently told every enrolled node to reject its commands, and the symptom
// — commands time out across the whole fleet — looks nothing like the cause.
//
// So: in production the seed comes from the environment and this function only
// records the public half. In dev, where no seed is configured, a generated key
// is PERSISTED, so `docker compose restart` does not invalidate the agents the
// simulator just enrolled.
func LoadOrCreateSigningKey(ctx context.Context, s *Store, keyID, seed string, allowGenerate bool, log *slog.Logger) (*keys.Signer, error) {
	if seed != "" {
		signer, err := keys.FromSeed(keyID, seed)
		if err != nil {
			return nil, err
		}
		if err := s.upsertControllerKey(ctx, keyID, signer.PublicKeys()[keyID], nil); err != nil {
			return nil, err
		}
		if err := s.trustRetiredKeys(ctx, signer, log); err != nil {
			return nil, err
		}
		return signer, nil
	}

	if !allowGenerate {
		return nil, errors.New("store: no signing key seed configured and generation is not permitted outside dev")
	}

	// Dev: reuse the persisted key if there is one.
	var stored []byte
	err := s.pool.QueryRow(ctx, `SELECT private_key FROM controller_keys WHERE key_id = $1 AND private_key IS NOT NULL`, keyID).Scan(&stored)
	switch {
	case err == nil && len(stored) > 0:
		signer, err := keys.FromSeed(keyID, string(stored))
		if err != nil {
			return nil, fmt.Errorf("store: load persisted dev signing key: %w", err)
		}
		log.Warn("using the persisted DEV signing key; never do this outside dev", "key_id", keyID)
		if err := s.trustRetiredKeys(ctx, signer, log); err != nil {
			return nil, err
		}
		return signer, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("store: load controller key: %w", err)
	}

	signer, err := keys.Generate(keyID)
	if err != nil {
		return nil, err
	}
	devSeed := []byte(signer.Seed())
	if err := s.upsertControllerKey(ctx, keyID, signer.PublicKeys()[keyID], devSeed); err != nil {
		return nil, err
	}
	log.Warn("generated and persisted a DEV signing key; set METERNODE_SIGNING_KEY_SEED for anything else", "key_id", keyID)
	return signer, nil
}

func (s *Store) upsertControllerKey(ctx context.Context, keyID string, pub, priv []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO controller_keys (key_id, public_key, private_key, active)
		VALUES ($1, $2, $3, true)
		ON CONFLICT (key_id) DO UPDATE SET public_key = EXCLUDED.public_key, active = true`,
		keyID, pub, priv)
	if err != nil {
		return fmt.Errorf("store: upsert controller key: %w", err)
	}
	return nil
}

// trustRetiredKeys loads previously-active keys so credentials and commands
// they signed keep verifying.
//
// A fleet is always partly offline. A node that has been unplugged for three
// weeks comes back holding a credential signed by whatever key was active when
// it enrolled, and refusing it would mean a truck roll for a key rotation.
func (s *Store) trustRetiredKeys(ctx context.Context, signer *keys.Signer, log *slog.Logger) error {
	rows, err := s.pool.Query(ctx, `SELECT key_id, public_key FROM controller_keys WHERE key_id <> $1`, signer.KeyID())
	if err != nil {
		return fmt.Errorf("store: load retired controller keys: %w", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var keyID string
		var pub []byte
		if err := rows.Scan(&keyID, &pub); err != nil {
			return fmt.Errorf("store: scan retired controller key: %w", err)
		}
		if err := signer.TrustRetired(keyID, pub); err != nil {
			log.Warn("skipping unusable retired signing key", "key_id", keyID, "error", err)
			continue
		}
		n++
	}
	if n > 0 {
		log.Info("trusting retired controller signing keys", "count", n)
	}
	return rows.Err()
}

// EnsureBootstrapAdmin seeds a single admin API token so a fresh dev stack is
// usable without a manual SQL insert.
//
// config.Validate refuses METERNODE_BOOTSTRAP_ADMIN_TOKEN outside dev, and this
// function refuses it a second time. Two independent checks, because a shared
// admin credential baked into an image is the kind of mistake that survives to
// production precisely when only one thing guards it.
func EnsureBootstrapAdmin(ctx context.Context, s *Store, token string, isDev bool, log *slog.Logger) error {
	if token == "" {
		return nil
	}
	if !isDev {
		return errors.New("store: refusing to seed a bootstrap admin token outside dev")
	}
	sum := sha256.Sum256([]byte(token))
	prefix := hex.EncodeToString(sum[:4])
	_, err := s.pool.Exec(ctx, `
		INSERT INTO api_tokens (token_hash, token_prefix, name, role, created_by)
		VALUES ($1, $2, 'dev-bootstrap', $3, 'bootstrap')
		ON CONFLICT (token_hash) DO NOTHING`,
		sum[:], prefix, string(model.RoleAdmin))
	if err != nil {
		return fmt.Errorf("store: seed bootstrap admin token: %w", err)
	}
	log.Warn("seeded the DEV bootstrap admin token; it grants full admin and must never exist outside dev")
	return nil
}
