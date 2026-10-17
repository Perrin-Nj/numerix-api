package app

// Passkeys (BE-65) with github.com/go-webauthn/webauthn.
//
// Go for Spring people: the library does the cryptography (Spring equivalent: spring-security-webauthn).
// A passkey ceremony has two calls. Begin makes a random challenge and we keep the library's SessionData
// in SQLite for 5 minutes (a server-side HttpSession). Finish gets the browser's answer, the library checks
// the signature against that session, and we store or update the credential row.

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/config"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

const (
	ceremonyTTL     = 5 * time.Minute // how long a Begin call stays valid
	maxPasskeys     = 10              // per parent, a sanity cap
	maxNicknameLen  = 60              // runes
	defaultNickname = "Passkey"       // used when the browser sends no nickname
)

// newWebAuthn builds the library instance from config. It returns nil when passkeys are not configured
// (no WEBAUTHN_RP_ID, which is the production default) or the config is invalid; handlers then answer 503.
func newWebAuthn(cfg config.Config) *webauthn.WebAuthn {
	if cfg.WebAuthnRPID == "" || len(cfg.WebAuthnOrigins) == 0 {
		return nil
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID: cfg.WebAuthnRPID, RPDisplayName: cfg.WebAuthnRPName, RPOrigins: cfg.WebAuthnOrigins,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
		},
	})
	if err != nil {
		return nil
	}
	return wa
}

// pkUser adapts a parent to the library's User interface (Spring: a UserDetails implementation).
type pkUser struct {
	id    string // parent id; its bytes are the WebAuthn "user handle"
	name  string
	creds []webauthn.Credential
}

func (u pkUser) WebAuthnID() []byte                         { return []byte(u.id) }
func (u pkUser) WebAuthnName() string                       { return u.name }
func (u pkUser) WebAuthnDisplayName() string                { return u.name }
func (u pkUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// pkFlags rebuilds the library's flag byte from the booleans we stored.
func pkFlags(uv, be, bs bool) protocol.AuthenticatorFlags {
	f := protocol.FlagUserPresent
	if uv {
		f |= protocol.FlagUserVerified
	}
	if be {
		f |= protocol.FlagBackupEligible
	}
	if bs {
		f |= protocol.FlagBackupState
	}
	return f
}

// pkLoadCreds returns every credential of one parent in the library's shape.
func (s *Server) pkLoadCreds(ctx context.Context, parentID string) ([]webauthn.Credential, error) {
	rows, err := s.Store.R.QueryContext(ctx,
		`SELECT credential_id, public_key, sign_count, aaguid, attestation_type, transports, user_verified, backup_eligible, backup_state
		 FROM passkeys WHERE parent_id = ?`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var c webauthn.Credential
		var aaguid []byte
		var count int64
		var transports string
		var uv, be, bs bool
		if err := rows.Scan(&c.ID, &c.PublicKey, &count, &aaguid, &c.AttestationType, &transports, &uv, &be, &bs); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(transports), &c.Transport)
		c.Authenticator.AAGUID, c.Authenticator.SignCount = aaguid, uint32(count)
		c.Flags = webauthn.NewCredentialFlags(pkFlags(uv, be, bs))
		out = append(out, c)
	}
	return out, rows.Err()
}

// pkParentUser loads a parent plus credentials as a library User. ok=false when the parent is gone.
func (s *Server) pkParentUser(ctx context.Context, parentID string) (u pkUser, ok bool, err error) {
	var email string
	e := s.Store.R.QueryRowContext(ctx, `SELECT email FROM parents WHERE id = ? AND deleted_at IS NULL`, parentID).Scan(&email)
	if errors.Is(e, sql.ErrNoRows) {
		return pkUser{}, false, nil
	}
	if e != nil {
		return pkUser{}, false, e
	}
	creds, err := s.pkLoadCreds(ctx, parentID)
	return pkUser{id: parentID, name: email, creds: creds}, true, err
}

