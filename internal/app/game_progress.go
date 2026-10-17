package app

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/narrator"
)

// misconceptions turns the profile's active error codes into the contract's list, sorted by code.
func misconceptions(prof *engine.Profile) []api.Misconception {
	active := prof.ActiveMisconceptions()
	out := make([]api.Misconception, 0, len(active))
	for code, skills := range active {
		desc := prof.Descs[code]
		if desc == "" {
			desc = code
		}
		out = append(out, api.Misconception{Code: code, Description: desc, SkillIds: skills})
	}
	slices.SortFunc(out, func(a, b api.Misconception) int { return strings.Compare(a.Code, b.Code) })
	return out
}

// GetSessionSummary answers GET /sessions/{sessionId}/summary: score, skills practised, stuck points, story recap.
func (s *Server) GetSessionSummary(ctx context.Context, req api.GetSessionSummaryRequestObject) (api.GetSessionSummaryResponseObject, error) {
	r, c, err := s.loadSession(ctx, plUID(req.SessionId))
	if err != nil {
		return nil, err
	}
	g := s.game()
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	view, err := s.sessionView(ctx, r, lang, true, true)
	if err != nil {
		return nil, err
	}
	// answers of this session: outcome and error code, first and last time
	rows, err := s.Store.R.QueryContext(ctx, `SELECT COALESCE(outcome,''), COALESCE(json_extract(json,'$.code_erreur'),''), created_at
		FROM answers WHERE session_id = ? ORDER BY created_at, rowid`, r.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load answers", err)
	}
	defer rows.Close()
	attempts, correct := 0, 0
	var codes []string
	var last time.Time
	for rows.Next() {
		var outcome, code, at string
		if err := rows.Scan(&outcome, &code, &at); err != nil {
			return nil, s.plInternalErr(ctx, "scan answer", err)
		}
		attempts++
		if outcome == string(engine.Correct) {
			correct++
		}
		if code != "" && !slices.Contains(codes, code) {
			codes = append(codes, code)
		}
		last = plParseTS(at)
	}
	if err := rows.Err(); err != nil {
		return nil, s.plInternalErr(ctx, "read answers", err)
	}
	seconds := 0
	if !last.IsZero() {
		seconds = int(min(20*time.Minute, max(0, last.Sub(plParseTS(r.Started)))).Seconds())
	}
	skill := api.SessionSummarySkillsReason(r.Data.Reason)
	entry := struct {
		Attempts int                             `json:"attempts"`
		Correct  int                             `json:"correct"`
		Label    string                          `json:"label"`
		Reason   *api.SessionSummarySkillsReason `json:"reason,omitempty"`
		SkillId  api.SkillId                     `json:"skill_id"`
	}{Attempts: attempts, Correct: correct, Label: g.skillLabel(r.Data.Skill, lang), SkillId: r.Data.Skill}
	if slices.Contains([]api.SessionSummarySkillsReason{api.Revision, api.Consolidation, api.Nouvelle, api.Entretien}, skill) {
		entry.Reason = &skill
	}
	out := api.SessionSummary{Session: view, DurationSeconds: seconds}
	out.Skills = append(out.Skills, entry)
	stuck := []api.Misconception{}
	if len(codes) > 0 {
		prof, err := s.loadProfile(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		for _, code := range codes {
			desc := prof.Descs[code]
			if desc == "" {
				desc = code
			}
			stuck = append(stuck, api.Misconception{Code: code, Description: desc, SkillIds: []string{r.Data.Skill}})
		}
	}
	out.StuckPoints = &stuck
	hero, _ := narrator.HeroOf(r.World, r.Data.Seed, lang)
	title, place := narrator.TitleOf(r.World, r.Data.TitleIdx, lang), narrator.PlaceOf(r.World, lang)
	var recap string
	if lang == "en" {
		recap = c.DisplayName + " and " + hero + " lived “" + title + "” " + place + ". " + strconv.Itoa(correct) + " of " + strconv.Itoa(attempts) + " right on the first try."
	} else {
		recap = c.DisplayName + " et " + hero + " ont vécu « " + title + " » " + place + ". " + strconv.Itoa(correct) + " sur " + strconv.Itoa(attempts) + " du premier coup."
	}
	out.StoryRecap = &recap
	return api.GetSessionSummary200JSONResponse(out), nil
}

// GetProgress answers GET /children/{childId}/progress: mastery per implemented skill plus active misconceptions.
func (s *Server) GetProgress(ctx context.Context, req api.GetProgressRequestObject) (api.GetProgressResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), false)
	if err != nil {
		return nil, err
	}
	g := s.game()
	prof, err := s.loadProfile(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	now := g.now()
	mastered := prof.Mastered(now)
	due := map[string]bool{}
	for _, d := range prof.ToReview(now) {
		due[d.SkillID] = true
	}
	skills := []api.SkillProgress{}
	for _, id := range g.graph.Implemented() {
		sp := api.SkillProgress{SkillId: id, Mastery: float32(prof.Mastery(id, now)), State: g.skillState(prof, id, mastered, due)}
		if e, ok := prof.States[id]; ok {
			attempts, last := e.Obs, plParseTS(e.Last)
			sp.Attempts, sp.LastPracticedAt = &attempts, &last
			if t, ok := prof.NextReview(id, now); ok {
				sp.NextReviewAt = &t
			}
		}
		skills = append(skills, sp)
	}
	return api.GetProgress200JSONResponse(api.Progress{Skills: skills, Misconceptions: misconceptions(prof)}), nil
}

