package app

// Platform config, plans and the parent subscription (BE-24, BE-25 part 1, BE-62).
//
// Go for Spring people: `interface` in Go is satisfied implicitly. PaymentProvider below is like a Spring
// interface, and devPayments implements it just by having the same methods (no `implements` keyword).
// The consumer (this file) defines the interface, the real Stripe-like provider will live elsewhere.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// plPtr returns a pointer to a copy of v (the contract models use pointers for optional fields).
func plPtr[T any](v T) *T { return &v }

// ───────────── Payment provider (no real processor yet) ─────────────

// CheckoutRequest is what the billing code asks of a payment provider.
type CheckoutRequest struct {
	ParentID string
	Plan     string // family_monthly or family_yearly
	Method   string // card, interac, ...
}

// CheckoutSession is the provider's answer: either the payment is already done (Activated) or the
// browser must go to URL (hosted page, https only) and a webhook will confirm later.
type CheckoutSession struct {
	Ref       string // provider's subscription or session id, stored in subscriptions.provider_ref
	URL       string
	Activated bool
}

// PaymentProvider is the seam for a real processor (Spring: a strategy interface behind @Service).
type PaymentProvider interface {
	Checkout(ctx context.Context, r CheckoutRequest) (CheckoutSession, error)
	Cancel(ctx context.Context, ref string) error
}

// devPayments completes every checkout at once without taking money, like the frontend mock.
// It is refused in production: plPayments only returns it when Config.Env is not production.
type devPayments struct{}

func (devPayments) Checkout(_ context.Context, _ CheckoutRequest) (CheckoutSession, error) {
	return CheckoutSession{Ref: "dev_" + uuid.NewString(), Activated: true}, nil
}
func (devPayments) Cancel(context.Context, string) error { return nil }

// plPayments picks the provider: the one wired in Server.Payments, else dev outside production, else none.
func (s *Server) plPayments() PaymentProvider {
	if s.Payments != nil {
		return s.Payments
	}
	if !s.Config.IsProduction() {
		return devPayments{}
	}
	return nil
}

// ───────────── Platform config and plan catalog (defaults when the admin never saved one) ─────────────

// platformConfig is the JSON document in platform_config (same keys as the mock's DEFAULT_CONFIG).
type platformConfig struct {
	CardThresholdPct             int             `json:"card_threshold_pct"`
	UnpaidPolicy                 string          `json:"unpaid_policy"`
	TrialDays                    int             `json:"trial_days"`
	DailyTimeLimitMinutesDefault int             `json:"daily_time_limit_minutes_default"`
	ReminderHourLocal            int             `json:"reminder_hour_local"`
	ExpiringWarningDays          int             `json:"expiring_warning_days"`
	DefaultLocale                string          `json:"default_locale"`
	Features                     map[string]bool `json:"features"`
}

func plDefaultConfig() platformConfig {
	return platformConfig{
		CardThresholdPct: 70, UnpaidPolicy: "block", TrialDays: 14, DailyTimeLimitMinutesDefault: 60,
		ReminderHourLocal: 17, ExpiringWarningDays: 7, DefaultLocale: "fr-CA",
		Features: map[string]bool{"feed": true, "challenges": true, "push": false, "payments": true},
	}
}

// plConfig reads the config row over the defaults; on any problem the defaults are used (never fails a request).
func (s *Server) plConfig(ctx context.Context) platformConfig {
	cfg := plDefaultConfig()
	var raw string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT json FROM platform_config WHERE id = 1`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg) // keys missing in the JSON keep their default
	}
	return cfg
}

// plDefaultPlans is the catalog of the mock (CAD cents). Stored form and API form are the same JSON.
const plDefaultPlans = `{"currency":"CAD","plans":[
{"id":"free","price_cad_cents":0,"period":"none","name":{"fr":"Gratuit","en":"Free"},"features":{"fr":["Un profil enfant","Un chapitre par jour","Rapports de base"],"en":["One child profile","One chapter a day","Basic reports"]}},
{"id":"family_monthly","price_cad_cents":899,"period":"month","name":{"fr":"Famille mensuel","en":"Family monthly"},"features":{"fr":["Jusqu’à 8 profils enfants","Chapitres illimités","Rapports détaillés pour les parents","Annulable à tout moment"],"en":["Up to 8 child profiles","Unlimited chapters","Detailed parent reports","Cancel any time"]}},
{"id":"family_yearly","price_cad_cents":8990,"period":"year","name":{"fr":"Famille annuel","en":"Family yearly"},"features":{"fr":["Tout le forfait mensuel","Deux mois offerts","Annulable à tout moment"],"en":["Everything in monthly","Two months free","Cancel any time"]}}],
"methods":[{"id":"card","label":{"fr":"Carte de crédit","en":"Credit card"}},{"id":"interac","label":{"fr":"Interac","en":"Interac"}},{"id":"paypal","label":{"fr":"PayPal","en":"PayPal"}},{"id":"apple_pay","label":{"fr":"Apple Pay","en":"Apple Pay"}},{"id":"google_pay","label":{"fr":"Google Pay","en":"Google Pay"}}]}`

// plPlans reads the catalog row, else the built-in default.
func (s *Server) plPlans(ctx context.Context) api.PlanCatalog {
	var out api.PlanCatalog
	var raw string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT json FROM plan_catalog WHERE id = 1`).Scan(&raw); err == nil {
		if json.Unmarshal([]byte(raw), &out) == nil && len(out.Plans) > 0 {
			return out
		}
	}
	out = api.PlanCatalog{}
	_ = json.Unmarshal([]byte(plDefaultPlans), &out) // constant JSON, cannot fail
	return out
}

