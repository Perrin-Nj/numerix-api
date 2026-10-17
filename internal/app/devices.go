package app

// Device sessions (BE-64, BE-66): a known browser keeps a long-lived cookie so the parent does not sign in
// every time, and a child on that browser can skip the PIN when the parent allowed it.
//
// Go for Spring people: this file is a @Service plus a @RestController in one. The cookie value is 256
// random bits; the database keeps only its SHA-256, so a database leak gives nobody a usable cookie.
// "Rotate on each use" means every successful POST /auth/device swaps the value for a new one; an old
// value then matches nothing, which is how a stolen copy gets noticed (the real browser is logged out too).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/config"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

const (
	deviceCookie = "nx_device"          // cookie name (contract BE-64)
	deviceTTL    = 180 * 24 * time.Hour // lifetime, slides forward on every rotation
)

// errDeviceInvalid means the cookie is unknown, expired, revoked, reused or belongs to someone else.
var errDeviceInvalid = errors.New("device token invalid")

// devHash returns the hex SHA-256 of a device token (what the database stores).
func devHash(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// devNewToken returns 32 random bytes as URL-safe text (cookie-safe characters only).
func devNewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// devUserAgent turns a User-Agent header into (browser family, label such as "Chrome on Windows").
// Only the family is kept, never the full header (Law 25: collect the least).
// shortcut: substring matching, not a full UA parser; swap for a library if labels look wrong.
func devUserAgent(ua string) (family, label string) {
	family = "Other"
	switch {
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "EdgA/") || strings.Contains(ua, "EdgiOS/"):
		family = "Edge"
	case strings.Contains(ua, "Firefox/") || strings.Contains(ua, "FxiOS/"):
		family = "Firefox"
	case strings.Contains(ua, "Chrome/") || strings.Contains(ua, "CriOS/"):
		family = "Chrome"
	case strings.Contains(ua, "Safari/"):
		family = "Safari"
	}
	osName := ""
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		osName = "iOS"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		osName = "macOS"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	if osName == "" {
		return family, family
	}
	return family, family + " on " + osName
}

// setDeviceCookie sends the cookie: HttpOnly (scripts cannot read it), Secure except in development,
// SameSite=Lax, Path=/, 180 days.
func (s *Server) setDeviceCookie(ctx context.Context, tok string) {
	httpapi.SetCookie(ctx, &http.Cookie{
		Name: deviceCookie, Value: tok, Path: "/", MaxAge: int(deviceTTL.Seconds()),
		HttpOnly: true, Secure: s.Config.Env != config.EnvDevelopment, SameSite: http.SameSiteLaxMode,
	})
}

// clearDeviceCookie tells the browser to delete the cookie (same attributes, MaxAge -1).
func (s *Server) clearDeviceCookie(ctx context.Context) {
	httpapi.SetCookie(ctx, &http.Cookie{
		Name: deviceCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Config.Env != config.EnvDevelopment, SameSite: http.SameSiteLaxMode,
	})
}

// rotateDevice checks a cookie value and swaps it for a fresh one. wantParent, when not empty, must own the
// device (so a login as parent B never rotates the cookie of parent A). Returns (parentID, deviceID, newToken).
func (s *Server) rotateDevice(ctx context.Context, plain, wantParent string) (parentID, deviceID, next string, err error) {
	next, err = devNewToken()
	if err != nil {
		return "", "", "", err
	}
	now := time.Now()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error { // Spring: @Transactional
		var expires string
		var revoked sql.NullString
		e := tx.QueryRowContext(ctx, `SELECT id, parent_id, expires_at, revoked_at FROM devices WHERE token_hash = ?`,
			devHash(plain)).Scan(&deviceID, &parentID, &expires, &revoked)
		if errors.Is(e, sql.ErrNoRows) {
			return errDeviceInvalid
		}
		if e != nil {
			return e
		}
		if revoked.Valid || !now.Before(plParseTS(expires)) || (wantParent != "" && wantParent != parentID) {
			return errDeviceInvalid
		}
		_, e = tx.ExecContext(ctx, `UPDATE devices SET token_hash = ?, last_used_at = ?, expires_at = ? WHERE id = ?`,
			devHash(next), plTS(now), plTS(now.Add(deviceTTL)), deviceID)
		return e
	})
	if err != nil {
		return "", "", "", err
	}
	return parentID, deviceID, next, nil
}

