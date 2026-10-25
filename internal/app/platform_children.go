package app

// Child profiles: list, create, get, update, delete, play token (PIN), settings, Law 25 export.
//
// Go for Spring people: SQL runs through database/sql (JDBC). `QueryRowContext(...).Scan(&a, &b)` reads one
// row into variables (a RowMapper); sql.NullString / sql.NullInt64 hold columns that may be NULL
// (Spring: Optional or a nullable Integer). `?` are bind parameters, never build SQL with string concatenation of user input.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// ListChildren returns the parent's profiles (at most 8, not paginated).
func (s *Server) ListChildren(ctx context.Context, _ api.ListChildrenRequestObject) (api.ListChildrenResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.R.QueryContext(ctx,
		`SELECT `+plChildCols+` FROM children c WHERE c.parent_id = ? AND c.deleted_at IS NULL ORDER BY c.created_at`, p.ParentID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list children", err)
	}
	defer rows.Close() // like try-with-resources: always release the cursor
	items := []api.Child{}
	for rows.Next() {
		c, err := plScanChild(rows)
		if err != nil {
			return nil, s.plInternalErr(ctx, "scan child", err)
		}
		items = append(items, c.toAPI())
	}
	if err := rows.Err(); err != nil {
		return nil, s.plInternalErr(ctx, "list children", err)
	}
	return api.ListChildren200JSONResponse{Items: items}, nil
}

// plValidTZ tells whether name is an IANA zone.
func plValidTZ(name string) bool {
	_, err := time.LoadLocation(name)
	return name != "" && err == nil
}

// CreateChild adds a profile (no consent yet, so consent_given is false).
func (s *Server) CreateChild(ctx context.Context, req api.CreateChildRequestObject) (api.CreateChildResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	name, tz := strings.TrimSpace(b.DisplayName), "America/Toronto"
	if b.Timezone != nil {
		tz = *b.Timezone
	}
	var bad []httpapi.FieldError
	if n := utf8.RuneCountInString(name); n < 1 || n > 30 {
		bad = append(bad, plField("display_name", "1 to 30 characters"))
	}
	if b.Grade < 1 || b.Grade > 4 {
		bad = append(bad, plField("grade", "1 to 4"))
	}
	if b.AvatarId == "" || len(b.AvatarId) > 40 {
		bad = append(bad, plField("avatar_id", "required, 40 characters at most"))
	}
	if b.Pin != nil && !plPinRe.MatchString(*b.Pin) {
		bad = append(bad, plField("pin", "exactly 4 digits"))
	}
	if !plValidTZ(tz) {
		bad = append(bad, plField("timezone", "unknown IANA zone"))
	}
	if len(bad) > 0 {
		return nil, httpapi.Validation(bad...)
	}
	var pinHash any // nil = SQL NULL = no PIN
	if b.Pin != nil && *b.Pin != "" {
		h, err := auth.Hash(*b.Pin)
		if err != nil {
			return nil, s.plInternalErr(ctx, "hash pin", err)
		}
		pinHash = h
	}
	birth := time.Now().Year() - (b.Grade + 5)
	if b.BirthYear != nil {
		birth = *b.BirthYear
	}
	settings, _ := json.Marshal(s.plDefaultSettings(ctx))
	id := uuid.NewString()
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var n int // count and insert in one transaction so two requests cannot both pass the limit
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM children WHERE parent_id = ? AND deleted_at IS NULL`, p.ParentID).Scan(&n); err != nil {
			return err
		}
		if n >= plMaxChildren {
			return httpapi.Validation(plField("display_name", "a family holds at most 8 profiles"))
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO children (id, parent_id, display_name, birth_year, grade, avatar_id, pin_hash, timezone) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, p.ParentID, name, birth, b.Grade, b.AvatarId, pinHash, tz); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO child_settings (child_id, json) VALUES (?, ?)`, id, string(settings))
		return err
	})
	var ae *httpapi.APIError
	if errors.As(err, &ae) {
		return nil, ae
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "create child", err)
	}
	c, err := s.plOwnedChild(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return api.CreateChild201JSONResponse(c.toAPI()), nil
}

// GetChild returns one profile (parent of the child, or the child itself).
func (s *Server) GetChild(ctx context.Context, req api.GetChildRequestObject) (api.GetChildResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	return api.GetChild200JSONResponse(c.toAPI()), nil
}

