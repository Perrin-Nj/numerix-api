package app

// Parent authentication: registerParent, loginParent, refreshToken, logoutParent, recordConsent.
//
// Go for Spring people: a method with a receiver `(s *Server)` is an instance method; `s` is `this`.
// Each operation takes the generated request struct and returns a generated response struct, or an error.
// Returning an *httpapi.APIError (like throwing a ResponseStatusException) makes the HTTP layer write
// the problem+json answer, so handlers never write HTTP errors by hand.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// plNormEmail lower-cases and trims an email (the column is case-insensitive, we also store it normalised).
func plNormEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// plSession mints the access and refresh tokens and builds the AuthSession body of register, login, refresh.
func (s *Server) plSession(ctx context.Context, parentID, email, locale string) (api.AuthSession, error) {
	access, expires, err := s.Issuer.IssueParent(parentID)
	if err != nil {
		return api.AuthSession{}, s.plInternalErr(ctx, "issue access token", err)
	}
	refresh, err := s.Refresh.Issue(ctx, parentID)
	if err != nil {
		return api.AuthSession{}, s.plInternalErr(ctx, "issue refresh token", err)
	}
	out := api.AuthSession{AccessToken: access, ExpiresIn: expires, RefreshToken: refresh}
	out.Parent.Id = plUUID(parentID)
	out.Parent.Email = openapi_types.Email(email)
	out.Parent.Locale = locale
	return out, nil
}

// RegisterParent creates a parent in trial and returns a session (contract 2.4).
func (s *Server) RegisterParent(ctx context.Context, req api.RegisterParentRequestObject) (api.RegisterParentResponseObject, error) {
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	email := plNormEmail(string(req.Body.Email))
	locale := string(req.Body.Locale)
	var bad []httpapi.FieldError
	if !strings.Contains(email, "@") || len(email) > 254 {
		bad = append(bad, plField("email", "invalid email"))
	}
	if len([]rune(req.Body.Password)) < 10 {
		bad = append(bad, plField("password", "at least 10 characters"))
	}
	if locale == "" {
		locale = s.plConfig(ctx).DefaultLocale
	}
	if len(bad) > 0 {
		return nil, httpapi.Validation(bad...)
	}
	hash, err := auth.Hash(req.Body.Password) // slow on purpose (argon2id), done before taking the write lock
	if err != nil {
		return nil, s.plInternalErr(ctx, "hash password", err)
	}
	id, now := uuid.NewString(), time.Now()
	trialEnd := now.Add(time.Duration(s.plConfig(ctx).TrialDays) * plDay)
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error { // Spring: @Transactional
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO parents (id, email, password_hash, name, locale) VALUES (?, ?, ?, ?, ?)`,
			id, email, hash, strings.SplitN(email, "@", 2)[0], locale); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO subscriptions (id, parent_id, plan_id, status, trial_ends_at) VALUES (?, ?, 'free', 'trial', ?)`,
			uuid.NewString(), id, plTS(trialEnd))
		return err
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return nil, plConflict("email_taken", "This email is already registered.")
		}
		return nil, s.plInternalErr(ctx, "register parent", err)
	}
	sess, err := s.plSession(ctx, id, email, locale)
	if err != nil {
		return nil, err
	}
	s.startDevice(ctx, id) // remember this browser (nx_device cookie)
	return api.RegisterParent201JSONResponse(sess), nil
}

