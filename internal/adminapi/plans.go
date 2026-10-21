package adminapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
)

type bi struct {
	FR string `json:"fr"`
	EN string `json:"en"`
}

type planDef struct {
	ID            string `json:"id"`
	PriceCADCents int    `json:"price_cad_cents"`
	Period        string `json:"period"` // none, month, year
	Name          bi     `json:"name"`
	Features      struct {
		FR []string `json:"fr"`
		EN []string `json:"en"`
	} `json:"features"`
}

type methodDef struct {
	ID    string `json:"id"`
	Label bi     `json:"label"`
}

// plansDoc is the plans + payment methods document stored under admin_kv key "plans".
type plansDoc struct {
	Currency string      `json:"currency"`
	Plans    []planDef   `json:"plans"`
	Methods  []methodDef `json:"methods"`
}

var methodIDs = []string{"card", "interac", "paypal", "apple_pay", "google_pay"}

// defaultPlans is used until an admin saves a plans document (same values as the mock).
func defaultPlans() plansDoc {
	mk := func(id string, price int, period, fr, en string, ffr, fen []string) planDef {
		p := planDef{ID: id, PriceCADCents: price, Period: period, Name: bi{fr, en}}
		p.Features.FR, p.Features.EN = ffr, fen
		return p
	}
	return plansDoc{Currency: "CAD",
		Plans: []planDef{
			mk("free", 0, "none", "Gratuit", "Free", []string{"Un profil enfant", "Un chapitre par jour", "Rapports de base"}, []string{"One child profile", "One chapter a day", "Basic reports"}),
			mk("family_monthly", 899, "month", "Famille mensuel", "Family monthly", []string{"Jusqu’à 8 profils enfants", "Chapitres illimités", "Rapports détaillés pour les parents", "Annulable à tout moment"}, []string{"Up to 8 child profiles", "Unlimited chapters", "Detailed parent reports", "Cancel any time"}),
			mk("family_yearly", 8990, "year", "Famille annuel", "Family yearly", []string{"Tout le forfait mensuel", "Deux mois offerts", "Annulable à tout moment"}, []string{"Everything in monthly", "Two months free", "Cancel any time"}),
		},
		Methods: []methodDef{
			{"card", bi{"Carte de crédit", "Credit card"}}, {"interac", bi{"Interac", "Interac"}}, {"paypal", bi{"PayPal", "PayPal"}},
			{"apple_pay", bi{"Apple Pay", "Apple Pay"}}, {"google_pay", bi{"Google Pay", "Google Pay"}},
		}}
}

// kvQuerier is *sql.DB or *sql.Tx.
type kvQuerier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// loadKV reads a JSON document from admin_kv into out; false when the key does not exist.
func loadKV(ctx context.Context, q kvQuerier, key string, out any) (bool, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT json FROM admin_kv WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), out)
}

// saveKV upserts a JSON document into admin_kv.
func (a *API) saveKV(tx *sql.Tx, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO admin_kv (key, json, updated_at) VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET json = excluded.json, updated_at = excluded.updated_at`, key, string(raw), a.stamp())
	if err == nil && key == "plans" { // the game reads plan_catalog; before this mirror, admin edits never reached it
		_, err = tx.Exec(`INSERT INTO plan_catalog (id, json, updated_at) VALUES (1,?,?) ON CONFLICT(id) DO UPDATE SET json = excluded.json, updated_at = excluded.updated_at`, string(raw), a.stamp())
	}
	return err
}

// loadPlans returns the saved plans document or the defaults.
func loadPlans(ctx context.Context, q kvQuerier) (plansDoc, error) {
	doc := plansDoc{}
	ok, err := loadKV(ctx, q, "plans", &doc)
	if err != nil || !ok {
		return defaultPlans(), err
	}
	return doc, nil
}

// GET /admin/v1/subscriptions/plans
func (a *API) getPlans(w http.ResponseWriter, r *http.Request) {
	doc, err := loadPlans(r.Context(), a.db.R)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// PUT /admin/v1/subscriptions/plans: body {plans?, methods?}; absent parts keep their current value.
func (a *API) putPlans(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Plans   *[]planDef   `json:"plans"`
		Methods *[]methodDef `json:"methods"`
	}
	if err := decode(r, &b); err != nil {
		a.fail(w, r, err)
		return
	}
	var out plansDoc
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		cur, err := loadPlans(r.Context(), tx)
		if err != nil {
			return err
		}
		if b.Plans != nil {
			cur.Plans = *b.Plans
		}
		if b.Methods != nil {
			cur.Methods = *b.Methods
		}
		if err := validatePlans(cur); err != nil {
			return err
		}
		cur.Currency, out = "CAD", cur
		detail := ""
		for _, p := range cur.Plans {
			detail += p.ID + "=" + strconv.Itoa(p.PriceCADCents) + " "
		}
		if err := a.saveKV(tx, "plans", cur); err != nil {
			return err
		}
		return a.writeAudit(tx, actor(r), "plans.update", "plans", "*", detail)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// validatePlans enforces: exactly free, family_monthly, family_yearly; sane prices; bilingual texts; known methods.
func validatePlans(d plansDoc) error {
	if len(d.Plans) != len(planIDs) {
		return bad("plans must hold free, family_monthly and family_yearly")
	}
	for _, id := range planIDs {
		if !slices.ContainsFunc(d.Plans, func(p planDef) bool { return p.ID == id }) {
			return bad("plans must hold free, family_monthly and family_yearly")
		}
	}
	for _, p := range d.Plans {
		if p.PriceCADCents < 0 || p.PriceCADCents > 1_000_000 || !slices.Contains([]string{"none", "month", "year"}, p.Period) ||
			p.Name.FR == "" || p.Name.EN == "" || p.Features.FR == nil || p.Features.EN == nil {
			return bad("each plan needs id, price_cad_cents, period, name{fr,en}, features{fr[],en[]}")
		}
	}
	if len(d.Methods) == 0 {
		return bad("methods must be a non-empty list")
	}
	for _, m := range d.Methods {
		if !slices.Contains(methodIDs, m.ID) || m.Label.FR == "" || m.Label.EN == "" {
			return bad("methods must use known ids with label{fr,en}")
		}
	}
	return nil
}