// GetPlatformPublic exposes the public policy values (no auth).
func (s *Server) GetPlatformPublic(ctx context.Context, _ api.GetPlatformPublicRequestObject) (api.GetPlatformPublicResponseObject, error) {
	c := s.plConfig(ctx)
	return api.GetPlatformPublic200JSONResponse{
		UnpaidPolicy:                 api.PlatformPublicUnpaidPolicy(c.UnpaidPolicy),
		CardThresholdPct:             c.CardThresholdPct,
		TrialDays:                    &c.TrialDays,
		DefaultLocale:                &c.DefaultLocale,
		Features:                     &c.Features,
		DailyTimeLimitMinutesDefault: &c.DailyTimeLimitMinutesDefault,
		ReminderHourLocal:            &c.ReminderHourLocal,
		ExpiringWarningDays:          &c.ExpiringWarningDays,
	}, nil
}

// ListSubscriptionPlans returns the catalog.
func (s *Server) ListSubscriptionPlans(ctx context.Context, _ api.ListSubscriptionPlansRequestObject) (api.ListSubscriptionPlansResponseObject, error) {
	return api.ListSubscriptionPlans200JSONResponse(s.plPlans(ctx)), nil
}

// ───────────── Subscription state ─────────────

// plSub is one row of the subscriptions table.
type plSub struct {
	ID, Status, PlanID                           string
	TrialEnds, RenewsAt, CanceledAt, ProviderRef sql.NullString
	Method                                       string // from the json column
	MarkedByAdmin                                bool   // from the json column
}

// plLoadSub reads the parent's subscription, creating a trial when none exists yet (admin-created parents).
func (s *Server) plLoadSub(ctx context.Context, parentID string) (plSub, error) {
	q := `SELECT id, COALESCE(plan_id,'free'), status, trial_ends_at, current_period_end, canceled_at, provider_ref, json
	      FROM subscriptions WHERE parent_id = ?`
	scan := func(r plRowScanner) (plSub, error) {
		var sub plSub
		var raw string
		if err := r.Scan(&sub.ID, &sub.PlanID, &sub.Status, &sub.TrialEnds, &sub.RenewsAt, &sub.CanceledAt, &sub.ProviderRef, &raw); err != nil {
			return sub, err
		}
		var j struct {
			Method        string `json:"method"`
			MarkedByAdmin bool   `json:"marked_by_admin"`
		}
		_ = json.Unmarshal([]byte(raw), &j)
		sub.Method, sub.MarkedByAdmin = j.Method, j.MarkedByAdmin
		return sub, nil
	}
	sub, err := scan(s.Store.R.QueryRowContext(ctx, q, parentID))
	if errors.Is(err, sql.ErrNoRows) {
		end := plTS(time.Now().Add(time.Duration(s.plConfig(ctx).TrialDays) * plDay))
		err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `INSERT OR IGNORE INTO subscriptions (id, parent_id, plan_id, status, trial_ends_at) VALUES (?, ?, 'free', 'trial', ?)`,
				uuid.NewString(), parentID, end)
			return e
		})
		if err == nil {
			sub, err = scan(s.Store.R.QueryRowContext(ctx, q, parentID))
		}
	}
	if err != nil {
		return sub, s.plInternalErr(ctx, "load subscription", err)
	}
	return sub, nil
}

// plView computes the effective status, can_play and days_left (guide 8.2, same rules as the mock's subView).
func plView(sub plSub, now time.Time) api.SubscriptionView {
	trialEnd, renews := plNullTime(sub.TrialEnds), plNullTime(sub.RenewsAt)
	status, can := sub.Status, false
	var end *time.Time
	switch sub.Status {
	case "trial":
		end = trialEnd
		can = end != nil && end.After(now)
		if !can {
			status = "expired"
		}
	case "active":
		end = renews
		can = end == nil || end.After(now)
		if !can {
			status = "expired"
		}
	case "canceled":
		end = renews
		can = end != nil && end.After(now)
	}
	v := api.SubscriptionView{
		Status: api.SubscriptionViewStatus(status), Plan: api.SubscriptionViewPlan(sub.PlanID), CanPlay: can,
		TrialEndsAt: trialEnd, RenewsAt: renews,
	}
	if end != nil {
		v.DaysLeft = plPtr(int(math.Ceil(end.Sub(now).Hours() / 24)))
	}
	if sub.Method != "" {
		v.Method = plPtr(api.PaymentMethod(sub.Method))
	}
	if sub.MarkedByAdmin {
		v.MarkedByAdmin = plPtr(true)
	}
	return v
}

