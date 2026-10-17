package app

// Notifications (BE-25), web push subscriptions (BE-26) and product events (BE-53).

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// plNoteCopy is the text of the server-made notifications: kind -> {fr title, fr body, en title, en body}.
var plNoteCopy = map[string][4]string{
	"subscription_expiring": {"Votre abonnement se termine bientôt", "Renouvelez pour que la famille puisse continuer à jouer.", "Your subscription ends soon", "Renew so the family can keep playing."},
	"payment_due":           {"Paiement requis", "Réglez votre abonnement pour que votre enfant puisse rejouer.", "Payment needed", "Settle your subscription so your child can play again."},
}

// plAutoNotes creates the subscription notifications a parent should see (once per kind and period, via json.key).
func (s *Server) plAutoNotes(ctx context.Context, parentID, locale string) error {
	sub, err := s.plLoadSub(ctx, parentID)
	if err != nil {
		return err
	}
	v := plView(sub, time.Now())
	kind, key := "", ""
	switch {
	case v.Status == "unpaid" || v.Status == "expired":
		kind, key = "payment_due", string(v.Status)+":"+sub.RenewsAt.String+sub.TrialEnds.String
	case v.DaysLeft != nil && v.CanPlay && *v.DaysLeft <= s.plConfig(ctx).ExpiringWarningDays:
		kind, key = "subscription_expiring", sub.RenewsAt.String+sub.TrialEnds.String
	default:
		return nil
	}
	txt := plNoteCopy[kind]
	title, body := txt[0], txt[1]
	if locale == "en-CA" {
		title, body = txt[2], txt[3]
	}
	raw, _ := json.Marshal(map[string]string{"title": title, "body": body, "key": kind + ":" + key})
	return s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE parent_id = ? AND json_extract(json, '$.key') = ?`,
			parentID, kind+":"+key).Scan(&n); err != nil || n > 0 {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO notifications (id, parent_id, audience, kind, json) VALUES (?, ?, 'parent', ?, ?)`,
			uuid.NewString(), parentID, kind, string(raw))
		return err
	})
}

// ListNotifications returns the parent's notifications, newest first, with the unread count.
func (s *Server) ListNotifications(ctx context.Context, req api.ListNotificationsRequestObject) (api.ListNotificationsResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	audience := "parent"
	if a := req.Params.Audience; a != nil && string(*a) == "child" {
		audience = "child"
	}
	var locale string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT locale FROM parents WHERE id = ?`, p.ParentID).Scan(&locale); err != nil {
		return nil, s.plInternalErr(ctx, "load parent", err)
	}
	if err := s.plAutoNotes(ctx, p.ParentID, locale); err != nil {
		return nil, s.plInternalErr(ctx, "create notifications", err)
	}
	rows, err := s.Store.R.QueryContext(ctx,
		`SELECT id, kind, json, read_at, created_at FROM notifications WHERE parent_id = ? AND audience = ? ORDER BY created_at DESC LIMIT 200`,
		p.ParentID, audience)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list notifications", err)
	}
	defer rows.Close()
	items, unread := []api.Notification{}, 0
	for rows.Next() {
		var id, kind, raw, created string
		var readAt sql.NullString
		if err := rows.Scan(&id, &kind, &raw, &readAt, &created); err != nil {
			return nil, s.plInternalErr(ctx, "scan notification", err)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		title, _ := m["title"].(string)
		body, _ := m["body"].(string)
		if !readAt.Valid {
			unread++
		}
		items = append(items, api.Notification{
			Id: id, Kind: api.NotificationKind(kind), Audience: api.NotificationAudience(audience),
			Title: title, Body: body, Read: readAt.Valid, CreatedAt: plParseTS(created),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, s.plInternalErr(ctx, "list notifications", err)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return api.ListNotifications200JSONResponse{Items: items, UnreadCount: unread}, nil
}

// MarkNotificationRead marks one notification of the caller as read (404 for anyone else's).
func (s *Server) MarkNotificationRead(ctx context.Context, req api.MarkNotificationReadRequestObject) (api.MarkNotificationReadResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	var found int64
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		res, e := tx.ExecContext(ctx, `UPDATE notifications SET read_at = COALESCE(read_at, ?) WHERE id = ? AND parent_id = ?`,
			plTS(time.Now()), req.NotificationId, p.ParentID)
		if e != nil {
			return e
		}
		found, e = res.RowsAffected()
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "mark notification read", err)
	}
	if found == 0 {
		return nil, httpapi.ErrNotFound
	}
	return api.MarkNotificationRead204Response{}, nil
}

// ───────────── Web push ─────────────

// maxPushPerParent is the limit of guide section 12.
const maxPushPerParent = 10

// SubscribePush stores (upserts by endpoint) a browser push subscription: 201 new, 204 already known.
// shortcut: p256dh and auth are stored as received; encrypt them with a key from config before real sending (guide 12).
func (s *Server) SubscribePush(ctx context.Context, req api.SubscribePushRequestObject) (api.SubscribePushResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	u, perr := url.Parse(b.Endpoint)
	if perr != nil || u.Scheme != "https" || len(b.Endpoint) > 2048 || b.Keys.P256dh == "" || b.Keys.Auth == "" || len(b.Keys.P256dh) > 512 || len(b.Keys.Auth) > 512 {
		return nil, httpapi.Validation(plField("endpoint", "an https endpoint with p256dh and auth keys is required"))
	}
	created := false
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var owner string
		e := tx.QueryRowContext(ctx, `SELECT parent_id FROM push_subscriptions WHERE endpoint = ?`, b.Endpoint).Scan(&owner)
		if e == nil { // known endpoint: refresh keys, and move it to the caller (the browser now belongs to this login)
			_, e = tx.ExecContext(ctx, `UPDATE push_subscriptions SET parent_id = ?, p256dh = ?, auth = ? WHERE endpoint = ?`,
				p.ParentID, b.Keys.P256dh, b.Keys.Auth, b.Endpoint)
			return e
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE parent_id = ?`, p.ParentID).Scan(&n); e != nil {
			return e
		}
		if n >= maxPushPerParent { // drop the oldest so a parent with many browsers keeps working
			if _, e := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = (SELECT id FROM push_subscriptions WHERE parent_id = ? ORDER BY created_at LIMIT 1)`, p.ParentID); e != nil {
				return e
			}
		}
		created = true
		_, e = tx.ExecContext(ctx, `INSERT INTO push_subscriptions (id, parent_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), p.ParentID, b.Endpoint, b.Keys.P256dh, b.Keys.Auth)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "subscribe push", err)
	}
	if created {
		return api.SubscribePush201Response{}, nil
	}
	return api.SubscribePush204Response{}, nil
}

