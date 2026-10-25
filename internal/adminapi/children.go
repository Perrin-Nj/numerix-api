package adminapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// worldIDs are the learning worlds a child can enable.
var worldIDs = []string{"nature", "espace", "cuisine", "sport", "construction", "medecine", "science", "echecs"}

// childJSON is one child as the admin app shows it.
type childJSON struct {
	ID             string         `json:"id"`
	ParentID       string         `json:"parent_id"`
	ParentName     *string        `json:"parent_name,omitempty"`
	Parent         *parentBrief   `json:"parent,omitempty"` // detail only
	DisplayName    string         `json:"display_name"`
	Grade          int            `json:"grade"`
	AvatarID       string         `json:"avatar_id,omitempty"`
	CreatedAt      string         `json:"created_at"`
	LastSeenAt     string         `json:"last_seen_at,omitempty"`
	MinutesToday   int            `json:"minutes_today"`
	SessionsTotal  int            `json:"sessions_total"`
	CardsTotal     int            `json:"cards_total"`
	SkillsMastered int            `json:"skills_mastered"` // 0 until the BKT profile format is read here (see docs/ADMIN_API.md)
	Settings       map[string]any `json:"settings"`
}

type parentBrief struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// settingRules validate each child setting (same rules as the mock).
var settingRules = map[string]func(any) bool{
	"daily_goal_chapters":      func(v any) bool { return isInt(v, 1, 5) },
	"daily_time_limit_minutes": func(v any) bool { return v == nil || isInt(v, 5, 180) },
	"enabled_worlds": func(v any) bool {
		l, ok := v.([]any)
		if !ok || len(l) == 0 {
			return false
		}
		for _, w := range l {
			if s, _ := w.(string); !slices.Contains(worldIDs, s) {
				return false
			}
		}
		return true
	},
	"read_aloud":              isBool,
	"sound":                   isBool,
	"kid_can_change_language": isBool,
}

func isBool(v any) bool { _, ok := v.(bool); return ok }

// isInt reports whether v is a JSON number holding an integer in [lo, hi] (JSON numbers decode as float64).
func isInt(v any, lo, hi float64) bool {
	f, ok := v.(float64)
	return ok && f == float64(int64(f)) && f >= lo && f <= hi
}

// defaultSettings fill gaps in the stored child_settings json.
func defaultSettings() map[string]any {
	return map[string]any{"daily_goal_chapters": 1.0, "daily_time_limit_minutes": nil, "enabled_worlds": []any{"echecs", "medecine", "espace"},
		"read_aloud": true, "sound": true, "kid_can_change_language": false}
}

// childSelect is the shared SELECT; callers append WHERE/ORDER/LIMIT. Stats come from sessions and collection.
const childSelect = `SELECT c.id, c.parent_id, p.name, p.email, c.display_name, COALESCE(c.grade,0), COALESCE(c.avatar_id,''), c.created_at,
	COALESCE((SELECT MAX(started_at) FROM sessions s WHERE s.child_id = c.id),'') AS last_seen,
	COALESCE((SELECT CAST(SUM((julianday(COALESCE(s.finished_at, s.paused_at, s.started_at)) - julianday(s.started_at)) * 1440) AS INTEGER)
		FROM sessions s WHERE s.child_id = c.id AND substr(s.started_at,1,10) = ?),0),
	(SELECT COUNT(*) FROM sessions s WHERE s.child_id = c.id),
	(SELECT COUNT(*) FROM collection k WHERE k.child_id = c.id),
	COALESCE(cs.json,'{}')
	FROM children c JOIN parents p ON p.id = c.parent_id LEFT JOIN child_settings cs ON cs.child_id = c.id
	WHERE c.deleted_at IS NULL AND p.deleted_at IS NULL `