// UpdateChild changes the given fields only. shortcut: the generated model cannot tell `"pin": null` from
// an absent pin, so an empty string "" removes the PIN; ask the contract owner to make pin nullable.
func (s *Server) UpdateChild(ctx context.Context, req api.UpdateChildRequestObject) (api.UpdateChildResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	id := plUID(req.ChildId)
	cur, err := s.plOwnedChild(ctx, p, id)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	var bad []httpapi.FieldError
	if b.DisplayName != nil {
		*b.DisplayName = strings.TrimSpace(*b.DisplayName)
		if n := utf8.RuneCountInString(*b.DisplayName); n < 1 || n > 30 {
			bad = append(bad, plField("display_name", "1 to 30 characters"))
		}
	}
	if b.Grade != nil && (*b.Grade < 1 || *b.Grade > 4) {
		bad = append(bad, plField("grade", "1 to 4"))
	}
	if b.AvatarId != nil && (*b.AvatarId == "" || len(*b.AvatarId) > 40) {
		bad = append(bad, plField("avatar_id", "required, 40 characters at most"))
	}
	if b.Pin != nil && *b.Pin != "" && !plPinRe.MatchString(*b.Pin) {
		bad = append(bad, plField("pin", "exactly 4 digits"))
	}
	if b.Timezone != nil && !plValidTZ(*b.Timezone) {
		bad = append(bad, plField("timezone", "unknown IANA zone"))
	}
	if len(bad) > 0 {
		return nil, httpapi.Validation(bad...)
	}
	name, grade, avatar, tz, onboarded, pin := cur.DisplayName, int(cur.Grade.Int64), cur.AvatarID.String, cur.Timezone, cur.Onboarded, cur.PinHash
	if b.DisplayName != nil {
		name = *b.DisplayName
	}
	if b.Grade != nil {
		grade = *b.Grade
	}
	if b.AvatarId != nil {
		avatar = *b.AvatarId
	}
	if b.Timezone != nil {
		tz = *b.Timezone
	}
	if b.OnboardingCompleted != nil {
		onboarded = *b.OnboardingCompleted
	}
	if b.Pin != nil {
		pin = sql.NullString{}
		if *b.Pin != "" {
			h, err := auth.Hash(*b.Pin)
			if err != nil {
				return nil, s.plInternalErr(ctx, "hash pin", err)
			}
			pin = sql.NullString{String: h, Valid: true}
			s.PINLimit.Reset(id) // a new PIN starts with a clean slate
		}
	}
	flag := 0
	if onboarded {
		flag = 1
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`UPDATE children SET display_name = ?, grade = ?, avatar_id = ?, timezone = ?, onboarding_completed = ?, pin_hash = ?
			 WHERE id = ? AND parent_id = ?`, name, grade, avatar, tz, flag, pin, id, p.ParentID)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "update child", err)
	}
	c, err := s.plOwnedChild(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return api.UpdateChild200JSONResponse(c.toAPI()), nil
}

// DeleteChild removes the profile and everything under it (foreign keys cascade). The consent evidence
// has no foreign key on purpose and stays for 2 years (BE-39).
// shortcut: a child JWT already issued stays valid until it expires (8 h); add a deny list checked by the guard (guide 22.4).
func (s *Server) DeleteChild(ctx context.Context, req api.DeleteChildRequestObject) (api.DeleteChildResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	id := plUID(req.ChildId)
	if _, err := s.plOwnedChild(ctx, p, id); err != nil {
		return nil, err
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `DELETE FROM children WHERE id = ? AND parent_id = ?`, id, p.ParentID); e != nil {
			return e
		}
		return s.syncChildRemoved(ctx, tx, id, p.ParentID) // erase the child's pseudonymous Mongo documents (Law 25)
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "delete child", err)
	}
	s.PINLimit.Reset(id)
	return api.DeleteChild204Response{}, nil
}

