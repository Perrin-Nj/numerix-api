// Package pwreset issues and checks one-time password reset codes (8 digits, shown as NNNN-NNNN).
// An admin creates a code for a parent; the parent enters it with the email and a new password.
// The code is never stored: only HMAC-SHA256(key, salt || code), with a per-row salt and a server side key,
// so a copy of the database alone cannot verify a guess.
package pwreset

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TTL         = time.Hour
	MaxAttempts = 5 // wrong codes before the active code is burned
	layout      = "2006-01-02T15:04:05.000Z"
)

// Stamp formats a time like the rest of the database.
func Stamp(t time.Time) string { return t.UTC().Format(layout) }

// Normalize accepts the code with or without dash and spaces and returns 8 digits, or "" when malformed.
func Normalize(in string) string {
	s := strings.NewReplacer("-", "", " ", "").Replace(in)
	if len(s) != 8 {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}

// DeriveKey makes the dedicated reset key from the signing secret (never use the raw secret).
func DeriveKey(secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("numerix:password-reset:v1"))
	return m.Sum(nil)
}

// Hash is the stored form of a code.
func Hash(key []byte, salt, code string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(salt + ":" + code))
	return hex.EncodeToString(m.Sum(nil))
}

// Match compares a stored hash with a candidate in constant time.
func Match(key []byte, salt, wantHash, code string) bool {
	return hmac.Equal([]byte(Hash(key, salt, code)), []byte(wantHash))
}

// Issue makes a new code for parentID inside tx; every earlier unused code of that parent is burned (replaced).
// It returns the code as "NNNN-NNNN".
func Issue(tx *sql.Tx, key []byte, parentID string, now time.Time) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%08d", n.Int64())
	sb := make([]byte, 16)
	if _, err := rand.Read(sb); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(sb)
	if _, err := tx.Exec(`UPDATE password_resets SET burned_at = ? WHERE parent_id = ? AND used_at IS NULL AND burned_at IS NULL`, Stamp(now), parentID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO password_resets (id, parent_id, code_hash, salt, created_at, expires_at) VALUES (?,?,?,?,?,?)`,
		uuid.NewString(), parentID, Hash(key, salt, code), salt, Stamp(now), Stamp(now.Add(TTL))); err != nil {
		return "", err
	}
	return code[:4] + "-" + code[4:], nil
}

// Active is the usable code row of a parent.
type Active struct{ ID, Salt, Hash string }

// ErrNone means no usable code exists for that email.
var ErrNone = errors.New("no active reset code")

// Find loads the active (unused, unburned, unexpired) code of the parent with this email, who must not be blocked or deleted.
func Find(q interface {
	QueryRow(string, ...any) *sql.Row
}, email string, now time.Time) (parentID string, a Active, err error) {
	err = q.QueryRow(`SELECT p.id, r.id, r.salt, r.code_hash FROM parents p JOIN password_resets r ON r.parent_id = p.id
		WHERE p.email = ? AND p.deleted_at IS NULL AND p.blocked_at IS NULL
		AND r.used_at IS NULL AND r.burned_at IS NULL AND r.expires_at > ?`, email, Stamp(now)).Scan(&parentID, &a.ID, &a.Salt, &a.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNone
	}
	return
}

// Wrong records one wrong guess and burns the code at MaxAttempts.
func Wrong(tx *sql.Tx, id string, now time.Time) error {
	_, err := tx.Exec(`UPDATE password_resets SET attempts = attempts + 1,
		burned_at = CASE WHEN attempts + 1 >= ? THEN ? ELSE burned_at END
		WHERE id = ? AND used_at IS NULL AND burned_at IS NULL`, MaxAttempts, Stamp(now), id)
	return err
}

// Claim marks the code used. ok is false when another request got there first (single use under concurrency).
func Claim(tx *sql.Tx, id string, now time.Time) (bool, error) {
	res, err := tx.Exec(`UPDATE password_resets SET used_at = ? WHERE id = ? AND used_at IS NULL AND burned_at IS NULL AND expires_at > ?`, Stamp(now), id, Stamp(now))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