// GetReviewCalendar answers GET /children/{childId}/reviews?days=N: mastered skills whose mastery will drop
// under the threshold within N days (default 30), soonest first.
func (s *Server) GetReviewCalendar(ctx context.Context, req api.GetReviewCalendarRequestObject) (api.GetReviewCalendarResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), false)
	if err != nil {
		return nil, err
	}
	g := s.game()
	days := 30
	if req.Params.Days != nil && *req.Params.Days > 0 {
		days = min(*req.Params.Days, 365)
	}
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
	prof, err := s.loadProfile(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	now := g.now()
	horizon := now.Add(time.Duration(days) * 24 * time.Hour)
	items := []api.ReviewDue{}
	for id := range prof.States {
		if t, ok := prof.NextReview(id, now); ok && !t.After(horizon) {
			items = append(items, api.ReviewDue{SkillId: id, Label: g.skillLabel(id, lang), DueAt: t})
		}
	}
	slices.SortFunc(items, func(a, b api.ReviewDue) int {
		if c := a.DueAt.Compare(b.DueAt); c != 0 {
			return c
		}
		return strings.Compare(a.SkillId, b.SkillId)
	})
	return api.GetReviewCalendar200JSONResponse{Items: items}, nil
}

// masteredTimes lists when each skill-mastery event happened for a child.
func (s *Server) masteredTimes(ctx context.Context, childID string) ([]time.Time, error) {
	rows, err := s.Store.R.QueryContext(ctx, `SELECT a.created_at FROM answers a JOIN sessions s ON s.id = a.session_id
		WHERE s.child_id = ? AND json_extract(a.json,'$.mastered') = 1`, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, plParseTS(at))
	}
	return out, rows.Err()
}

// growthAt computes the two-period growth ending at now, including mastered skills.
func (s *Server) growthAt(ctx context.Context, childID string, now time.Time, window int) (api.Growth, error) {
	sessions, err := s.feedSessions(ctx, childID)
	if err != nil {
		return api.Growth{}, s.plInternalErr(ctx, "load sessions", err)
	}
	gr := feedGrowth(sessions, now, window)
	times, err := s.masteredTimes(ctx, childID)
	if err != nil {
		return gr, s.plInternalErr(ctx, "load mastery events", err)
	}
	span := time.Duration(window) * 24 * time.Hour
	for _, t := range times {
		addTo(&gr, now, span, t, func(p *api.GrowthPeriod) { p.SkillsMastered++ })
	}
	return gr, nil
}