// CreateChildToken switches to play mode: checks ownership, lockout, consent, then the PIN (contract 2.5).
func (s *Server) CreateChildToken(ctx context.Context, req api.CreateChildTokenRequestObject) (api.CreateChildTokenResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	id := plUID(req.ChildId)
	c, err := s.plOwnedChild(ctx, p, id)
	if err != nil {
		return nil, err
	}
	if locked, wait := s.PINLimit.Locked(id); locked { // 5 wrong PINs in 15 minutes
		return nil, httpapi.RateLimited(httpapi.RetrySeconds(wait))
	}
	if !c.Consented {
		return nil, httpapi.NewError(http.StatusForbidden, "consent_missing", "A parent must give consent before this child can play.")
	}
	if c.PinHash.Valid && c.PinHash.String != "" && !s.deviceSkipsPIN(ctx, p.ParentID, id) { // known device + remember_on_device skips the PIN
		pin := ""
		if req.Body != nil && req.Body.Pin != nil {
			pin = *req.Body.Pin
		}
		ok := len(pin) <= 8 // a PIN is 4 digits: anything long is wrong, and never reaches argon2 (QA-BE-001)
		if ok {
			ok, _ = auth.Verify(pin, c.PinHash.String) // an empty pin simply fails the check
		}
		if !ok {
			s.PINLimit.Fail(id)
			return nil, httpapi.NewError(http.StatusForbidden, "invalid_pin", "That PIN does not match.")
		}
		s.PINLimit.Reset(id)
	}
	token, expires, err := s.Issuer.IssueChild(p.ParentID, id)
	if err != nil {
		return nil, s.plInternalErr(ctx, "issue child token", err)
	}
	return api.CreateChildToken200JSONResponse{AccessToken: token, ExpiresIn: expires, Child: c.toAPI()}, nil
}

// ───────────── Settings ─────────────

// enabledWorlds returns the worlds a child may play: the saved enabled_worlds, or plDefaultWorlds when nothing is saved.
// (Spring: a repository read plus a default, used by every screen that lists worlds.)
func (s *Server) enabledWorlds(ctx context.Context, childID string) []string {
	var raw string
	if s.Store.R.QueryRowContext(ctx, `SELECT json FROM child_settings WHERE child_id = ?`, childID).Scan(&raw) != nil {
		return plDefaultWorlds // no settings row yet
	}
	var st struct {
		Worlds []string `json:"enabled_worlds"` // same JSON key the settings API stores
	}
	if json.Unmarshal([]byte(raw), &st) != nil || len(st.Worlds) == 0 {
		return plDefaultWorlds
	}
	return st.Worlds
}

// plDefaultSettings are the settings of a new child (daily limit from the platform default).
func (s *Server) plDefaultSettings(ctx context.Context) api.ChildSettings {
	worlds := make([]api.WorldId, len(plDefaultWorlds)) // only the focus worlds start enabled
	for i, w := range plDefaultWorlds {
		worlds[i] = api.WorldId(w)
	}
	var limit *int
	if d := s.plConfig(ctx).DailyTimeLimitMinutesDefault; d != nil && *d > 0 {
		v := *d
		limit = &v
	}
	cfg, mu, vo := s.plConfig(ctx), appconfig.Get[appconfig.Music](ctx, s.Cfg, appconfig.NSMusic), appconfig.Get[appconfig.Voice](ctx, s.Cfg, appconfig.NSVoice)
	return api.ChildSettings{DailyGoalChapters: cfg.DailyGoalDefault, DailyTimeLimitMinutes: limit, EnabledWorlds: worlds,
		KidCanChangeLanguage: plPtr(true), ReadAloud: true, Sound: true,
		Music: plPtr(api.MusicSettings{Enabled: mu.Enabled, Volume: mu.DefaultVolume}), Voice: plPtr(api.VoiceSettings{HumanEnabled: vo.HumanEnabledDefault})}
}

// defaultMusic: music on at a low volume (it still starts only after the first tap, client side), not locked.
func defaultMusic() api.MusicSettings { return api.MusicSettings{Enabled: true, Volume: 30} }

// defaultVoice: human clips on, the computer voice off until a parent opts in.
func defaultVoice() api.VoiceSettings { return api.VoiceSettings{HumanEnabled: true} }

// fillSettings gives settings stored before R3 their music and voice blocks, and enforces the parent lock.
func fillSettings(st *api.ChildSettings) {
	if st.Music == nil {
		st.Music = plPtr(defaultMusic())
	}
	if st.Voice == nil {
		st.Voice = plPtr(defaultVoice())
	}
	if st.Music.ParentLocked {
		st.Music.Enabled = false // a locked music switch always reads as off
	}
}