// pkSave stores the SessionData of a Begin call and returns its random id (the contract's session_id).
func (s *Server) pkSave(ctx context.Context, kind, parentID string, sd *webauthn.SessionData) (string, error) {
	raw, err := json.Marshal(sd)
	if err != nil {
		return "", err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE expires_at < ?`, plTS(now)); e != nil { // tidy up old ones
			return e
		}
		var parent any // NULL for a login (nobody is known yet)
		if parentID != "" {
			parent = parentID
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO webauthn_sessions (id, kind, parent_id, data, expires_at) VALUES (?, ?, ?, ?, ?)`,
			id, kind, parent, string(raw), plTS(now.Add(ceremonyTTL)))
		return e
	})
	return id, err
}

// errCeremonyGone means the session id is unknown, already used, of the wrong kind or older than 5 minutes.
var errCeremonyGone = httpapi.NewError(410, "passkey_session_expired", "This passkey request expired. Start again.")

// pkTake loads and deletes a stored session (one try per Begin, so a failed answer cannot be retried).
func (s *Server) pkTake(ctx context.Context, id, kind string) (parentID string, sd webauthn.SessionData, err error) {
	var raw, expires string
	var parent sql.NullString
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		e := tx.QueryRowContext(ctx, `SELECT parent_id, data, expires_at FROM webauthn_sessions WHERE id = ? AND kind = ?`, id, kind).Scan(&parent, &raw, &expires)
		if errors.Is(e, sql.ErrNoRows) {
			return errCeremonyGone
		}
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE id = ?`, id)
		return e
	})
	if err != nil {
		return "", sd, err
	}
	if !time.Now().Before(plParseTS(expires)) || json.Unmarshal([]byte(raw), &sd) != nil {
		return "", sd, errCeremonyGone
	}
	return parent.String, sd, nil
}

// pkOptions converts the library's options struct to the generic JSON object of the contract.
func pkOptions(v any) (map[string]interface{}, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	return m, json.Unmarshal(raw, &m)
}

// errPasskeysOff is the 503 answer when WEBAUTHN_RP_ID is not configured.
var errPasskeysOff = httpapi.NewError(503, "passkeys_unavailable", "Passkeys are not enabled on this server.")

// BeginPasskeyRegistration answers POST /auth/passkeys/register/begin.
func (s *Server) BeginPasskeyRegistration(ctx context.Context, _ api.BeginPasskeyRegistrationRequestObject) (api.BeginPasskeyRegistrationResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if s.WebAuthn == nil {
		return nil, errPasskeysOff
	}
	u, ok, err := s.pkParentUser(ctx, p.ParentID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	if !ok {
		return nil, httpapi.ErrUnauthorized
	}
	if len(u.creds) >= maxPasskeys {
		return nil, plConflict("passkey_limit", "Remove a passkey before adding another.")
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(u.creds)) // stops registering the same authenticator twice
	for i := range u.creds {
		exclude = append(exclude, u.creds[i].Descriptor())
	}
	// Discoverable credential required: that is what lets login work without typing an email.
	creation, sd, err := s.WebAuthn.BeginRegistration(u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired), webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, s.plInternalErr(ctx, "begin passkey registration", err)
	}
	id, err := s.pkSave(ctx, "register", p.ParentID, sd)
	if err != nil {
		return nil, s.plInternalErr(ctx, "save passkey session", err)
	}
	opts, err := pkOptions(creation)
	if err != nil {
		return nil, s.plInternalErr(ctx, "encode passkey options", err)
	}
	return api.BeginPasskeyRegistration200JSONResponse{SessionId: id, Options: opts}, nil
}

// FinishPasskeyRegistration answers POST /auth/passkeys/register/finish.
func (s *Server) FinishPasskeyRegistration(ctx context.Context, req api.FinishPasskeyRegistrationRequestObject) (api.FinishPasskeyRegistrationResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if s.WebAuthn == nil {
		return nil, errPasskeysOff
	}
	if req.Body == nil || req.Body.SessionId == "" || req.Body.Credential == nil {
		return nil, httpapi.Validation(plField("session_id", "required"), plField("credential", "required"))
	}
	owner, sd, err := s.pkTake(ctx, req.Body.SessionId, "register")
	if errors.Is(err, errCeremonyGone) {
		return nil, errCeremonyGone
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load passkey session", err)
	}
	if owner != p.ParentID { // a session id from another parent: treat as unknown
		return nil, errCeremonyGone
	}
	u, ok, err := s.pkParentUser(ctx, p.ParentID)
	if err != nil || !ok {
		return nil, s.plInternalErr(ctx, "load parent", errors.Join(err, errors.New("parent missing")))
	}
	raw, _ := json.Marshal(req.Body.Credential)
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(raw))
	if err != nil {
		return nil, httpapi.NewError(422, "passkey_invalid", "The passkey answer could not be read.")
	}
	cred, err := s.WebAuthn.CreateCredential(u, sd, parsed) // verifies challenge, origin, RP id and attestation
	if err != nil {
		return nil, httpapi.NewError(422, "passkey_invalid", "The passkey could not be verified.")
	}
	nick := defaultNickname
	if req.Body.Nickname != nil && strings.TrimSpace(*req.Body.Nickname) != "" {
		nick = strings.TrimSpace(*req.Body.Nickname)
	}
	if r := []rune(nick); len(r) > maxNicknameLen {
		nick = string(r[:maxNicknameLen])
	}
	transports, _ := json.Marshal(cred.Transport)
	if cred.Transport == nil {
		transports = []byte("[]")
	}
	id, now := uuid.NewString(), time.Now()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`INSERT INTO passkeys (id, parent_id, credential_id, public_key, sign_count, aaguid, attestation_type, transports,
			   user_verified, backup_eligible, backup_state, nickname, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, p.ParentID, cred.ID, cred.PublicKey, int64(cred.Authenticator.SignCount), cred.Authenticator.AAGUID, cred.AttestationType,
			string(transports), cred.Flags.UserVerified, cred.Flags.BackupEligible, cred.Flags.BackupState, nick, plTS(now))
		return e
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return nil, plConflict("passkey_exists", "This passkey is already registered.")
		}
		return nil, s.plInternalErr(ctx, "store passkey", err)
	}
	return api.FinishPasskeyRegistration201JSONResponse(pkToAPI(id, nick, cred.Authenticator.AAGUID, cred.Transport, cred.Flags.BackupEligible, cred.Flags.BackupState, now.UTC(), nil)), nil
}

