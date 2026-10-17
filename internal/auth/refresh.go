package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// ErrRefreshInvalid means the refresh token is unknown, expired, revoked or reused (handler answers 401).
var ErrRefreshInvalid = errors.New("auth: refresh token invalid")

// RefreshTokens issues and rotates opaque refresh tokens. Only the SHA-256 hash is stored, so a
// database leak does not leak usable tokens. SHA-256 (not argon2) is right here because the token
// is 256 random bits, not a human-chosen secret.
type RefreshTokens struct {
	store *store.Store
	ttl   time.Duration
	now   func() time.Time
}

// NewRefreshTokens builds the service (Spring: a @Service with a constructor-injected repository).
func NewRefreshTokens(s *store.Store, ttl time.Duration) *RefreshTokens {
	return &RefreshTokens{store: s, ttl: ttl, now: time.Now}
}

// hashToken returns the hex SHA-256 of a token.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// newToken returns 32 random bytes as URL-safe text.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Issue starts a new token family for a parent (login, register) and returns the plain token.
func (r *RefreshTokens) Issue(ctx context.Context, parentID string) (string, error) {
	var out string
	err := r.store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = r.insert(ctx, tx, parentID, uuid.NewString())
		return err
	})
	return out, err
}

// insert stores a fresh token of an existing family inside the caller's transaction.
func (r *RefreshTokens) insert(ctx context.Context, tx *sql.Tx, parentID, familyID string) (string, error) {
	tok, err := newToken()
	if err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, parent_id, family_id, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		uuid.NewString(), parentID, familyID, hashToken(tok), r.now().UTC().Add(r.ttl).Format(time.RFC3339Nano))
	if err != nil {
		return "", fmt.Errorf("insert refresh token: %w", err)
	}
	return tok, nil
}

// Rotate exchanges a valid token for a new one in the same family and returns (parentID, newToken).
// Presenting an already-used token is a theft signal: the whole family is revoked (contract 2.4).
func (r *RefreshTokens) Rotate(ctx context.Context, plain string) (string, string, error) {
	var parentID, next string
	var reuse bool // set when we must revoke; the revoke has to be committed, so we cannot just return an error
	err := r.store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var id, familyID, expires string
		var used, revoked sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT id, parent_id, family_id, expires_at, used_at, revoked_at FROM refresh_tokens WHERE token_hash = ?`,
			hashToken(plain)).Scan(&id, &parentID, &familyID, &expires, &used, &revoked)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRefreshInvalid
		}
		if err != nil {
			return fmt.Errorf("load refresh token: %w", err)
		}
		exp, perr := time.Parse(time.RFC3339Nano, expires)
		if revoked.Valid || perr != nil || !r.now().Before(exp) {
			return ErrRefreshInvalid
		}
		if used.Valid { // second use of the same token: revoke the family and commit that
			reuse = true
			_, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL`,
				r.now().UTC().Format(time.RFC3339Nano), familyID)
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET used_at = ? WHERE id = ?`,
			r.now().UTC().Format(time.RFC3339Nano), id); err != nil {
			return err
		}
		next, err = r.insert(ctx, tx, parentID, familyID)
		return err
	})
	if err != nil {
		return "", "", err
	}
	if reuse {
		return "", "", ErrRefreshInvalid
	}
	return parentID, next, nil
}

// Revoke kills the whole family of a token (logout). Unknown tokens are ignored (logout is idempotent).
func (r *RefreshTokens) Revoke(ctx context.Context, plain string) error {
	return r.store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE refresh_tokens SET revoked_at = ? WHERE revoked_at IS NULL AND family_id =
			 (SELECT family_id FROM refresh_tokens WHERE token_hash = ?)`,
			r.now().UTC().Format(time.RFC3339Nano), hashToken(plain))
		return err
	})
}