// mergeChildSettings applies a child's PUT: only its own music on/off and volume and the human voice switch
// change; everything else, including the parent lock and the computer voice, keeps the stored value.
func mergeChildSettings(stored, in api.ChildSettings) api.ChildSettings {
	out := stored
	if in.Music != nil {
		m := *out.Music // copy, so the stored pointer is not edited through the alias
		m.Volume = in.Music.Volume
		if !m.ParentLocked {
			m.Enabled = in.Music.Enabled
		}
		out.Music = &m
	}
	if in.Voice != nil {
		v := *out.Voice
		v.HumanEnabled = in.Voice.HumanEnabled
		out.Voice = &v
	}
	return out
}

// GetChildSettings reads the settings (a child token may read its own).
func (s *Server) GetChildSettings(ctx context.Context, req api.GetChildSettingsRequestObject) (api.GetChildSettingsResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	id := plUID(req.ChildId)
	if _, err := s.plOwnedChild(ctx, p, id); err != nil {
		return nil, err
	}
	out := s.plDefaultSettings(ctx)
	var raw string
	err = s.Store.R.QueryRowContext(ctx, `SELECT json FROM child_settings WHERE child_id = ?`, id).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal([]byte(raw), &out)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, s.plInternalErr(ctx, "load settings", err)
	}
	if out.KidCanChangeLanguage == nil {
		out.KidCanChangeLanguage = plPtr(true)
	}
	fillSettings(&out)
	return api.GetChildSettings200JSONResponse(out), nil
}

// UpdateChildSettings replaces the settings after validating them (BE-28 flag included).
func (s *Server) UpdateChildSettings(ctx context.Context, req api.UpdateChildSettingsRequestObject) (api.UpdateChildSettingsResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	id := plUID(req.ChildId)
	if _, err := s.plOwnedChild(ctx, p, id); err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	in := *req.Body
	var bad []httpapi.FieldError
	if m := in.Music; m != nil && (m.Volume < 0 || m.Volume > 100) {
		bad = append(bad, plField("music.volume", "0 to 100"))
	}
	child := p.Role == auth.RoleChild // a child edit is merged below; its other fields are ignored, so not validated
	if !child && (in.DailyGoalChapters < 1 || in.DailyGoalChapters > 5) {
		bad = append(bad, plField("daily_goal_chapters", "1 to 5"))
	}
	if l := in.DailyTimeLimitMinutes; !child && l != nil && (*l < 5 || *l > 180) {
		bad = append(bad, plField("daily_time_limit_minutes", "null or 5 to 180"))
	}
	known := map[string]bool{}
	for _, w := range plWorldIDs {
		known[w] = true
	}
	seen := map[api.WorldId]bool{}
	for _, w := range in.EnabledWorlds {
		if child {
			break
		}
		if !known[string(w)] || seen[w] {
			bad = append(bad, plField("enabled_worlds", "unknown or repeated world"))
			break
		}
		seen[w] = true
	}
	if !child && len(in.EnabledWorlds) == 0 {
		bad = append(bad, plField("enabled_worlds", "at least one world"))
	}
	if len(bad) > 0 {
		return nil, httpapi.Validation(bad...)
	}
	// the stored settings: what a child edit is merged into, and what an omitted music or voice block keeps
	stored := s.plDefaultSettings(ctx)
	var prev string
	if e := s.Store.R.QueryRowContext(ctx, `SELECT json FROM child_settings WHERE child_id = ?`, id).Scan(&prev); e == nil {
		_ = json.Unmarshal([]byte(prev), &stored)
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, s.plInternalErr(ctx, "load settings", e)
	}
	fillSettings(&stored)
	if child {
		in = mergeChildSettings(stored, in)
	} else {
		if in.Music == nil {
			in.Music = stored.Music
		}
		if in.Voice == nil {
			in.Voice = stored.Voice
		}
		fillSettings(&in)
	}
	if in.KidCanChangeLanguage == nil {
		in.KidCanChangeLanguage = plPtr(true) // default true
	}
	raw, _ := json.Marshal(in)
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`INSERT INTO child_settings (child_id, json, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(child_id) DO UPDATE SET json = excluded.json, updated_at = excluded.updated_at`, id, string(raw), plTS(time.Now()))
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "save settings", err)
	}
	return api.UpdateChildSettings200JSONResponse(in), nil
}