// scanChildren runs a childSelect query and builds the JSON views.
func (a *API) scanChildren(r *http.Request, where string, args []any, tail string) ([]childJSON, error) {
	args = append([]any{a.now().UTC().Format("2006-01-02")}, args...) // first ? is "today"
	rows, err := a.db.R.QueryContext(r.Context(), childSelect+where+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("query children: %w", err)
	}
	defer rows.Close()
	out := []childJSON{}
	for rows.Next() {
		var c childJSON
		var pname sql.NullString
		var pemail, raw string
		if err := rows.Scan(&c.ID, &c.ParentID, &pname, &pemail, &c.DisplayName, &c.Grade, &c.AvatarID, &c.CreatedAt, &c.LastSeenAt,
			&c.MinutesToday, &c.SessionsTotal, &c.CardsTotal, &raw); err != nil {
			return nil, err
		}
		c.Settings = defaultSettings()
		var stored map[string]any
		_ = json.Unmarshal([]byte(raw), &stored)
		for k, v := range stored {
			c.Settings[k] = v
		}
		c.ParentName = &pname.String
		c.Parent = &parentBrief{ID: c.ParentID, Name: pname.String, Email: pemail}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GET /admin/v1/children?parent=&q=  (audited: lists child data)
func (a *API) listChildren(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := `AND (? = '' OR c.parent_id = ?) AND (? = '' OR c.display_name LIKE '%'||?||'%') `, []any{q.Get("parent"), q.Get("parent"), q.Get("q"), q.Get("q")}
	pg, size, off := page(r)
	var total int
	if err := a.db.R.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM children c WHERE c.deleted_at IS NULL `+where, args...).Scan(&total); err != nil {
		a.fail(w, r, err)
		return
	}
	items, err := a.scanChildren(r, where, append(args, size, off), `ORDER BY last_seen DESC, c.created_at DESC LIMIT ? OFFSET ?`)
	if err == nil {
		err = a.auditRead(r.Context(), r, "children.list", "child", "*", fmt.Sprintf("parent=%s q=%s", q.Get("parent"), q.Get("q")))
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for i := range items {
		items[i].Parent = nil // the list carries parent_name only
	}
	writeJSON(w, http.StatusOK, pageOf[childJSON]{Items: items, Page: pg, PageSize: size, Total: total})
}

// childByID loads one child or 404.
func (a *API) childByID(r *http.Request, id string) (childJSON, error) {
	list, err := a.scanChildren(r, `AND c.id = ? `, []any{id}, "")
	if err != nil {
		return childJSON{}, err
	}
	if len(list) == 0 {
		return childJSON{}, httpapi.ErrNotFound
	}
	return list[0], nil
}

// GET /admin/v1/children/{id}  (audited)
func (a *API) getChild(w http.ResponseWriter, r *http.Request) {
	c, err := a.childByID(r, r.PathValue("id"))
	if err == nil {
		err = a.auditRead(r.Context(), r, "child.read", "child", c.ID, "")
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// PATCH /admin/v1/children/{id}: display_name|name, grade, avatar_id, settings{...}, or the two flat setting shortcuts.
func (a *API) patchChild(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b := map[string]any{}
	if err := decode(r, &b); err != nil {
		a.fail(w, r, err)
		return
	}
	settings := map[string]any{}
	if s, ok := b["settings"].(map[string]any); ok {
		for k, v := range s {
			settings[k] = v
		}
	}
	for _, k := range []string{"daily_time_limit_minutes", "kid_can_change_language"} { // flat shortcuts used by the app
		if v, ok := b[k]; ok {
			settings[k] = v
		}
	}
	for k, v := range settings {
		if rule := settingRules[k]; rule == nil || !rule(v) {
			a.fail(w, r, bad("invalid setting "+k))
			return
		}
	}
	name, hasName := b["display_name"]
	if !hasName {
		name, hasName = b["name"]
	}
	if hasName {
		if s, _ := name.(string); strings.TrimSpace(s) == "" || len(s) > 30 {
			a.fail(w, r, bad("name must be 1 to 30 characters"))
			return
		}
	}
	if g, ok := b["grade"]; ok && !isInt(g, 1, 6) {
		a.fail(w, r, bad("grade must be 1 to 6"))
		return
	}
	if v, ok := b["avatar_id"]; ok {
		if _, isStr := v.(string); !isStr {
			a.fail(w, r, bad("avatar_id must be a string"))
			return
		}
	}
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		var raw string
		err := tx.QueryRow(`SELECT COALESCE((SELECT json FROM child_settings WHERE child_id = c.id),'{}') FROM children c WHERE c.id = ? AND c.deleted_at IS NULL`, id).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return httpapi.ErrNotFound
		} else if err != nil {
			return err
		}
		if hasName {
			s, _ := name.(string)
			if _, err := tx.Exec(`UPDATE children SET display_name = ? WHERE id = ?`, strings.TrimSpace(s), id); err != nil {
				return err
			}
		}
		if g, ok := b["grade"]; ok {
			if _, err := tx.Exec(`UPDATE children SET grade = ? WHERE id = ?`, int(g.(float64)), id); err != nil {
				return err
			}
		}
		if v, ok := b["avatar_id"]; ok {
			if _, err := tx.Exec(`UPDATE children SET avatar_id = ? WHERE id = ?`, v, id); err != nil {
				return err
			}
		}
		if len(settings) > 0 { // merge into the stored json
			cur := map[string]any{}
			_ = json.Unmarshal([]byte(raw), &cur)
			for k, v := range settings {
				cur[k] = v
			}
			js, _ := json.Marshal(cur)
			if _, err := tx.Exec(`INSERT INTO child_settings (child_id, json) VALUES (?,?)
				ON CONFLICT(child_id) DO UPDATE SET json = excluded.json, updated_at = ?`, id, string(js), a.stamp()); err != nil {
				return err
			}
		}
		keys := map[string]any{}
		for k := range b {
			keys[k] = true
		}
		for k := range settings {
			keys[k] = true
		}
		return a.writeAudit(tx, actor(r), "child.update", "child", id, strings.Join(sortedKeys(keys), ","))
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	c, err := a.childByID(r, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// GET /admin/v1/children/{id}/activity  (audited)
func (a *API) childActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var exists int
	if err := a.db.R.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM children WHERE id = ? AND deleted_at IS NULL`, id).Scan(&exists); err != nil || exists == 0 {
		if err == nil {
			err = httpapi.ErrNotFound
		}
		a.fail(w, r, err)
		return
	}
	type sess struct {
		ID          string `json:"id"`
		World       string `json:"world"`
		SkillID     string `json:"skill_id"`
		SkillName   string `json:"skill_name"`
		StartedAt   string `json:"started_at"`
		Status      string `json:"status"`
		Correct     int    `json:"correct"`
		Total       int    `json:"total"`
		CardAwarded bool   `json:"card_awarded"`
	}
	type card struct {
		ID       string `json:"id"`
		World    string `json:"world"`
		Name     string `json:"name"`
		EarnedAt string `json:"earned_at"`
	}
	out := struct {
		ChildID  string `json:"child_id"`
		Sessions []sess `json:"sessions"`
		Cards    []card `json:"cards"`
		Skills   []any  `json:"skills"`
	}{ChildID: id, Sessions: []sess{}, Cards: []card{}, Skills: []any{}}

	// session json keys (skill_id, skill_name, correct, total) are written by the game module; missing = empty/0.
	rows, err := a.db.R.QueryContext(r.Context(), `SELECT id, COALESCE(world_id,''), COALESCE(json_extract(json,'$.skill_id'),''), COALESCE(json_extract(json,'$.skill_name'),''),
		started_at, status, COALESCE(json_extract(json,'$.correct'),0), COALESCE(json_extract(json,'$.total'),0)
		FROM sessions WHERE child_id = ? ORDER BY started_at DESC LIMIT 10`, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for rows.Next() {
		var s sess
		if err = rows.Scan(&s.ID, &s.World, &s.SkillID, &s.SkillName, &s.StartedAt, &s.Status, &s.Correct, &s.Total); err != nil {
			break
		}
		s.CardAwarded = s.Total > 0 && float64(s.Correct)/float64(s.Total) >= 0.7
		out.Sessions = append(out.Sessions, s)
	}
	rows.Close()
	if err == nil {
		var crows *sql.Rows
		if crows, err = a.db.R.QueryContext(r.Context(), `SELECT card_id, COALESCE(json_extract(json,'$.world'),''), COALESCE(json_extract(json,'$.name'), card_id), acquired_at
			FROM collection WHERE child_id = ? ORDER BY acquired_at DESC LIMIT 6`, id); err == nil {
			for crows.Next() {
				var c card
				if err = crows.Scan(&c.ID, &c.World, &c.Name, &c.EarnedAt); err != nil {
					break
				}
				out.Cards = append(out.Cards, c)
			}
			crows.Close()
		}
	}
	if err == nil {
		err = a.auditRead(r.Context(), r, "child.activity.read", "child", id, "")
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
