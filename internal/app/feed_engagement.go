// Operations that read the child's progress: getEngagement, getDailyQuest, getHome, getCollection.
//
// Go for Spring people: everything here only READS tables written by the game module (sessions, answers,
// collection, child_profiles). Days are computed in the child's own time zone (time.Location, like java.time.ZoneId).
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// feedWorld is one world of the catalogue as the home screen shows it.
type feedWorldInfo struct {
	ID         api.WorldId
	FR, EN     string // names
	HookFR     string // teaser lines
	HookEN     string
	Color      string
	SkillCount int
}

// feedWorlds is the world catalogue (same data as contracts/wiremock/mappings/037-home.json).
var feedWorlds = []feedWorldInfo{
	{"nature", "Nature", "Nature", "Explore la forêt et ses secrets", "Explore the forest and its secrets", "#3FA34D", 41},
	{"espace", "Espace", "Space", "Une mission vers les étoiles", "A mission to the stars", "#5B5BD6", 32},
	{"cuisine", "Cuisine", "Cooking", "Un grand repas à préparer", "A big meal to cook", "#F07E2B", 39},
	{"sport", "Sport", "Sport", "Le grand match approche", "The big match is coming", "#1E88E5", 29},
	{"construction", "Construction", "Building", "Bâtis quelque chose d’incroyable", "Build something amazing", "#E0A100", 44},
	{"medecine", "Médecine", "Medicine", "Soigne et sauve la journée", "Heal and save the day", "#14A0A0", 52},
	{"science", "Science", "Science", "Le laboratoire a besoin de toi", "The lab needs you", "#8E44AD", 75},
	{"echecs", "Échecs", "Chess", "Un tournoi t’attend", "A tournament awaits", "#4A5568", 43},
}

// starterSkill is the skill suggested when the child has no review due (same default as the mock).
const starterSkill api.SkillId = "ARI-OPE-A06a"

// feedTime parses a stored timestamp; the zero time means missing or malformed.
func feedTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// feedLoc returns the child's time zone, UTC if the stored name is unknown.
func feedLoc(c plChild) *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// feedSession is the part of a session row the engagement maths need.
type feedSession struct {
	World             string
	Status, Kind      string
	Started, Finished time.Time
	Answers           int
	LastAnswer        time.Time
}

