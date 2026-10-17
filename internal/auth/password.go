// Package auth holds password/PIN hashing, JWT access tokens, refresh-token rotation,
// the request Principal and PIN lockout.
//
// Go for Spring people: this package is what Spring Security gives you (PasswordEncoder,
// JwtEncoder/Decoder, SecurityContext), written by hand and small.
package auth

import (
	"crypto/rand"
	"crypto/subtle" // constant-time comparison, so timing does not leak how many bytes matched
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (OWASP minimum: 19 MiB, 2 iterations, 1 lane). Stored inside each hash,
// so they can be raised later and old hashes still verify.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// ErrInvalidHash means the stored hash string is not in the expected format.
var ErrInvalidHash = errors.New("auth: invalid password hash format")

// Hash returns an argon2id hash in the standard PHC string format:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>. Used for parent passwords and 4-digit child PINs
// (Spring: Argon2PasswordEncoder.encode). A fresh random salt makes equal inputs hash differently.
func Hash(secret string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil { // crypto/rand = secure random source
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(secret), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify checks secret against a hash made by Hash, in constant time
// (Spring: PasswordEncoder.matches). It returns (false, nil) for a wrong secret and an error only
// when the stored hash is malformed.
func Verify(secret, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$") // ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrInvalidHash
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash lets login spend the same time for an unknown email as for a known one
// (no account enumeration by timing). Built once at start-up.
var dummyHash, _ = Hash("numerix-dummy-password")

// BurnVerify runs a verification that always fails; call it when the account does not exist.
func BurnVerify(secret string) { _, _ = Verify(secret, dummyHash) }