// GetSubscription returns the effective subscription of the caller.
func (s *Server) GetSubscription(ctx context.Context, _ api.GetSubscriptionRequestObject) (api.GetSubscriptionResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := s.plLoadSub(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	return api.GetSubscription200JSONResponse(plView(sub, time.Now())), nil
}

// CheckoutSubscription starts a paid plan through the payment provider (BE-24).
func (s *Server) CheckoutSubscription(ctx context.Context, req api.CheckoutSubscriptionRequestObject) (api.CheckoutSubscriptionResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	if !s.plConfig(ctx).Features["payments"] { // BE-44: payments switched off by the admin
		return nil, httpapi.NewError(http.StatusServiceUnavailable, "service_unavailable", "Payments are paused for now.")
	}
	catalog := s.plPlans(ctx)
	period := ""
	for _, pl := range catalog.Plans {
		if string(pl.Id) == string(req.Body.Plan) && pl.PriceCadCents > 0 {
			period = string(pl.Period)
		}
	}
	if period == "" {
		return nil, httpapi.Validation(plField("plan", "choose a paid plan"))
	}
	methodOK := false
	for _, m := range catalog.Methods {
		methodOK = methodOK || string(m.Id) == string(req.Body.Method)
	}
	if !methodOK {
		return nil, httpapi.Validation(plField("method", "choose one of the listed payment methods"))
	}
	prov := s.plPayments()
	if prov == nil {
		return nil, httpapi.NewError(http.StatusServiceUnavailable, "service_unavailable", "Payments are not available yet.")
	}
	sess, err := prov.Checkout(ctx, CheckoutRequest{ParentID: p.ParentID, Plan: string(req.Body.Plan), Method: string(req.Body.Method)})
	if err != nil {
		return nil, s.plInternalErr(ctx, "payment provider checkout", err)
	}
	if sess.Activated { // instant provider: activate now. A hosted page leaves the subscription alone until the webhook.
		now := time.Now()
		days := 30
		if period == "year" {
			days = 365
		}
		meta, _ := json.Marshal(map[string]string{"method": string(req.Body.Method)})
		if _, err := s.plLoadSub(ctx, p.ParentID); err != nil { // makes sure the row exists
			return nil, err
		}
		err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx,
				`UPDATE subscriptions SET plan_id = ?, status = 'active', current_period_end = ?, canceled_at = NULL,
				 provider_ref = ?, json = ?, updated_at = ? WHERE parent_id = ?`,
				string(req.Body.Plan), plTS(now.Add(time.Duration(days)*plDay)), sess.Ref, string(meta), plTS(now), p.ParentID)
			return e
		})
		if err != nil {
			return nil, s.plInternalErr(ctx, "activate subscription", err)
		}
	}
	sub, err := s.plLoadSub(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	v := plView(sub, time.Now())
	out := api.CheckoutResult{
		CanPlay: v.CanPlay, DaysLeft: v.DaysLeft, MarkedByAdmin: v.MarkedByAdmin, Method: v.Method,
		Plan: api.CheckoutResultPlan(v.Plan), RenewsAt: v.RenewsAt, Status: api.CheckoutResultStatus(v.Status), TrialEndsAt: v.TrialEndsAt,
	}
	if sess.URL != "" {
		out.CheckoutUrl = &sess.URL
	}
	return api.CheckoutSubscription200JSONResponse(out), nil
}

// CancelSubscription stops renewal; access continues to the end of the paid period or trial.
func (s *Server) CancelSubscription(ctx context.Context, _ api.CancelSubscriptionRequestObject) (api.CancelSubscriptionResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := s.plLoadSub(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	switch plView(sub, now).Status {
	case "canceled":
		return api.CancelSubscription200JSONResponse(plView(sub, now)), nil // already canceled: same answer
	case "trial", "active":
	default:
		return nil, plConflict("not_cancelable", "This subscription is not running.")
	}
	if sub.ProviderRef.Valid && s.plPayments() != nil {
		if err := s.plPayments().Cancel(ctx, sub.ProviderRef.String); err != nil {
			return nil, s.plInternalErr(ctx, "payment provider cancel", err)
		}
	}
	end := sub.RenewsAt
	if !end.Valid {
		end = sub.TrialEnds // a trial keeps running until its end date
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx,
			`UPDATE subscriptions SET status = 'canceled', current_period_end = ?, canceled_at = ?, updated_at = ? WHERE parent_id = ?`,
			end, plTS(now), plTS(now), p.ParentID)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "cancel subscription", err)
	}
	sub, err = s.plLoadSub(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	return api.CancelSubscription200JSONResponse(plView(sub, now)), nil
}
