package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuerName = "numerix-api"

// ErrInvalidToken covers every reason a token is refused (bad signature, expired, wrong shape).
var ErrInvalidToken = errors.New("auth: invalid token")

// claims is the JWT payload. RegisteredClaims carries sub (parent id), exp, iat, iss.
type claims struct {
	Role    Role   `json:"role"`
	ChildID string `json:"child_id,omitempty"`
	jwt.RegisteredClaims
}

// Issuer signs and verifies access tokens with HS256 (Spring: JwtEncoder + JwtDecoder).
type Issuer struct {
	secret    []byte
	parentTTL time.Duration
	childTTL  time.Duration
	now       func() time.Time // injectable clock for tests
}

// NewIssuer builds an Issuer. parentTTL is 15 min by default, childTTL 8 h (contract section 2.7).
func NewIssuer(secret string, parentTTL, childTTL time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), parentTTL: parentTTL, childTTL: childTTL, now: time.Now}
}

// IssueParent returns a parent access token and its lifetime in seconds (the `expires_in` field).
func (i *Issuer) IssueParent(parentID string) (string, int, error) {
	return i.sign(Principal{Role: RoleParent, ParentID: parentID}, i.parentTTL)
}

// IssueChild returns a child access token bound to one child (role=child, child_id).
func (i *Issuer) IssueChild(parentID, childID string) (string, int, error) {
	return i.sign(Principal{Role: RoleChild, ParentID: parentID, ChildID: childID}, i.childTTL)
}

func (i *Issuer) sign(p Principal, ttl time.Duration) (string, int, error) {
	now := i.now()
	c := claims{
		Role:    p.Role,
		ChildID: p.ChildID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuerName,
			Subject:   p.ParentID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(i.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign token: %w", err)
	}
	return s, int(ttl.Seconds()), nil
}

// Parse validates signature (HS256 only, blocks the "alg=none" trick), expiry and issuer,
// and returns the Principal.
func (i *Issuer) Parse(token string) (Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c,
		func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(issuerName),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(i.now),
	)
	if err != nil || c.Subject == "" {
		return Principal{}, ErrInvalidToken
	}
	switch c.Role {
	case RoleParent:
		return Principal{Role: RoleParent, ParentID: c.Subject}, nil
	case RoleChild:
		if c.ChildID == "" {
			return Principal{}, ErrInvalidToken
		}
		return Principal{Role: RoleChild, ParentID: c.Subject, ChildID: c.ChildID}, nil
	}
	return Principal{}, ErrInvalidToken
}
