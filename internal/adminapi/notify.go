package adminapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// strList accepts "a" or ["a","b"] in JSON (filters in the broadcast body).
type strList []string

func (s *strList) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*s = strList{one}
		return nil
	}
	return json.Unmarshal(b, (*[]string)(s))
}

// biText accepts "text" or {"fr": "...", "en": "..."}.
type biText struct{ FR, EN string }

func (t *biText) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		t.FR, t.EN = one, one
		return nil
	}
	var m struct {
		FR string `json:"fr"`
		EN string `json:"en"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	t.FR, t.EN = m.FR, m.EN
	if t.FR == "" {
		t.FR = t.EN
	}
	if t.EN == "" {
		t.EN = t.FR
	}
	return nil
}

// pick returns the text for a parent's locale (en* = English, else French).
func (t biText) pick(locale string) string {
	if strings.HasPrefix(locale, "en") {
		return t.EN
	}
	return t.FR
}

// POST /admin/v1/notifications/broadcast
func (a *API) broadcast(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Audience string `json:"audience"`
		Title    biText `json:"title"`
		Body     biText `json:"body"`
		Filter   struct {
			Subscription strList `json:"subscription"`
			Status       strList `json:"status"`
			Locale       strList `json:"locale"`
			Plan         strList `json:"plan"`
			ParentIDs    strList `json:"parent_ids"`
		} `json:"filter"`
	}
	if err := decode(r, &b); err != nil {
		a.fail(w, r, err)
		return
	}
	if b.Audience == "" {
		b.Audience = "parent"
	}
	if b.Audience != "parent" && b.Audience != "child" {
		a.fail(w, r, bad("audience must be parent or child"))
		return
	}
	if b.Title.FR == "" || b.Body.FR == "" {
		a.fail(w, r, bad("title and body are required (string or {fr,en})"))
		return
	}
	f := b.Filter
	all, err := a.findParents(r.Context(), parentFilter{Sub: f.Subscription, Plan: f.Plan, IDs: f.ParentIDs})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	bid, at := uuid.NewString(), a.stamp()
	var targets []parentJSON
	for _, p := range all { // status and locale filters are applied here
		if has(f.Status, p.Status) && has(f.Locale, p.Locale) {
			targets = append(targets, p)
		}
	}
	err = a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		for _, p := range targets {
			js, _ := json.Marshal(map[string]string{"title": b.Title.pick(p.Locale), "body": b.Body.pick(p.Locale), "broadcast_id": bid})
			if _, err := tx.Exec(`INSERT INTO notifications (id, parent_id, audience, kind, json, created_at) VALUES (?,?,?,'announcement',?,?)`,
				uuid.NewString(), p.ID, b.Audience, string(js), at); err != nil {
				return err
			}
		}
		return a.writeAudit(tx, actor(r), "notification.broadcast", "notification", bid, fmt.Sprintf("%s to %d: %s", b.Audience, len(targets), b.Title.FR))
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"broadcast_id": bid, "recipients": len(targets)})
}

// GET /admin/v1/notifications: one row per broadcast (or per single notification), newest first.
func (a *API) listBroadcasts(w http.ResponseWriter, r *http.Request) {
	pg, size, off := page(r)
	const grouped = `FROM notifications GROUP BY COALESCE(json_extract(json,'$.broadcast_id'), id)`
	var total int
	if err := a.db.R.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM (SELECT 1 `+grouped+`)`).Scan(&total); err != nil {
		a.fail(w, r, err)
		return
	}
	rows, err := a.db.R.QueryContext(r.Context(), `SELECT COALESCE(json_extract(json,'$.broadcast_id'), id), MAX(kind),
		COALESCE(MAX(json_extract(json,'$.title')),''), COALESCE(MAX(json_extract(json,'$.body')),''), MIN(created_at), MAX(audience), COUNT(*),
		MAX(json_extract(json,'$.broadcast_id') IS NOT NULL), MAX(parent_id) `+grouped+` ORDER BY MIN(created_at) DESC LIMIT ? OFFSET ?`, size, off)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer rows.Close()
	type row struct {
		ID         string  `json:"id"`
		Kind       string  `json:"kind"`
		Title      string  `json:"title"`
		Body       string  `json:"body"`
		CreatedAt  string  `json:"created_at"`
		Audience   string  `json:"audience"`
		Recipients int     `json:"recipients"`
		Broadcast  bool    `json:"broadcast"`
		ParentID   *string `json:"parent_id"`
	}
	items := []row{}
	for rows.Next() {
		var x row
		var pid string
		if err := rows.Scan(&x.ID, &x.Kind, &x.Title, &x.Body, &x.CreatedAt, &x.Audience, &x.Recipients, &x.Broadcast, &pid); err != nil {
			a.fail(w, r, err)
			return
		}
		if !x.Broadcast {
			x.ParentID = &pid
		}
		items = append(items, x)
	}
	writeJSON(w, http.StatusOK, pageOf[row]{Items: items, Page: pg, PageSize: size, Total: total})
}

// GET /admin/v1/audit: newest first.
func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	pg, size, off := page(r)
	var total int
	if err := a.db.R.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM admin_audit`).Scan(&total); err != nil {
		a.fail(w, r, err)
		return
	}
	rows, err := a.db.R.QueryContext(r.Context(), `SELECT id, at, actor, action, target_type, target_id, detail FROM admin_audit ORDER BY at DESC, rowid DESC LIMIT ? OFFSET ?`, size, off)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer rows.Close()
	type row struct {
		ID         string `json:"id"`
		At         string `json:"at"`
		Actor      string `json:"actor"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		Detail     string `json:"detail"`
	}
	items := []row{}
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ID, &x.At, &x.Actor, &x.Action, &x.TargetType, &x.TargetID, &x.Detail); err != nil {
			a.fail(w, r, err)
			return
		}
		items = append(items, x)
	}
	writeJSON(w, http.StatusOK, pageOf[row]{Items: items, Page: pg, PageSize: size, Total: total})
}

// GET /admin/v1/health: database check and row counts (503 when the database does not answer).
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	out := map[string]any{"status": "ok", "db": "ok", "time": a.stamp()}
	if err := a.db.Ping(ctx); err != nil {
		a.log.Error("admin health: db down", "err", err)
		out["status"], out["db"] = "degraded", "down"
		writeJSON(w, http.StatusServiceUnavailable, out)
		return
	}
	counts := map[string]int{}
	for _, t := range []string{"parents", "children", "sessions", "notifications", "admin_audit"} { // fixed list: safe to concatenate
		var n int
		if err := a.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t).Scan(&n); err == nil {
			counts[t] = n
		}
	}
	out["counts"] = counts
	out["ai"] = a.drafter != nil // the admin app enables the "draft with AI" button from this flag
	if a.sync != nil {           // Mongo copy counters (pending, last success, last error); token-protected on purpose
		out["mongo_sync"] = a.sync()
	}
	writeJSON(w, http.StatusOK, out)
}