// pkToAPI builds the contract's Passkey object.
func pkToAPI(id, nick string, aaguid []byte, transports []protocol.AuthenticatorTransport, be, bs bool, created time.Time, used *time.Time) api.Passkey {
	out := api.Passkey{Id: plUUID(id), Nickname: nick, Transports: []string{}, BackupEligible: be, BackupState: bs, CreatedAt: created, LastUsedAt: used}
	for _, t := range transports {
		out.Transports = append(out.Transports, string(t))
	}
	if len(aaguid) > 0 && !bytes.Equal(aaguid, make([]byte, len(aaguid))) { // all zeros means "unknown model"
		h := hex.EncodeToString(aaguid)
		out.Aaguid = &h
	}
	return out
}

// BeginPasskeyLogin answers POST /auth/passkeys/login/begin (no username: the authenticator picks the account).
func (s *Server) BeginPasskeyLogin(ctx context.Context, _ api.BeginPasskeyLoginRequestObject) (api.BeginPasskeyLoginResponseObject, error) {
	if s.WebAuthn == nil {
		return nil, errPasskeysOff
	}
	assertion, sd, err := s.WebAuthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, s.plInternalErr(ctx, "begin passkey login", err)
	}
	id, err := s.pkSave(ctx, "login", "", sd)
	if err != nil {
		return nil, s.plInternalErr(ctx, "save passkey session", err)
	}
	opts, err := pkOptions(assertion)
	if err != nil {
		return nil, s.plInternalErr(ctx, "encode passkey options", err)
	}
	return api.BeginPasskeyLogin200JSONResponse{SessionId: id, Options: opts}, nil
}

