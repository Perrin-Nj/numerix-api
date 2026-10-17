package adminapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/google/uuid"
)

const day = 24 * time.Hour

// planIDs are the only plans a subscription can have.
var planIDs = []string{"free", "family_monthly", "family_yearly"}

var localeRe = regexp.MustCompile(`^[a-z]{2}-[A-Z]{2}$`)

// subJSON is the effective subscription the admin app shows.
type subJSON struct {
	Status        string `json:"status"` // trial, active, unpaid, expired, canceled
	Plan          string `json:"plan"`
	Method        string `json:"method,omitempty"`
	RenewsAt      string `json:"renews_at,omitempty"`
	TrialEndsAt   string `json:"trial_ends_at,omitempty"`
	MarkedByAdmin bool   `json:"marked_by_admin,omitempty"`
	CanPlay       bool   `json:"can_play"`
	DaysLeft      *int   `json:"days_left"`
}

// subRow is the raw stored subscription (latest row of a parent).
type subRow struct {
	ID, Plan, Status, TrialEnds, PeriodEnd, Canceled string
	Extra                                            map[string]any // the json column
}

// parentJSON is one parent in lists and details.
type parentJSON struct {
	ID            string       `json:"id"`
	Email         string       `json:"email"`
	Name          string       `json:"name"`
	CreatedAt     string       `json:"created_at"`
	Locale        string       `json:"locale"`
	Status        string       `json:"status"` // active or blocked
	BlockedReason string       `json:"blocked_reason,omitempty"`
	Subscription  subJSON      `json:"subscription"`
	ChildrenCount int          `json:"children_count"`
	Children      []childBrief `json:"children,omitempty"` // detail only
}

type childBrief struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Grade       int    `json:"grade"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
}

// effective computes the status the platform really applies (a trial or paid period can have ended).
func effective(s subRow, now time.Time) subJSON {
	out := subJSON{Status: s.Status, Plan: s.Plan}
	if out.Plan == "" {
		out.Plan = "free"
	}
	if s.ID == "" { // parent without any subscription row
		out.Status = "expired"
	}
	if m, ok := s.Extra["method"].(string); ok {
		out.Method = m
	}
	out.MarkedByAdmin, _ = s.Extra["marked_by_admin"].(bool)
	end := ""
	switch s.Status {
	case "trial":
		end, out.TrialEndsAt = s.TrialEnds, s.TrialEnds
		if out.CanPlay = after(end, now); !out.CanPlay {
			out.Status = "expired"
		}
	case "active":
		end, out.RenewsAt = s.PeriodEnd, s.PeriodEnd
		if out.CanPlay = end == "" || after(end, now); !out.CanPlay {
			out.Status = "expired"
		}
	case "canceled":
		end, out.RenewsAt = s.PeriodEnd, s.PeriodEnd
		out.CanPlay = after(end, now)
	case "unpaid":
		out.RenewsAt = s.PeriodEnd
	}
	if t, err := time.Parse(time.RFC3339, end); err == nil {
		d := int((t.Sub(now) + day - 1) / day) // ceil, like the mock
		out.DaysLeft = &d
	}
	return out
}

// after reports whether the ISO timestamp ts is later than now (bad or empty = false).
func after(ts string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, ts)
	return err == nil && t.After(now)
}

// querier is satisfied by *sql.DB and *sql.Tx so helpers work in and out of a transaction.
type querier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// loadSubs returns the latest subscription per parent (parentID "" = all parents).
func loadSubs(ctx context.Context, q querier, parentID string) (map[string]subRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.parent_id, s.id, COALESCE(s.plan_id,'free'), s.status, COALESCE(s.trial_ends_at,''),
		COALESCE(s.current_period_end,''), COALESCE(s.canceled_at,''), s.json
		FROM subscriptions s WHERE s.rowid = (SELECT MAX(rowid) FROM subscriptions WHERE parent_id = s.parent_id)
		AND (? = '' OR s.parent_id = ?)`, parentID, parentID)
	if err != nil {
		return nil, fmt.Errorf("load subscriptions: %w", err)
	}
	defer rows.Close()
	out := map[string]subRow{}
	for rows.Next() {
		var pid, raw string
		var s subRow
		if err := rows.Scan(&pid, &s.ID, &s.Plan, &s.Status, &s.TrialEnds, &s.PeriodEnd, &s.Canceled, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &s.Extra) // bad json = no extras
		out[pid] = s
	}
	return out, rows.Err()
}

// parentFilter are the list filters (SQL ones first, subscription ones applied in Go on the effective status).
type parentFilter struct {
	Q, Status, Locale string
	Sub, Plan         []string
	IDs               []string
}