// UnsubscribePush removes one endpoint of the caller. Always 204 (idempotent).
func (s *Server) UnsubscribePush(ctx context.Context, req api.UnsubscribePushRequestObject) (api.UnsubscribePushResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || req.Body.Endpoint == "" {
		return nil, httpapi.Validation(plField("endpoint", "required"))
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ? AND parent_id = ?`, req.Body.Endpoint, p.ParentID)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "unsubscribe push", err)
	}
	return api.UnsubscribePush204Response{}, nil
}

// ───────────── Events ─────────────

// plEventName limits event names to a safe vocabulary; anything else is dropped silently (contract).
var plEventName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// plChildHash pseudonymises a child id with a keyed hash so the events table never holds the real id.
func (s *Server) plChildHash(childID string) string {
	m := hmac.New(sha256.New, []byte(s.Config.JWTSecret))
	m.Write([]byte("analytics:" + childID))
	return hex.EncodeToString(m.Sum(nil))[:24]
}

// SendEvents stores a batch of UI events. It keeps only scalar props with short values (no free text)
// and always answers 202, even when everything was dropped.
func (s *Server) SendEvents(ctx context.Context, req api.SendEventsRequestObject) (api.SendEventsResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return api.SendEvents202Response{}, nil
	}
	var hash any // NULL for parent events
	if p.Role == auth.RoleChild {
		hash = s.plChildHash(p.ChildID)
	}
	events := req.Body.Events
	if len(events) > 100 {
		events = events[:100]
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		for _, ev := range events {
			if !plEventName.MatchString(ev.Name) {
				continue
			}
			props := map[string]any{"at": ev.At.UTC().Format(time.RFC3339)}
			if ev.Props != nil {
				for k, v := range *ev.Props {
					if len(props) >= 21 || !plEventName.MatchString(k) {
						continue
					}
					switch x := v.(type) {
					case bool, float64:
						props[k] = x
					case string:
						if len(x) <= 40 { // short enum-like values only
							props[k] = x
						}
					}
				}
			}
			raw, _ := json.Marshal(props)
			if _, err := tx.ExecContext(ctx, `INSERT INTO analytics_events (child_hash, name, json) VALUES (?, ?, ?)`, hash, ev.Name, string(raw)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "store events", err)
	}
	return api.SendEvents202Response{}, nil
}