// feedSessions loads every session of a child (read-only, tables owned by the game module).
func (s *Server) feedSessions(ctx context.Context, childID string) ([]feedSession, error) {
	rows, err := s.Store.R.QueryContext(ctx, `SELECT COALESCE(s.world_id,''), s.status, COALESCE(json_extract(s.json,'$.kind'),'chapter'),
		s.started_at, COALESCE(s.finished_at,''), (SELECT COUNT(*) FROM answers a WHERE a.session_id = s.id),
		COALESCE((SELECT MAX(a.created_at) FROM answers a WHERE a.session_id = s.id),'')
		FROM sessions s WHERE s.child_id = ?`, childID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // release the connection when done
	var out []feedSession
	for rows.Next() {
		var x feedSession
		var st, fin, last string
		if err := rows.Scan(&x.World, &x.Status, &x.Kind, &st, &fin, &x.Answers, &last); err != nil {
			return nil, err
		}
		x.Started, x.Finished, x.LastAnswer = feedTime(st), feedTime(fin), feedTime(last)
		out = append(out, x)
	}
	return out, rows.Err()
}

// day formats t as a local calendar day, the unit of streaks and goals.
func feedDay(t time.Time, loc *time.Location) string { return t.In(loc).Format("2006-01-02") }

// feedMinutes is the active duration of a session clamped to 1..20 minutes; 0 when no answer was given.
func feedMinutes(x feedSession) int {
	if x.Answers == 0 {
		return 0
	}
	end := x.Finished
	if end.IsZero() {
		end = x.LastAnswer
	}
	return int(math.Min(20, math.Max(1, math.Round(end.Sub(x.Started).Minutes()))))
}

// feedGoalTarget reads daily_goal_chapters (1..5) from the child's settings, default 1.
func (s *Server) feedGoalTarget(ctx context.Context, childID string) int {
	var raw string
	if err := s.Store.R.QueryRowContext(ctx, `SELECT json FROM child_settings WHERE child_id = ?`, childID).Scan(&raw); err != nil {
		return 1 // no settings row yet
	}
	var st struct {
		Goal int `json:"daily_goal_chapters"`
	}
	if json.Unmarshal([]byte(raw), &st) != nil || st.Goal < 1 || st.Goal > 5 {
		return 1
	}
	return st.Goal
}

// feedEngagement computes streak, daily goal and minutes for a child at time now.
func (s *Server) feedEngagement(ctx context.Context, c plChild, sessions []feedSession, now time.Time) api.Engagement {
	loc := feedLoc(c)
	days := map[string]bool{} // local days with at least one completed chapter
	doneToday, minutes := 0, 0
	var last time.Time
	today := feedDay(now, loc)
	for _, x := range sessions {
		if x.Status == "completed" && !x.Finished.IsZero() {
			d := feedDay(x.Finished, loc)
			days[d] = true
			if d == today {
				doneToday++
			}
			if x.Finished.After(last) {
				last = x.Finished
			}
		}
		if !x.Started.IsZero() && feedDay(x.Started, loc) == today {
			minutes += feedMinutes(x)
		}
	}
	list := make([]string, 0, len(days))
	for d := range days {
		list = append(list, d)
	}
	sort.Strings(list) // ISO dates sort chronologically
	longest, run, prev := 0, 0, time.Time{}
	for _, d := range list {
		t, _ := time.Parse("2006-01-02", d)
		if !prev.IsZero() && t.Sub(prev) == 24*time.Hour { // consecutive day
			run++
		} else {
			run = 1
		}
		longest = max(longest, run)
		prev = t
	}
	// Current streak: count back from today, or from yesterday if nothing was done yet today.
	cursor := now.In(loc)
	if !days[today] {
		cursor = cursor.AddDate(0, 0, -1)
	}
	current := 0
	for days[cursor.Format("2006-01-02")] {
		current++
		cursor = cursor.AddDate(0, 0, -1)
	}
	e := api.Engagement{MinutesToday: &minutes}
	e.Streak.CurrentDays, e.Streak.LongestDays = current, max(longest, current)
	if len(list) > 0 {
		t, _ := time.Parse("2006-01-02", list[len(list)-1])
		d := openapi_types.Date{Time: t}
		e.Streak.LastActiveDate = &d
	}
	e.DailyGoal.TargetChapters, e.DailyGoal.DoneChapters = s.feedGoalTarget(ctx, c.ID), doneToday
	if !last.IsZero() {
		e.LastPlayedAt = &last
	}
	return e
}

// feedGrowth compares the last window days with the window before (the same child only, nobody else).
func feedGrowth(sessions []feedSession, now time.Time, window int) api.Growth {
	g := api.Growth{WindowDays: window}
	span := time.Duration(window) * 24 * time.Hour
	for _, x := range sessions {
		if x.Status == "completed" && !x.Finished.IsZero() {
			addTo(&g, now, span, x.Finished, func(p *api.GrowthPeriod) { p.ChaptersCompleted++ })
		}
		if !x.Started.IsZero() {
			m := feedMinutes(x)
			addTo(&g, now, span, x.Started, func(p *api.GrowthPeriod) { p.MinutesPlayed += m })
		}
	}
	return g // skills_mastered stays 0: it needs mastery history, which the game module does not store yet
}

// addTo applies f to the growth period that contains t, if any.
func addTo(g *api.Growth, now time.Time, span time.Duration, t time.Time, f func(*api.GrowthPeriod)) {
	switch {
	case t.After(now.Add(-span)) && !t.After(now):
		f(&g.Current)
	case t.After(now.Add(-2*span)) && !t.After(now.Add(-span)):
		f(&g.Previous)
	}
}

// GetEngagement answers GET /children/{childId}/engagement.
func (s *Server) GetEngagement(ctx context.Context, req api.GetEngagementRequestObject) (api.GetEngagementResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	ss, err := s.feedSessions(ctx, c.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load sessions", err)
	}
	return api.GetEngagement200JSONResponse(s.feedEngagement(ctx, c, ss, time.Now().UTC())), nil
}

// feedQuest builds today's review quest: skills due for review, else the starter skill.
func (s *Server) feedQuest(ctx context.Context, c plChild, sessions []feedSession, now time.Time) api.DailyQuest {
	loc := feedLoc(c)
	q := api.DailyQuest{Available: true}
	for _, x := range sessions { // completed when a daily_quest chapter finished today
		if x.Kind == "daily_quest" && x.Status == "completed" && !x.Finished.IsZero() && feedDay(x.Finished, loc) == feedDay(now, loc) {
			q.Completed = true
		}
	}
	ids := []api.SkillId{}
	var raw string
	if s.Store.R.QueryRowContext(ctx, `SELECT bkt FROM child_profiles WHERE child_id = ?`, c.ID).Scan(&raw) == nil {
		// shortcut: assumes bkt is {skillId: {"due_at": RFC3339}}; align with internal/engine once its format is final.
		var bkt map[string]struct {
			DueAt string `json:"due_at"`
		}
		if json.Unmarshal([]byte(raw), &bkt) == nil {
			for id, v := range bkt {
				if t := feedTime(v.DueAt); !t.IsZero() && !t.After(now) {
					ids = append(ids, api.SkillId(id))
				}
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] }) // stable answer
	if len(ids) == 0 {
		ids = append(ids, starterSkill)
	}
	if len(ids) > 3 {
		ids = ids[:3]
	}
	q.SkillIds = &ids
	return q
}

// GetDailyQuest answers GET /children/{childId}/quests/daily.
func (s *Server) GetDailyQuest(ctx context.Context, req api.GetDailyQuestRequestObject) (api.GetDailyQuestResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	ss, err := s.feedSessions(ctx, c.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load sessions", err)
	}
	return api.GetDailyQuest200JSONResponse(s.feedQuest(ctx, c, ss, time.Now().UTC())), nil
}

// feedCards loads the child's cards, newest first, optionally only one world; limit 0 means all.
func (s *Server) feedCards(ctx context.Context, c plChild, en bool, world *api.WorldId, limit int) ([]api.StoryCard, error) {
	rows, err := s.Store.R.QueryContext(ctx, `SELECT card_id, json, acquired_at FROM collection WHERE child_id = ? ORDER BY acquired_at DESC, card_id`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []api.StoryCard{} // never nil so JSON gives [] not null
	for rows.Next() {
		var id, raw, at string
		if err := rows.Scan(&id, &raw, &at); err != nil {
			return nil, err
		}
		card := feedCardFrom(id, raw, feedTime(at), en)
		if world != nil && card.World != *world {
			continue
		}
		cards = append(cards, card)
		if limit > 0 && len(cards) == limit {
			break
		}
	}
	return cards, rows.Err()
}

// feedCardNS makes card ids deterministic: the contract wants a uuid, the logical ids are "welcome" and "ch-<challenge>".
var feedCardNS = uuid.MustParse("6f1c1f0e-1b6b-4a52-9a53-3f6f2d6f5a10")

// feedCardFrom turns a collection row into the API card. Session cards store {world,title,session_id,image_url}.
func feedCardFrom(id, raw string, at time.Time, en bool) api.StoryCard {
	var j struct {
		World     string `json:"world"`
		Title     string `json:"title"`
		SessionID string `json:"session_id"`
		ImageURL  string `json:"image_url"`
	}
	_ = json.Unmarshal([]byte(raw), &j) // tolerant: bad JSON gives an empty card
	card := api.StoryCard{EarnedAt: at, World: api.WorldId(j.World), Title: j.Title, ImageUrl: j.ImageURL}
	if sid, err := uuid.Parse(j.SessionID); err == nil {
		u := plUUID(sid.String())
		card.SessionId = &u
	}
	if t, w, ok := feedStaticCard(id, en); ok { // gift and challenge cards are named by the catalogue
		card.Title, card.World = t, w
	}
	if u, err := uuid.Parse(id); err == nil {
		card.Id = plUUID(u.String())
	} else {
		card.Id = plUUID(uuid.NewSHA1(feedCardNS, []byte(id)).String())
	}
	if card.World == "" {
		card.World = "nature"
	}
	return card
}

// feedStaticCard names the welcome card and challenge cards from the catalogue.
func feedStaticCard(id string, en bool) (string, api.WorldId, bool) {
	if id == "welcome" {
		return map[bool]string{true: "The welcome gift", false: "Le cadeau de bienvenue"}[en], "nature", true
	}
	if d, ok := feedChallengeByCard(id); ok {
		return d.cardTitle[feedLang(en)], d.world, true
	}
	return "", "", false
}

// feedLang indexes the [fr, en] pairs.
func feedLang(en bool) int {
	if en {
		return 1
	}
	return 0
}

// GetCollection answers GET /children/{childId}/collection[?world=].
func (s *Server) GetCollection(ctx context.Context, req api.GetCollectionRequestObject) (api.GetCollectionResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	cards, err := s.feedCards(ctx, c, plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA", req.Params.World, 0)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load collection", err)
	}
	return api.GetCollection200JSONResponse{Items: cards}, nil
}

// GetHome answers GET /children/{childId}/home: everything the landing screen needs in one request.
func (s *Server) GetHome(ctx context.Context, req api.GetHomeRequestObject) (api.GetHomeResponseObject, error) {
	p, err := plCaller(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.plOwnedChild(ctx, p, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	en := plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA"
	now := time.Now().UTC()
	if _, err := s.settleChallenges(ctx, c); err != nil { // keep the badge count right
		return nil, err
	}
	ss, err := s.feedSessions(ctx, c.ID)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load sessions", err)
	}
	cards, err := s.feedCards(ctx, c, en, nil, 5)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load cards", err)
	}
	quest := s.feedQuest(ctx, c, ss, now)
	h := api.Home{Quest: quest, RecentCards: cards, Engagement: s.feedEngagement(ctx, c, ss, now), Growth: feedGrowth(ss, now, 7)}
	// Suggested chapter: favorite world, else the last played one, else nature. open_session stays null (see report).
	world := api.WorldId("nature")
	if c.FavoriteWorld.Valid {
		world = api.WorldId(c.FavoriteWorld.String)
	}
	h.Suggested.World = world
	skill := (*quest.SkillIds)[0]
	h.Suggested.SkillId = &skill
	h.Suggested.Label = map[bool]string{true: "Continue the adventure", false: "Continue l’aventure"}[en]
	homeLang := "fr"
	if en {
		homeLang = "en"
	}
	worlds, werr := s.worldViews(ctx, homeLang)
	if werr != nil {
		return nil, werr
	}
	h.Worlds = append(h.Worlds, worlds...)
	gift := false
	if s.Config.FeatureWelcomeGift {
		var n int
		if err := s.Store.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection WHERE child_id = ? AND card_id = 'welcome'`, c.ID).Scan(&n); err != nil && err != sql.ErrNoRows {
			return nil, s.plInternalErr(ctx, "load gift", err)
		}
		gift = n == 0
	}
	h.GiftAvailable = &gift
	views, err := s.feedChallengeViews(ctx, c, en)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load challenges", err)
	}
	open := 0
	for _, v := range views {
		if v.Status != "done" {
			open++
		}
	}
	h.ActiveChallenges = &open
	return api.GetHome200JSONResponse(h), nil
}