// startDevice runs after register, login and passkey login. It rotates the cookie the browser already
// has for this parent, or registers a new device. Best effort: a failure is logged and the sign in still works.
func (s *Server) startDevice(ctx context.Context, parentID string) {
	if cur := httpapi.ReadCookie(ctx, deviceCookie); cur != "" {
		if _, _, next, err := s.rotateDevice(ctx, cur, parentID); err == nil {
			s.setDeviceCookie(ctx, next)
			return
		}
	}
	tok, err := devNewToken()
	if err != nil {
		s.Log.Error("device token", "err", err)
		return
	}
	family, label := devUserAgent(httpapi.UserAgent(ctx))
	now := time.Now()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`INSERT INTO devices (id, parent_id, token_hash, label, user_agent_family, created_at, last_used_at, expires_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), parentID, devHash(tok), label, family, plTS(now), plTS(now), plTS(now.Add(deviceTTL)))
		return e
	})
	if err != nil {
		s.Log.Error("store device", "request_id", httpapi.RequestID(ctx), "err", err)
		return
	}
	s.setDeviceCookie(ctx, tok)
}

// revokeCurrentDevice is called by logout: the device behind the cookie stops working and the cookie is deleted.
func (s *Server) revokeCurrentDevice(ctx context.Context) {
	cur := httpapi.ReadCookie(ctx, deviceCookie)
	if cur == "" {
		return
	}
	err := s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE devices SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, plTS(time.Now()), devHash(cur))
		return e
	})
	if err != nil {
		s.Log.Error("revoke device", "request_id", httpapi.RequestID(ctx), "err", err)
	}
	s.clearDeviceCookie(ctx)
}

// deviceSkipsPIN tells whether a child may start without the PIN: the request carries a valid device cookie
// of the SAME parent AND the parent switched remember_on_device on for this child. It never rotates the cookie.
func (s *Server) deviceSkipsPIN(ctx context.Context, parentID, childID string) bool {
	cur := httpapi.ReadCookie(ctx, deviceCookie)
	if cur == "" {
		return false
	}
	var expires string
	var revoked sql.NullString
	err := s.Store.R.QueryRowContext(ctx, `SELECT expires_at, revoked_at FROM devices WHERE token_hash = ? AND parent_id = ?`,
		devHash(cur), parentID).Scan(&expires, &revoked)
	if err != nil || revoked.Valid || !time.Now().Before(plParseTS(expires)) {
		return false
	}
	var raw string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT json FROM child_settings WHERE child_id = ?`, childID).Scan(&raw); err != nil {
		return false // no saved settings means default false
	}
	var st struct {
		Remember bool `json:"remember_on_device"`
	}
	return json.Unmarshal([]byte(raw), &st) == nil && st.Remember
}

// DeviceLogin answers POST /auth/device: cookie in, rotated cookie and a fresh session out.
func (s *Server) DeviceLogin(ctx context.Context, _ api.DeviceLoginRequestObject) (api.DeviceLoginResponseObject, error) {
	cur := httpapi.ReadCookie(ctx, deviceCookie)
	if cur == "" {
		return nil, httpapi.ErrUnauthorized
	}
	parentID, _, next, err := s.rotateDevice(ctx, cur, "")
	if errors.Is(err, errDeviceInvalid) {
		s.clearDeviceCookie(ctx) // dead cookie: stop the browser sending it
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "rotate device", err)
	}
	var email, locale string
	err = s.Store.R.QueryRowContext(ctx, `SELECT email, locale FROM parents WHERE id = ? AND deleted_at IS NULL`, parentID).Scan(&email, &locale)
	if errors.Is(err, sql.ErrNoRows) {
		s.clearDeviceCookie(ctx)
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	sess, err := s.plSession(ctx, parentID, email, locale)
	if err != nil {
		return nil, err
	}
	s.setDeviceCookie(ctx, next)
	return api.DeviceLogin200JSONResponse(sess), nil
}

// ListDevices answers GET /account/devices: active devices of the caller, newest use first.
func (s *Server) ListDevices(ctx context.Context, _ api.ListDevicesRequestObject) (api.ListDevicesResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	curHash := ""
	if cur := httpapi.ReadCookie(ctx, deviceCookie); cur != "" {
		curHash = devHash(cur)
	}
	rows, err := s.Store.R.QueryContext(ctx,
		`SELECT id, token_hash, label, user_agent_family, created_at, last_used_at FROM devices
		 WHERE parent_id = ? AND revoked_at IS NULL AND expires_at > ? ORDER BY last_used_at DESC`, p.ParentID, plTS(time.Now()))
	if err != nil {
		return nil, s.plInternalErr(ctx, "list devices", err)
	}
	defer rows.Close()
	out := api.DeviceList{Items: []api.Device{}}
	for rows.Next() {
		var id, hash, label, family, created, used string
		if err := rows.Scan(&id, &hash, &label, &family, &created, &used); err != nil {
			return nil, s.plInternalErr(ctx, "scan device", err)
		}
		out.Items = append(out.Items, api.Device{
			Id: openapi_types.UUID(uuid.MustParse(id)), Label: label, UserAgentFamily: family,
			CreatedAt: plParseTS(created), LastUsedAt: plParseTS(used), Current: curHash != "" && hash == curHash,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, s.plInternalErr(ctx, "list devices", err)
	}
	return api.ListDevices200JSONResponse(out), nil
}

// RevokeDevice answers DELETE /account/devices/{deviceId}. Only the owner's own devices are visible (404 otherwise).
func (s *Server) RevokeDevice(ctx context.Context, req api.RevokeDeviceRequestObject) (api.RevokeDeviceResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	var n int64
	var hash string
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if e := tx.QueryRowContext(ctx, `SELECT token_hash FROM devices WHERE id = ? AND parent_id = ? AND revoked_at IS NULL`,
			plUID(req.DeviceId), p.ParentID).Scan(&hash); errors.Is(e, sql.ErrNoRows) {
			return nil // n stays 0 -> 404
		} else if e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE devices SET revoked_at = ? WHERE id = ?`, plTS(time.Now()), plUID(req.DeviceId))
		if e != nil {
			return e
		}
		n, e = res.RowsAffected()
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "revoke device", err)
	}
	if n == 0 {
		return nil, httpapi.ErrNotFound
	}
	if cur := httpapi.ReadCookie(ctx, deviceCookie); cur != "" && devHash(cur) == hash {
		s.clearDeviceCookie(ctx) // the parent forgot the device they are using
	}
	return api.RevokeDevice204Response{}, nil
}
