package app

// Password reset for parents (docs/specs/PASSWORD-RESET.md). An admin creates an 8 digit code (adminapi/password_reset.go),
// the parent spends it here with the email and a new password. No email is sent anywhere.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/pwreset"
)

// plResetInvalid is the one answer for every failure cause.
func plResetInvalid() *httpapi.APIError {
	return httpapi.NewError(http.StatusBadRequest, "reset_code_invalid", "This code is not valid. Ask support for a new one.")
}

// ResetPassword spends a reset code and sets the new password. No auto login.
func (s *Server) ResetPassword(ctx context.Context, req api.ResetPasswordRequestObject) (api.ResetPasswordResponseObject, error) {
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	b := req.Body
	if n := len([]rune(b.Password)); n < 10 || n > plPasswordMax { // the cap keeps argon2 off oversized input (QA-BE-001)
		return nil, httpapi.Validation(plField("password", "10 to 128 characters"))
	}
	email, now := plNormEmail(string(b.Email)), time.Now()
	code := pwreset.Normalize(b.Code)
	rk := pwreset.DeriveKey(s.Config.JWTSecret)

	pid, act, err := pwreset.Find(s.Store.R, email, now)
	if err != nil && !errors.Is(err, pwreset.ErrNone) {
		return nil, s.plInternalErr(ctx, "load reset code", err)
	}
	none := errors.Is(err, pwreset.ErrNone)
	if none { // no account or no active code: compare against a dummy so the work is the same
		act = pwreset.Active{ID: "none", Salt: "00000000000000000000000000000000", Hash: "0"}
	}
	good := pwreset.Match(rk, act.Salt, act.Hash, code) && code != "" && !none
	if !good {
		// Every failure does the same: one HMAC, one write transaction with the attempt counter UPDATE
		// (for an unknown email or no code it matches no row), so timing does not reveal that a code exists.
		if err := s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error { return pwreset.Wrong(tx, act.ID, now) }); err != nil {
			return nil, s.plInternalErr(ctx, "count wrong code", err)
		}
		return nil, plResetInvalid()
	}
	hash, err := auth.Hash(b.Password) // slow on purpose, only for a correct code, before the write lock
	if err != nil {
		return nil, s.plInternalErr(ctx, "hash password", err)
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		ok, err := pwreset.Claim(tx, act.ID, now) // single use: the conditional UPDATE lets one racer win
		if err != nil {
			return err
		}
		var blocked, deleted sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT blocked_at, deleted_at FROM parents WHERE id = ?`, pid).Scan(&blocked, &deleted); err != nil {
			return err
		}
		if !ok || blocked.Valid || deleted.Valid {
			return plResetInvalid() // rolls the claim back too
		}
		stamp := plTS(now)
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE parents SET password_hash = ? WHERE id = ?`, []any{hash, pid}},
			{`DELETE FROM refresh_tokens WHERE parent_id = ?`, []any{pid}},
			{`UPDATE devices SET revoked_at = ? WHERE parent_id = ? AND revoked_at IS NULL`, []any{stamp, pid}},
			{`INSERT INTO admin_audit (id, at, actor, action, target_type, target_id, detail) VALUES (?,?,'parent','parent.password_reset_used','parent',?,'password changed with a reset code')`,
				[]any{uuid.NewString(), stamp, pid}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
	})
	var ae *httpapi.APIError
	if errors.As(err, &ae) {
		return nil, err
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "reset password", err)
	}
	s.LoginLimit.Reset(email) // lifts a login lockout
	s.Log.Info("audit password.reset", "request_id", httpapi.RequestID(ctx))
	return api.ResetPassword204Response{}, nil
}
