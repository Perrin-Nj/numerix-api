package adminapi

import (
	"net/http"
	"sort"
	"time"
)

type dayVal struct {
	Date  string `json:"date"`
	Value int    `json:"value"`
}
type dayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// GET /admin/v1/overview: KPIs and series for the dashboard, computed from the live tables.
// Gaps (no history tables yet): mrr_by_month_12 and trend_30 carry real paid/MRR/retention only for "now".
func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	ctx, now := r.Context(), a.now().UTC()
	parents, err := a.findParents(ctx, parentFilter{})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	plans, err := loadPlans(ctx, a.db.R)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	monthly := func(plan string) int { // monthly price in cents; yearly plans divided by 12
		for _, p := range plans.Plans {
			if p.ID == plan {
				if p.Period == "year" {
					return (p.PriceCADCents + 6) / 12
				}
				return p.PriceCADCents
			}
		}
		return 0
	}
	dstr := func(t time.Time) string { return t.Format("2006-01-02") }
	today := now.Truncate(day)

	// subscriptions: status mix, plan mix, MRR, blocked
	statuses := []string{"trial", "active", "unpaid", "expired", "canceled"}
	by := map[string]int{}
	for _, s := range statuses {
		by[s] = 0
	}
	type planRow struct {
		Plan  string `json:"plan"`
		Count int    `json:"count"`
		MRR   int    `json:"mrr_cad_cents"`
	}
	mix := map[string]*planRow{}
	for _, p := range plans.Plans {
		mix[p.ID] = &planRow{Plan: p.ID}
	}
	mrr, blocked, signupDay, signupMonth := 0, 0, map[string]int{}, map[string]int{}
	for _, p := range parents {
		by[p.Subscription.Status]++
		row := mix[p.Subscription.Plan]
		if row == nil {
			row = &planRow{Plan: p.Subscription.Plan}
			mix[row.Plan] = row
		}
		row.Count++
		if p.Status == "blocked" {
			blocked++
		}
		if p.Subscription.Status == "active" && p.Status != "blocked" {
			v := monthly(p.Subscription.Plan)
			mrr, row.MRR = mrr+v, row.MRR+v
		}
		signupDay[p.CreatedAt[:10]]++
		signupMonth[p.CreatedAt[:7]]++
	}

	// children: totals, grades, creation dates
	crows, err := a.db.R.QueryContext(ctx, `SELECT COALESCE(grade,0), created_at FROM children WHERE deleted_at IS NULL`)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	grades, kidCreated := map[int]int{}, []string{}
	for crows.Next() {
		var g int
		var c string
		if err := crows.Scan(&g, &c); err != nil {
			crows.Close()
			a.fail(w, r, err)
			return
		}
		grades[g]++
		kidCreated = append(kidCreated, c)
	}
	crows.Close()

	// sessions per day over 90 days: active kids, sessions, minutes
	from := dstr(today.Add(-89 * day))
	type act struct{ kids, sessions, minutes int }
	byDay := map[string]act{}
	srows, err := a.db.R.QueryContext(ctx, `SELECT substr(started_at,1,10), COUNT(DISTINCT child_id), COUNT(*),
		COALESCE(CAST(SUM((julianday(COALESCE(finished_at, paused_at, started_at)) - julianday(started_at)) * 1440) AS INTEGER),0)
		FROM sessions WHERE substr(started_at,1,10) >= ? GROUP BY 1`, from)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for srows.Next() {
		var d string
		var x act
		if err := srows.Scan(&d, &x.kids, &x.sessions, &x.minutes); err != nil {
			srows.Close()
			a.fail(w, r, err)
			return
		}
		byDay[d] = x
	}
	srows.Close()

	// retention (kids older than 30 days who played in the last 7), churn (cancellations in 30 days), top worlds
	var eligible, retained, churn int
	week, month := now.Add(-7*day).Format(tsLayout), now.Add(-30*day).Format(tsLayout)
	if err := a.db.R.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(EXISTS(SELECT 1 FROM sessions s WHERE s.child_id = c.id AND s.started_at >= ?)),0)
		FROM children c WHERE c.deleted_at IS NULL AND c.created_at <= ?`, week, month).Scan(&eligible, &retained); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.db.R.QueryRowContext(ctx, `SELECT COUNT(DISTINCT parent_id) FROM subscriptions WHERE canceled_at >= ?`, month).Scan(&churn); err != nil {
		a.fail(w, r, err)
		return
	}
	type worldRow struct {
		World string `json:"world"`
		N     int    `json:"sessions_30d"`
	}
	worlds := []worldRow{}
	wrows, err := a.db.R.QueryContext(ctx, `SELECT world_id, COUNT(*) FROM sessions WHERE world_id IS NOT NULL AND started_at >= ? GROUP BY 1 ORDER BY 2 DESC`, month)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for wrows.Next() {
		var x worldRow
		if err := wrows.Scan(&x.World, &x.N); err != nil {
			wrows.Close()
			a.fail(w, r, err)
			return
		}
		worlds = append(worlds, x)
	}
	wrows.Close()

	// series
	type dayAct struct {
		Date       string `json:"date"`
		ActiveKids int    `json:"active_kids"`
		Minutes    int    `json:"minutes"`
		Sessions   int    `json:"sessions"`
	}
	days90 := make([]dayAct, 90)
	for i := range days90 {
		d := dstr(today.Add(time.Duration(i-89) * day))
		days90[i] = dayAct{d, byDay[d].kids, byDay[d].minutes, byDay[d].sessions}
	}
	last30 := days90[60:]
	pick := func(f func(dayAct) int) []dayVal {
		out := make([]dayVal, len(last30))
		for i, d := range last30 {
			out[i] = dayVal{d.Date, f(d)}
		}
		return out
	}
	signups30 := make([]dayCount, 30)
	signupsTotal := 0
	for i := range signups30 {
		d := dstr(today.Add(time.Duration(i-29) * day))
		signups30[i] = dayCount{d, signupDay[d]}
		signupsTotal += signupDay[d]
	}
	type monthCount struct {
		Month string `json:"month"`
		Count int    `json:"count"`
	}
	type monthMRR struct {
		Month string `json:"month"`
		MRR   int    `json:"mrr_cad_cents"`
	}
	signups12, mrr12 := make([]monthCount, 12), make([]monthMRR, 12)
	for i := range signups12 {
		m := time.Date(now.Year(), now.Month()-time.Month(11-i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
		signups12[i], mrr12[i] = monthCount{m, signupMonth[m]}, monthMRR{m, 0}
	}
	mrr12[11].MRR = mrr // only the current month is known
	grade := make([]map[string]int, 0, 6)
	for g := 1; g <= 6; g++ {
		grade = append(grade, map[string]int{"grade": g, "count": grades[g]})
	}
	paid := by["active"]
	arpu, retPct := 0, 0.0
	if paid > 0 {
		arpu = (mrr + paid/2) / paid
	}
	if eligible > 0 {
		retPct = float64(retained*1000/eligible) / 10
	}
	cumulative := func(created []string, end time.Time) int { // how many records existed at the end of that day
		stop := end.Add(day).Format(tsLayout)
		n := 0
		for _, c := range created {
			if c < stop {
				n++
			}
		}
		return n
	}
	parentCreated := make([]string, len(parents))
	for i, p := range parents {
		parentCreated[i] = p.CreatedAt
	}
	type trendRow struct {
		Date      string  `json:"date"`
		Parents   int     `json:"parents"`
		Kids      int     `json:"kids"`
		Paid      int     `json:"paid"`
		MRR       int     `json:"mrr_cad_cents"`
		ARPU      int     `json:"arpu_cad_cents"`
		Churn30   int     `json:"churn_30d"`
		Retention float64 `json:"retention_pct"`
	}
	trend := make([]trendRow, 31)
	for i := range trend {
		d := today.Add(time.Duration(i-30) * day)
		trend[i] = trendRow{Date: dstr(d), Parents: cumulative(parentCreated, d), Kids: cumulative(kidCreated, d)}
	}
	trend[30] = trendRow{dstr(today), len(parents), len(kidCreated), paid, mrr, arpu, churn, retPct}

	activeToday := byDay[dstr(today)].kids
	sort.Slice(worlds, func(i, j int) bool { return worlds[i].N > worlds[j].N })
	mixRows, subMix := []planRow{}, []map[string]any{}
	for _, p := range plans.Plans {
		mixRows = append(mixRows, *mix[p.ID])
	}
	for _, s := range statuses {
		subMix = append(subMix, map[string]any{"status": s, "count": by[s]})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"parents_total": len(parents), "by_subscription": by, "blocked": blocked,
		"kids_total": len(kidCreated), "active_today": activeToday, "mrr_cad_cents": mrr, "currency": "CAD",
		"signups_30d": signups30, "signups_30d_total": signupsTotal, "signups_by_day_30": signups30, "signups_by_month_12": signups12,
		"active_kids_by_day_30": pick(func(d dayAct) int { return d.ActiveKids }),
		"minutes_by_day_30":     pick(func(d dayAct) int { return d.Minutes }),
		"sessions_by_day_30":    pick(func(d dayAct) int { return d.Sessions }),
		"activity_by_day_90":    days90, "subscription_mix": subMix, "plan_mix": mixRows, "mrr_by_month_12": mrr12, "kids_by_grade": grade,
		"retention": map[string]any{"pct": retPct, "eligible": eligible, "active": retained},
		"churn_30d": churn, "arpu_cad_cents": arpu, "top_worlds": worlds, "trend_30": trend,
	})
}