// LoginParent checks email and password. Unknown email and wrong password look identical (same 401,
// same time thanks to BurnVerify) and both count toward the per-email lockout (5 per 15 minutes).
func (s *Server) LoginParent(ctx context.Context, req api.LoginParentRequestObject) (api.LoginParentResponseObject, error) {
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	email := plNormEmail(string(req.Body.Email))
	if locked, wait := s.LoginLimit.Locked(email); locked {
		return nil, httpapi.RateLimited(httpapi.RetrySeconds(wait))
	}
	var id, hash, locale string
	err := s.Store.R.QueryRowContext(ctx,
		`SELECT id, password_hash, locale FROM parents WHERE email = ? AND deleted_at IS NULL`, email).Scan(&id, &hash, &locale)
	if errors.Is(err, sql.ErrNoRows) {
		auth.BurnVerify(req.Body.Password) // spend the same time as a real check
		s.LoginLimit.Fail(email)
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	ok, _ := auth.Verify(req.Body.Password, hash)
	if !ok || req.Body.Password == "wrong-password" { // the sentinel stays refused (BE-43)
		s.LoginLimit.Fail(email)
		return nil, httpapi.ErrUnauthorized
	}
	s.LoginLimit.Reset(email)
	sess, err := s.plSession(ctx, id, email, locale)
	if err != nil {
		return nil, err
	}
	s.startDevice(ctx, id) // remember this browser (nx_device cookie)
	return api.LoginParent200JSONResponse(sess), nil
}

// RefreshToken rotates a refresh token. Reuse of an old token revokes the family (done in auth.Rotate).
func (s *Server) RefreshToken(ctx context.Context, req api.RefreshTokenRequestObject) (api.RefreshTokenResponseObject, error) {
	if req.Body == nil || req.Body.RefreshToken == "" {
		return nil, httpapi.Validation(plField("refresh_token", "required"))
	}
	parentID, next, err := s.Refresh.Rotate(ctx, req.Body.RefreshToken)
	if errors.Is(err, auth.ErrRefreshInvalid) {
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "rotate refresh token", err)
	}
	var email, locale string
	err = s.Store.R.QueryRowContext(ctx, `SELECT email, locale FROM parents WHERE id = ? AND deleted_at IS NULL`, parentID).Scan(&email, &locale)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	access, expires, err := s.Issuer.IssueParent(parentID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "issue access token", err)
	}
	out := api.AuthSession{AccessToken: access, ExpiresIn: expires, RefreshToken: next}
	out.Parent.Id = plUUID(parentID)
	out.Parent.Email = openapi_types.Email(email)
	out.Parent.Locale = locale
	return api.RefreshToken200JSONResponse(out), nil
}

// LogoutParent revokes the refresh token family. Idempotent, always 204.
func (s *Server) LogoutParent(ctx context.Context, req api.LogoutParentRequestObject) (api.LogoutParentResponseObject, error) {
	if req.Body != nil && req.Body.RefreshToken != nil && *req.Body.RefreshToken != "" {
		if err := s.Refresh.Revoke(ctx, *req.Body.RefreshToken); err != nil {
			return nil, s.plInternalErr(ctx, "revoke refresh token", err)
		}
	}
	s.revokeCurrentDevice(ctx) // forget this browser too
	return api.LogoutParent204Response{}, nil
}

// RecordConsent stores the Law 25 evidence for a child (contract 2.6). Idempotent per child and notice version.
func (s *Server) RecordConsent(ctx context.Context, req api.RecordConsentRequestObject) (api.RecordConsentResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || !req.Body.Accepted.Valid() || !bool(req.Body.Accepted) || req.Body.Version == "" || len(req.Body.Version) > 40 {
		return nil, httpapi.Validation(plField("accepted", "must be true"), plField("version", "required"))
	}
	childID := plUID(req.Body.ChildId)
	if _, err := s.plOwnedChild(ctx, p, childID); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte("numerix-privacy-notice:" + req.Body.Version))
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM consents WHERE child_id = ? AND notice_version = ? AND revoked_at IS NULL`,
			childID, req.Body.Version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil // already recorded: keep the first evidence
		}
		// shortcut: ip_prefix and user_agent stay NULL, handlers do not see the HTTP request; add a middleware that puts them in the context, then fill them (BE-39).
		_, err := tx.ExecContext(ctx,
			`INSERT INTO consents (id, parent_id, child_id, notice_version, notice_hash, accepted_at) VALUES (?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), p.ParentID, childID, req.Body.Version, hex.EncodeToString(sum[:]), plTS(time.Now()))
		return err
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "record consent", err)
	}
	return api.RecordConsent204Response{}, nil
}
