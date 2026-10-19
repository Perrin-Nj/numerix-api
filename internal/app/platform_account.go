package app

// Parent account: getAccount, updateAccount, deleteAccount (Law 25 erasure, BE-36, BE-55).

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// plAccount loads the parent as the Account model; the name defaults to the email prefix.
func (s *Server) plAccount(ctx context.Context, parentID string) (api.Account, error) {
	var email, locale, created string
	var name sql.NullString
	err := s.Store.R.QueryRowContext(ctx,
		`SELECT email, name, locale, created_at FROM parents WHERE id = ? AND deleted_at IS NULL`, parentID).Scan(&email, &name, &locale, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return api.Account{}, httpapi.ErrUnauthorized // the account is gone: the token is no longer good
	}
	if err != nil {
		return api.Account{}, s.plInternalErr(ctx, "load account", err)
	}
	n := name.String
	if n == "" {
		n = strings.SplitN(email, "@", 2)[0]
	}
	t := plParseTS(created)
	return api.Account{Id: plUUID(parentID), Email: openapi_types.Email(email), Name: n, Locale: api.AccountLocale(locale), CreatedAt: &t}, nil
}

// GetAccount returns the caller's account.
func (s *Server) GetAccount(ctx context.Context, _ api.GetAccountRequestObject) (api.GetAccountResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.plAccount(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	return api.GetAccount200JSONResponse(a), nil
}

// UpdateAccount changes name and or locale; later notifications use the new locale.
func (s *Server) UpdateAccount(ctx context.Context, req api.UpdateAccountRequestObject) (api.UpdateAccountResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	var bad []httpapi.FieldError
	if b.Name != nil {
		*b.Name = strings.TrimSpace(*b.Name)
		if n := utf8.RuneCountInString(*b.Name); n < 1 || n > 80 {
			bad = append(bad, plField("name", "1 to 80 characters"))
		}
	}
	if b.Locale != nil && !plValidLocale(string(*b.Locale)) {
		bad = append(bad, plField("locale", "fr-CA or en-CA"))
	}
	if len(bad) > 0 {
		return nil, httpapi.Validation(bad...)
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		// COALESCE(?, col): a NULL parameter keeps the current value, so absent fields are not touched.
		var name, loc any
		if b.Name != nil {
			name = *b.Name
		}
		if b.Locale != nil {
			loc = string(*b.Locale)
		}
		_, e := tx.ExecContext(ctx, `UPDATE parents SET name = COALESCE(?, name), locale = COALESCE(?, locale) WHERE id = ? AND deleted_at IS NULL`,
			name, loc, p.ParentID)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "update account", err)
	}
	a, err := s.plAccount(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	return api.UpdateAccount200JSONResponse(a), nil
}

// DeleteAccount erases the family now and leaves a tombstone parent row for 30 days (purge_after).
// The tombstone has no email, no name, no password, no children, no tokens: it only lets the purge job
// (BE-41) and the consent evidence (kept 2 years, no foreign key) stay consistent.
// A wrong password answers 401 like the guide says (and counts toward the same lockout as login).
func (s *Server) DeleteAccount(ctx context.Context, req api.DeleteAccountRequestObject) (api.DeleteAccountResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || req.Body.Password == "" || len([]rune(req.Body.Password)) > plPasswordMax {
		return nil, httpapi.Validation(plField("password", "required, 128 characters at most"))
	}
	key := "del:" + p.ParentID
	if locked, wait := s.LoginLimit.Locked(key); locked {
		return nil, httpapi.RateLimited(httpapi.RetrySeconds(wait))
	}
	var hash string
	err = s.Store.R.QueryRowContext(ctx, `SELECT password_hash FROM parents WHERE id = ? AND deleted_at IS NULL`, p.ParentID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	if ok, _ := auth.Verify(req.Body.Password, hash); !ok {
		s.LoginLimit.Fail(key)
		return nil, httpapi.ErrUnauthorized
	}
	// Cancel at the processor first: if that fails we have deleted nothing and the parent can retry.
	var ref sql.NullString
	_ = s.Store.R.QueryRowContext(ctx, `SELECT provider_ref FROM subscriptions WHERE parent_id = ?`, p.ParentID).Scan(&ref)
	if prov := s.plPayments(); prov != nil && ref.Valid {
		if err := prov.Cancel(ctx, ref.String); err != nil {
			return nil, s.plInternalErr(ctx, "cancel at payment provider", err)
		}
	}
	now := time.Now()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if s.Sync.Enabled() { // queue Mongo erasure for every child before the rows disappear
			rows, e := tx.QueryContext(ctx, `SELECT id FROM children WHERE parent_id = ?`, p.ParentID)
			if e != nil {
				return e
			}
			var kids []string
			for rows.Next() {
				var id string
				if e := rows.Scan(&id); e != nil {
					_ = rows.Close()
					return e
				}
				kids = append(kids, id)
			}
			if e := errors.Join(rows.Err(), rows.Close()); e != nil {
				return e
			}
			for _, id := range kids {
				if e := s.syncChildRemoved(ctx, tx, id, p.ParentID); e != nil {
					return e
				}
			}
		}
		// Children first: foreign keys cascade to settings, sessions, beats, answers, mastery, cards, feed, quests.
		for _, q := range []string{
			`DELETE FROM children WHERE parent_id = ?`,
			`DELETE FROM subscriptions WHERE parent_id = ?`,
			`DELETE FROM notifications WHERE parent_id = ?`,
			`DELETE FROM push_subscriptions WHERE parent_id = ?`,
			`DELETE FROM refresh_tokens WHERE parent_id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, p.ParentID); err != nil {
				return err
			}
		}
		// Frees the email for a new sign-up and invalidates every login (password_hash '!' never verifies).
		_, err := tx.ExecContext(ctx,
			`UPDATE parents SET email = 'deleted+' || id || '@deleted.invalid', password_hash = '!', name = NULL,
			 deleted_at = ?, purge_after = ? WHERE id = ?`,
			plTS(now), plTS(now.Add(30*plDay)), p.ParentID)
		return err
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "delete account", err)
	}
	s.Log.Info("audit account.delete", "request_id", httpapi.RequestID(ctx), "parent_id", p.ParentID)
	return api.DeleteAccount204Response{}, nil
}