// GetGrowth answers GET /children/{childId}/growth: the same child, last window against the one before.
func (s *Server) GetGrowth(ctx context.Context, req api.GetGrowthRequestObject) (api.GetGrowthResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), false)
	if err != nil {
		return nil, err
	}
	window := 7
	if req.Params.WindowDays != nil && *req.Params.WindowDays > 0 {
		window = min(int(*req.Params.WindowDays), 365)
	}
	gr, err := s.growthAt(ctx, c.ID, s.game().now(), window)
	if err != nil {
		return nil, err
	}
	return api.GetGrowth200JSONResponse(gr), nil
}

// GetWeeklyReport answers GET /reports/weekly?child_id=...&week_start=... (parent only).
func (s *Server) GetWeeklyReport(ctx context.Context, req api.GetWeeklyReportRequestObject) (api.GetWeeklyReportResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.Params.ChildId), false)
	if err != nil {
		return nil, err
	}
	loc := feedLoc(c)
	now := s.game().now()
	var start time.Time // Monday 00:00 in the child's time zone
	if req.Params.WeekStart != nil {
		d := req.Params.WeekStart.Time
		start = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		if start.Weekday() != time.Monday {
			return nil, httpapi.Validation(plField("week_start", "must be a Monday"))
		}
	} else { // default: the last full week
		local := now.In(loc)
		back := (int(local.Weekday())+6)%7 + 7
		start = time.Date(local.Year(), local.Month(), local.Day()-back, 0, 0, 0, 0, loc)
	}
	end := start.AddDate(0, 0, 7)

	sessions, err := s.feedSessions(ctx, c.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load sessions", err)
	}
	count := 0
	for _, x := range sessions {
		if !x.Started.Before(start) && x.Started.Before(end) {
			count++
		}
	}
	rows, err := s.Store.R.QueryContext(ctx, `SELECT COALESCE(json_extract(a.json,'$.skill_id'),''), COALESCE(json_extract(a.json,'$.code_erreur'),''), a.created_at
		FROM answers a JOIN sessions s ON s.id = a.session_id WHERE s.child_id = ? AND a.created_at >= ? AND a.created_at < ?`,
		c.ID, plTS(start), plTS(end))
	if err != nil {
		return nil, s.plInternalErr(ctx, "load answers", err)
	}
	defer rows.Close()
	days := map[string]bool{}
	var skills, codes []string
	for rows.Next() {
		var skill, code, at string
		if err := rows.Scan(&skill, &code, &at); err != nil {
			return nil, s.plInternalErr(ctx, "scan answer", err)
		}
		days[plParseTS(at).In(loc).Format("2006-01-02")] = true
		if skill != "" && !slices.Contains(skills, skill) {
			skills = append(skills, skill)
		}
		if code != "" {
			codes = append(codes, code)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, s.plInternalErr(ctx, "read answers", err)
	}
	slices.Sort(skills)
	stuck := []api.Misconception{}
	if len(codes) > 0 {
		prof, err := s.loadProfile(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, code := range codes {
			if seen[code] {
				continue
			}
			seen[code] = true
			desc := prof.Descs[code]
			if desc == "" {
				desc = code
			}
			stuck = append(stuck, api.Misconception{Code: code, Description: desc, SkillIds: skillsWithCode(prof, code)})
		}
		slices.SortFunc(stuck, func(a, b api.Misconception) int { return strings.Compare(a.Code, b.Code) })
	}
	at := end
	if at.After(now) {
		at = now
	}
	growth, err := s.growthAt(ctx, c.ID, at, 7)
	if err != nil {
		return nil, err
	}
	dp := len(days)
	return api.GetWeeklyReport200JSONResponse(api.WeeklyReport{
		ChildId: plUUID(c.ID), WeekStart: openapi_types.Date{Time: start}, Sessions: count, DaysPlayed: &dp,
		SkillsPractised: &skills, StuckPoints: &stuck, Growth: growth,
	}), nil
}

// skillsWithCode lists the skills where a code was recorded; falls back to an empty list.
func skillsWithCode(prof *engine.Profile, code string) []string {
	if ids := prof.ActiveMisconceptions()[code]; len(ids) > 0 {
		return ids
	}
	return []string{}
}