// FinishPasskeyLogin answers POST /auth/passkeys/login/finish. Every failure after the session is found is a plain 401.
func (s *Server) FinishPasskeyLogin(ctx context.Context, req api.FinishPasskeyLoginRequestObject) (api.FinishPasskeyLoginResponseObject, error) {
	if s.WebAuthn == nil {
		return nil, errPasskeysOff
	}
	if req.Body == nil || req.Body.SessionId == "" || req.Body.Credential == nil {
		return nil, httpapi.Validation(plField("session_id", "required"), plField("credential", "required"))
	}
	_, sd, err := s.pkTake(ctx, req.Body.SessionId, "login")
	if errors.Is(err, errCeremonyGone) {
		return nil, errCeremonyGone
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load passkey session", err)
	}
	raw, _ := json.Marshal(req.Body.Credential)
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(raw))
	if err != nil {
		return nil, httpapi.ErrUnauthorized
	}
	// The library calls this with the credential id and the user handle from the authenticator, and expects the user back.
	find := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, ok, err := s.pkParentUser(ctx, string(userHandle))
		if err != nil || !ok {
			return nil, errors.New("unknown user")
		}
		return u, nil
	}
	user, cred, err := s.WebAuthn.ValidatePasskeyLogin(find, sd, parsed)
	if err != nil || cred.Authenticator.CloneWarning { // bad signature, wrong origin, or counter went backwards (cloned key)
		return nil, httpapi.ErrUnauthorized
	}
	parentID := string(user.WebAuthnID())
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`UPDATE passkeys SET sign_count = ?, backup_state = ?, user_verified = ?, last_used_at = ? WHERE credential_id = ? AND parent_id = ?`,
			int64(cred.Authenticator.SignCount), cred.Flags.BackupState, cred.Flags.UserVerified, plTS(time.Now()), cred.ID, parentID)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "update passkey", err)
	}
	var email, locale string
	err = s.Store.R.QueryRowContext(ctx, `SELECT email, locale FROM parents WHERE id = ? AND deleted_at IS NULL`, parentID).Scan(&email, &locale)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpapi.ErrUnauthorized
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	sess, err := s.plSession(ctx, parentID, email, locale)
	if err != nil {
		return nil, err
	}
	s.startDevice(ctx, parentID)
	return api.FinishPasskeyLogin200JSONResponse(sess), nil
}

// ListPasskeys answers GET /account/passkeys.
func (s *Server) ListPasskeys(ctx context.Context, _ api.ListPasskeysRequestObject) (api.ListPasskeysResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.R.QueryContext(ctx,
		`SELECT id, nickname, aaguid, transports, backup_eligible, backup_state, created_at, last_used_at
		 FROM passkeys WHERE parent_id = ? ORDER BY created_at`, p.ParentID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list passkeys", err)
	}
	defer rows.Close()
	out := api.PasskeyList{Items: []api.Passkey{}}
	for rows.Next() {
		var id, nick, transports, created string
		var aaguid []byte
		var be, bs bool
		var used sql.NullString
		if err := rows.Scan(&id, &nick, &aaguid, &transports, &be, &bs, &created, &used); err != nil {
			return nil, s.plInternalErr(ctx, "scan passkey", err)
		}
		var tr []protocol.AuthenticatorTransport
		_ = json.Unmarshal([]byte(transports), &tr)
		out.Items = append(out.Items, pkToAPI(id, nick, aaguid, tr, be, bs, plParseTS(created), plNullTime(used)))
	}
	if err := rows.Err(); err != nil {
		return nil, s.plInternalErr(ctx, "list passkeys", err)
	}
	return api.ListPasskeys200JSONResponse(out), nil
}

// DeletePasskey answers DELETE /account/passkeys/{passkeyId}. Another parent's id looks like a missing one (404).
func (s *Server) DeletePasskey(ctx context.Context, req api.DeletePasskeyRequestObject) (api.DeletePasskeyResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	var n int64
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		res, e := tx.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND parent_id = ?`, openapi_types.UUID(req.PasskeyId).String(), p.ParentID)
		if e != nil {
			return e
		}
		n, e = res.RowsAffected()
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "delete passkey", err)
	}
	if n == 0 {
		return nil, httpapi.ErrNotFound
	}
	return api.DeletePasskey204Response{}, nil
}