func has(list []string, v string) bool {
	if list == nil {
		return true
	}
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// findParents loads parents (newest first) with effective subscription and child count.
// shortcut: filters subscription/plan in Go after loading all parents, fine to ~50k parents, then move to SQL.
func (a *API) findParents(ctx context.Context, f parentFilter) ([]parentJSON, error) {
	subs, err := loadSubs(ctx, a.db.R, "")
	if err != nil {
		return nil, err
	}
	rows, err := a.db.R.QueryContext(ctx, `SELECT p.id, p.email, COALESCE(p.name,''), p.locale, p.created_at,
		p.blocked_at IS NOT NULL, COALESCE(p.blocked_reason,''),
		(SELECT COUNT(*) FROM children c WHERE c.parent_id = p.id AND c.deleted_at IS NULL)
		FROM parents p WHERE p.deleted_at IS NULL
		AND (? = '' OR p.locale = ?)
		AND (? = '' OR p.name LIKE '%'||?||'%' OR p.email LIKE '%'||?||'%')
		ORDER BY p.created_at DESC`, f.Locale, f.Locale, f.Q, f.Q, f.Q)
	if err != nil {
		return nil, fmt.Errorf("list parents: %w", err)
	}
	defer rows.Close()
	out := []parentJSON{}
	now := a.now()
	for rows.Next() {
		var p parentJSON
		var blocked bool
		if err := rows.Scan(&p.ID, &p.Email, &p.Name, &p.Locale, &p.CreatedAt, &blocked, &p.BlockedReason, &p.ChildrenCount); err != nil {
			return nil, err
		}
		p.Status = "active"
		if blocked {
			p.Status = "blocked"
		}
		p.Subscription = effective(subs[p.ID], now)
		if (f.Status == "" || f.Status == p.Status) && has(f.Sub, p.Subscription.Status) && has(f.Plan, p.Subscription.Plan) &&
			(f.IDs == nil || has(f.IDs, p.ID)) {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// oneOf turns a query value into a filter list (nil = no filter).
func oneOf(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}

// GET /admin/v1/parents
func (a *API) listParents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := a.findParents(r.Context(), parentFilter{Q: q.Get("q"), Status: q.Get("status"), Sub: oneOf(q.Get("subscription")), Plan: oneOf(q.Get("plan"))})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, paged(list, r))
}

// parentByID loads one parent with its children list.
func (a *API) parentByID(ctx context.Context, id string) (parentJSON, error) {
	list, err := a.findParents(ctx, parentFilter{IDs: []string{id}})
	if err != nil {
		return parentJSON{}, err
	}
	if len(list) == 0 {
		return parentJSON{}, httpapi.ErrNotFound
	}
	p := list[0]
	rows, err := a.db.R.QueryContext(ctx, `SELECT c.id, c.display_name, COALESCE(c.grade,0),
		COALESCE((SELECT MAX(started_at) FROM sessions s WHERE s.child_id = c.id),'')
		FROM children c WHERE c.parent_id = ? AND c.deleted_at IS NULL ORDER BY c.created_at`, id)
	if err != nil {
		return p, fmt.Errorf("list children of parent: %w", err)
	}
	defer rows.Close()
	p.Children = []childBrief{}
	for rows.Next() {
		var c childBrief
		if err := rows.Scan(&c.ID, &c.DisplayName, &c.Grade, &c.LastSeenAt); err != nil {
			return p, err
		}
		p.Children = append(p.Children, c)
	}
	return p, rows.Err()
}

// GET /admin/v1/parents/{id}
func (a *API) getParent(w http.ResponseWriter, r *http.Request) {
	p, err := a.parentByID(r.Context(), r.PathValue("id"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// DELETE /admin/v1/parents/{id}: hard delete, cascades to children (consents stay: Law 25 evidence).
func (a *API) deleteParent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		var email string
		if err := tx.QueryRow(`SELECT email FROM parents WHERE id = ?`, id).Scan(&email); errors.Is(err, sql.ErrNoRows) {
			return httpapi.ErrNotFound
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM parents WHERE id = ?`, id); err != nil {
			return err
		}
		return a.writeAudit(tx, actor(r), "parent.delete", "parent", id, email)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// patchBody is PATCH /parents/{id}; pointers tell "absent" from "empty".
type patchBody struct {
	Name          *string `json:"name"`
	Locale        *string `json:"locale"`
	Status        *string `json:"status"`
	BlockedReason *string `json:"blocked_reason"`
	Subscription  *struct {
		Status     *string `json:"status"`
		Plan       *string `json:"plan"`
		ExtendDays *int    `json:"extend_days"`
	} `json:"subscription"`
}

// validate checks the body like the mock does.
func (b patchBody) validate() error {
	if b.Name != nil && (strings.TrimSpace(*b.Name) == "" || len(*b.Name) > 80) {
		return bad("name must be 1 to 80 characters")
	}
	if b.Locale != nil && !localeRe.MatchString(*b.Locale) {
		return bad("locale must look like fr-CA")
	}
	if b.Status != nil && *b.Status != "active" && *b.Status != "blocked" {
		return bad("status must be active or blocked")
	}
	if s := b.Subscription; s != nil {
		if s.Status != nil && !has([]string{"active", "paid", "unpaid"}, *s.Status) {
			return bad("subscription.status must be active (paid) or unpaid")
		}
		if s.Plan != nil && !has(planIDs, *s.Plan) {
			return bad("unknown plan")
		}
		if s.ExtendDays != nil && (*s.ExtendDays < 1 || *s.ExtendDays > 3650) {
			return bad("extend_days must be 1 to 3650")
		}
	}
	return nil
}

// PATCH /admin/v1/parents/{id}: name, locale, block/unblock, subscription override.
func (a *API) patchParent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var b patchBody
	if err := decode(r, &b); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := b.validate(); err != nil {
		a.fail(w, r, err)
		return
	}
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM parents WHERE id = ? AND deleted_at IS NULL`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return httpapi.ErrNotFound
		}
		var changes []string
		if b.Name != nil {
			if _, err := tx.Exec(`UPDATE parents SET name = ? WHERE id = ?`, strings.TrimSpace(*b.Name), id); err != nil {
				return err
			}
			changes = append(changes, "name")
		}
		if b.Locale != nil {
			if _, err := tx.Exec(`UPDATE parents SET locale = ? WHERE id = ?`, *b.Locale, id); err != nil {
				return err
			}
			changes = append(changes, "locale")
		}
		action := "parent.update"
		if b.Status != nil && *b.Status == "blocked" {
			reason := "Blocked by admin"
			if b.BlockedReason != nil && *b.BlockedReason != "" {
				reason = (*b.BlockedReason)[:min(200, len(*b.BlockedReason))]
			}
			if _, err := tx.Exec(`UPDATE parents SET blocked_at = ?, blocked_reason = ? WHERE id = ?`, a.stamp(), reason, id); err != nil {
				return err
			}
			action, changes = "parent.block", append(changes, "block: "+reason)
		} else if b.Status != nil {
			if _, err := tx.Exec(`UPDATE parents SET blocked_at = NULL, blocked_reason = NULL WHERE id = ?`, id); err != nil {
				return err
			}
			action, changes = "parent.unblock", append(changes, "unblock")
		}
		if b.Subscription != nil {
			c, err := a.overrideSub(r.Context(), tx, id, b)
			if err != nil {
				return err
			}
			changes = append(changes, c...)
		}
		return a.writeAudit(tx, actor(r), action, "parent", id, strings.Join(changes, ", "))
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p, err := a.parentByID(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// overrideSub applies the admin subscription override (plan, paid/unpaid, extend days) and returns the change notes.
func (a *API) overrideSub(ctx context.Context, tx *sql.Tx, parentID string, b patchBody) ([]string, error) {
	subs, err := loadSubs(ctx, tx, parentID)
	if err != nil {
		return nil, err
	}
	s, exists := subs[parentID]
	if !exists { // first row for this parent: starts expired, the override below decides
		s = subRow{ID: uuid.NewString(), Plan: "free", Status: "expired", Extra: map[string]any{}}
	}
	if s.Extra == nil {
		s.Extra = map[string]any{}
	}
	now, sb := a.now().UTC(), b.Subscription
	var notes []string
	if sb.Plan != nil {
		s.Plan = *sb.Plan
		notes = append(notes, "plan="+s.Plan)
	}
	if sb.Status != nil && *sb.Status == "unpaid" {
		s.Status, s.Extra["marked_by_admin"] = "unpaid", true
		notes = append(notes, "unpaid")
	}
	if sb.Status != nil && *sb.Status != "unpaid" { // active or paid
		if s.Plan == "free" {
			s.Plan = "family_monthly"
		}
		if !(s.Status == "active" && after(s.PeriodEnd, now)) { // start a new paid period
			days := 30 * day
			if s.Plan == "family_yearly" {
				days = 365 * day
			}
			s.PeriodEnd = now.Add(days).Format(tsLayout)
		}
		s.Status, s.Extra["marked_by_admin"] = "active", true
		notes = append(notes, "paid")
	}
	if sb.ExtendDays != nil {
		field := &s.PeriodEnd
		if s.Status == "trial" {
			field = &s.TrialEnds
		}
		base := now
		if t, err := time.Parse(time.RFC3339, *field); err == nil && t.After(now) {
			base = t
		}
		*field = base.Add(time.Duration(*sb.ExtendDays) * day).Format(tsLayout)
		notes = append(notes, fmt.Sprintf("extend %dd", *sb.ExtendDays))
	}
	extra, _ := json.Marshal(s.Extra)
	if !exists {
		_, err = tx.Exec(`INSERT INTO subscriptions (id, parent_id, plan_id, status, trial_ends_at, current_period_end, json) VALUES (?,?,?,?,NULLIF(?,''),NULLIF(?,''),?)`,
			s.ID, parentID, s.Plan, s.Status, s.TrialEnds, s.PeriodEnd, string(extra))
	} else {
		_, err = tx.Exec(`UPDATE subscriptions SET plan_id = ?, status = ?, trial_ends_at = NULLIF(?,''), current_period_end = NULLIF(?,''), json = ?, updated_at = ? WHERE id = ?`,
			s.Plan, s.Status, s.TrialEnds, s.PeriodEnd, string(extra), a.stamp(), s.ID)
	}
	return notes, err
}

// sortedKeys is a tiny helper for stable audit details.
func sortedKeys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}