// ───────────── Export (Law 25 access and portability) ─────────────

// plQueryMaps runs a query and returns every row as a map keyed by column name. Columns named json or bkt
// hold JSON text and are embedded as JSON, not as an escaped string. The PIN hash is never selected.
func (s *Server) plQueryMaps(ctx context.Context, q string, args ...any) ([]map[string]any, error) {
	rows, err := s.Store.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			if str, ok := vals[i].(string); ok && (c == "json" || c == "bkt") && json.Valid([]byte(str)) {
				vals[i] = json.RawMessage(str)
			}
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ExportChildData returns everything held about a child (guide 22.3).
func (s *Server) ExportChildData(ctx context.Context, req api.ExportChildDataRequestObject) (api.ExportChildDataResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	id := plUID(req.ChildId)
	c, err := s.plOwnedChild(ctx, p, id)
	if err != nil {
		return nil, err
	}
	// Each entry: output key, SQL. All take the child id as their only argument.
	parts := []struct{ key, sql string }{
		{"consent", `SELECT notice_version, accepted_at, revoked_at FROM consents WHERE child_id = ? ORDER BY accepted_at`},
		{"sessions", `SELECT id, world_id, status, json, started_at, paused_at, finished_at FROM sessions WHERE child_id = ? ORDER BY started_at`},
		{"beats", `SELECT b.id, b.session_id, b.idx, b.json, b.created_at FROM beats b JOIN sessions s ON s.id = b.session_id WHERE s.child_id = ? ORDER BY b.session_id, b.idx`},
		{"answers", `SELECT a.beat_id, a.session_id, a.outcome, a.json, a.created_at FROM answers a JOIN sessions s ON s.id = a.session_id WHERE s.child_id = ? ORDER BY a.created_at`},
		{"progress", `SELECT bkt AS json, updated_at FROM child_profiles WHERE child_id = ?`},
		{"cards", `SELECT card_id, json, acquired_at FROM collection WHERE child_id = ? ORDER BY acquired_at`},
		{"reactions", `SELECT feed_item_id, kind, created_at FROM reactions WHERE child_id = ?`},
		{"challenges", `SELECT id, status, json, created_at FROM challenges WHERE child_id = ?`},
		{"gifts", `SELECT status, json, claimed_at FROM gifts WHERE child_id = ?`},
	}
	doc := map[string]any{}
	for _, part := range parts {
		rows, err := s.plQueryMaps(ctx, part.sql, id)
		if err != nil {
			return nil, s.plInternalErr(ctx, "export "+part.key, err)
		}
		doc[part.key] = rows
	}
	// Nest beats (with their answers) under their session, the shape of the guide.
	answers := map[string][]map[string]any{}
	for _, a := range doc["answers"].([]map[string]any) {
		answers[a["beat_id"].(string)] = append(answers[a["beat_id"].(string)], a)
	}
	beats := map[string][]map[string]any{}
	for _, b := range doc["beats"].([]map[string]any) {
		b["answers"] = answers[b["id"].(string)]
		beats[b["session_id"].(string)] = append(beats[b["session_id"].(string)], b)
	}
	for _, sess := range doc["sessions"].([]map[string]any) {
		sess["beats"] = beats[sess["id"].(string)]
	}
	delete(doc, "beats")
	delete(doc, "answers")
	settings := s.plDefaultSettings(ctx)
	var raw string
	if s.Store.R.QueryRowContext(ctx, `SELECT json FROM child_settings WHERE child_id = ?`, id).Scan(&raw) == nil {
		_ = json.Unmarshal([]byte(raw), &settings)
	}
	child := c.toAPI()
	doc["exported_at"] = plTS(time.Now())
	doc["format_version"] = 1
	doc["child"] = child // the PIN hash is not part of api.Child
	doc["settings"] = settings
	// shortcut: no audit table yet (admin API owns it, BE-27); the access is logged without personal data.
	s.Log.Info("audit child.export", "request_id", httpapi.RequestID(ctx), "parent_id", p.ParentID, "child_id", id)
	return api.ExportChildData200JSONResponse(doc), nil
}
