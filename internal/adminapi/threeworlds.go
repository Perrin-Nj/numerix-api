package adminapi

// Routes of the three-worlds release: the world status switch and the parent free access. Both are reachable only
// with the admin bearer token (the guard in admin.go runs first) and both write an audit row in the same transaction.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// PUT /admin/v1/worlds/{id}/status {"status":"published"|"draft"}: opens or closes a world for children and parents.
func (a *API) putWorldStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if in.Status != "published" && in.Status != "draft" {
		a.fail(w, r, bad("status must be published or draft"))
		return
	}
	if in.Status == "draft" { // a world new children start in cannot be closed behind their back
		def := appconfig.Get[appconfig.Platform](r.Context(), a.cfg, appconfig.NSPlatform)
		if slices.Contains(def.DefaultWorlds, id) || slices.Contains(def.ReadyWorlds, id) {
			a.fail(w, r, bad("remove "+id+" from platform default_worlds and ready_worlds before closing it"))
			return
		}
	}
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		var old string
		if e := tx.QueryRow(`SELECT status FROM worlds WHERE id = ? AND status <> 'retired'`, id).Scan(&old); errors.Is(e, sql.ErrNoRows) {
			return httpapi.ErrNotFound
		} else if e != nil {
			return e
		}
		if old == in.Status {
			return nil
		}
		if _, e := tx.Exec(`UPDATE worlds SET status = ?, updated_at = ? WHERE id = ?`, in.Status, a.stamp(), id); e != nil {
			return e
		}
		return a.writeAudit(tx, actor(r), "world.status", "world", id, old+" -> "+in.Status)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.content.AfterCommit() // the cached world lists are stale
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": in.Status})
}

// freeAccess is the grant kept in subscriptions.json under "free_access".
type freeAccess struct {
	GrantedBy string  `json:"granted_by"`
	GrantedAt string  `json:"granted_at"`
	Until     *string `json:"until"` // RFC 3339 or null for no end date
	Reason    string  `json:"reason"`
}

// PUT /admin/v1/parents/{id}/free-access {"until": date|null, "reason": "..."}: grant or extend free access.
func (a *API) putFreeAccess(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Until  *string `json:"until"`
		Reason *string `json:"reason"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		a.fail(w, r, bad("body must be {until, reason}"))
		return
	}
	if in.Reason == nil || strings.TrimSpace(*in.Reason) == "" || len([]rune(*in.Reason)) > 200 {
		a.fail(w, r, bad("reason is required, 1 to 200 characters"))
		return
	}
	if in.Until != nil {
		t, err := time.Parse(time.RFC3339, *in.Until)
		if err != nil || !t.After(a.now()) {
			a.fail(w, r, bad("until must be a future RFC 3339 date, or null for no end date"))
			return
		}
	}
	id := r.PathValue("id")
	a.changeFreeAccess(w, r, id, func(old *freeAccess) (*freeAccess, string) {
		fa := &freeAccess{GrantedBy: actor(r), GrantedAt: a.stamp(), Until: in.Until, Reason: strings.TrimSpace(*in.Reason)}
		return fa, describe(old) + " -> " + describe(fa)
	})
}

// DELETE /admin/v1/parents/{id}/free-access: end free access now.
func (a *API) deleteFreeAccess(w http.ResponseWriter, r *http.Request) {
	a.changeFreeAccess(w, r, r.PathValue("id"), func(old *freeAccess) (*freeAccess, string) {
		return nil, describe(old) + " -> none"
	})
}

func describe(f *freeAccess) string {
	switch {
	case f == nil:
		return "none"
	case f.Until == nil:
		return "open ended"
	}
	return "until " + *f.Until
}

// changeFreeAccess reads the subscription JSON of a parent, applies change and writes it back with an audit row.
func (a *API) changeFreeAccess(w http.ResponseWriter, r *http.Request, parentID string, change func(old *freeAccess) (*freeAccess, string)) {
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		var n int
		if e := tx.QueryRow(`SELECT COUNT(*) FROM parents WHERE id = ?`, parentID).Scan(&n); e != nil {
			return e
		} else if n == 0 {
			return httpapi.ErrNotFound
		}
		var subID, raw string
		e := tx.QueryRow(`SELECT id, json FROM subscriptions WHERE parent_id = ? ORDER BY rowid DESC LIMIT 1`, parentID).Scan(&subID, &raw)
		if errors.Is(e, sql.ErrNoRows) { // admin-created parent with no row yet: start one without trial dates
			subID, raw = parentID+"-sub", "{}"
			if _, e = tx.Exec(`INSERT INTO subscriptions (id, parent_id, plan_id, status) VALUES (?, ?, 'free', 'expired')`, subID, parentID); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		doc := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(raw), &doc)
		var old *freeAccess
		if b, ok := doc["free_access"]; ok {
			old = &freeAccess{}
			if json.Unmarshal(b, old) != nil {
				old = nil
			}
		}
		next, note := change(old)
		if next == nil {
			delete(doc, "free_access")
		} else {
			doc["free_access"], _ = json.Marshal(next)
		}
		out, _ := json.Marshal(doc)
		if _, e := tx.Exec(`UPDATE subscriptions SET json = ?, updated_at = ? WHERE id = ?`, string(out), a.stamp(), subID); e != nil {
			return e
		}
		return a.writeAudit(tx, actor(r), "parent.free_access", "parent", parentID, note)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p, err := a.parentByID(r.Context(), parentID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
