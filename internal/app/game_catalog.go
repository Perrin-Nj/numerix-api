package app

import (
	"context"
	"slices"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/engine"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// skillNames are the child-friendly chapter titles [fr, en] of the implemented skills (from the mock).
var skillNames = map[string][2]string{
	"ARI-OPE-A02a": {"Additions rapides", "Quick sums"},
	"ARI-OPE-A02c": {"Additions éclair", "Lightning sums"},
	"ARI-OPE-A03a": {"Calcul mental", "Mental math"},
	"ARI-OPE-A05":  {"Le terme manquant", "The missing term"},
	"ARI-OPE-A06a": {"Les tables", "Times tables"},
	"ARI-OPE-A06c": {"Tables express", "Express tables"},
	"ARI-OPE-A07a": {"Division avec reste", "Division with remainder"},
	"ARI-SOP-A03a": {"Partager et grouper", "Share and group"},
}

// skillLabel returns the friendly name in the language, else the curriculum label.
func (g *gameState) skillLabel(id, lang string) string {
	if n, ok := skillNames[id]; ok {
		if lang == "en" {
			return n[1]
		}
		return n[0]
	}
	if sk, err := g.graph.Get(id); err == nil {
		return sk.Label
	}
	return id
}

// gradeOf is the school year used by the planner; an unknown grade means "everything".
func gradeOf(c plChild) int {
	if c.Grade.Valid && c.Grade.Int64 > 0 {
		return int(c.Grade.Int64)
	}
	return 6
}

// ListWorlds answers GET /worlds: the eight worlds with their skill counts.
func (s *Server) ListWorlds(ctx context.Context, req api.ListWorldsRequestObject) (api.ListWorldsResponseObject, error) {
	if _, err := plCaller(ctx); err != nil {
		return nil, err
	}
	g := s.game()
	en := gameLang(req.Params.AcceptLanguage, "fr-CA") == "en"
	items := make([]api.World, 0, len(feedWorlds))
	for _, w := range feedWorlds {
		name, hook := w.FR, w.HookFR
		if en {
			name, hook = w.EN, w.HookEN
		}
		items = append(items, api.World{
			Id: w.ID, Name: name, Tagline: name, Hook: &hook, ImageUrl: "", AccentColor: w.Color,
			SkillCount: g.graph.WorldSkillCount(string(w.ID)),
		})
	}
	return api.ListWorlds200JSONResponse{Items: items}, nil
}

// ListSkills answers GET /skills with the optional filters world, grade and implemented.
func (s *Server) ListSkills(ctx context.Context, req api.ListSkillsRequestObject) (api.ListSkillsResponseObject, error) {
	if _, err := plCaller(ctx); err != nil {
		return nil, err
	}
	g := s.game()
	items := []api.Skill{} // never nil: JSON gives [] not null
	for _, id := range g.graph.All() {
		sk, _ := g.graph.Get(id)
		if req.Params.World != nil && !slices.Contains(sk.Worlds, string(*req.Params.World)) {
			continue
		}
		if req.Params.Grade != nil && (sk.YearIntro > *req.Params.Grade || sk.YearMastery < *req.Params.Grade) {
			continue
		}
		if req.Params.Implemented != nil && *req.Params.Implemented != sk.Implementation {
			continue
		}
		worlds := make([]api.WorldId, 0, len(sk.Worlds))
		for _, w := range sk.Worlds {
			worlds = append(worlds, api.WorldId(w))
		}
		prereq := sk.Prerequisites
		if prereq == nil {
			prereq = []string{}
		}
		items = append(items, api.Skill{
			Id: sk.ID, Label: sk.Label, RefPda: sk.RefPDA, Section: sk.Section, Cycle: sk.Cycle,
			GradeIntro: sk.YearIntro, GradeMastery: sk.YearMastery, Implemented: sk.Implementation,
			Prerequisites: prereq, Worlds: &worlds,
		})
	}
	return api.ListSkills200JSONResponse{Items: items}, nil
}

// journeyCols is the winding column pattern of the path (a slot is {row, col}).
var journeyCols = []int{2, 3, 2, 1, 2, 3}

// skillState maps a profile to the contract's node state for one skill.
func (g *gameState) skillState(prof *engine.Profile, id string, mastered map[string]bool, due map[string]bool) api.NodeState {
	_, seen := prof.States[id]
	switch {
	case due[id]:
		return api.NodeStateNeedsReview
	case mastered[id]:
		return api.NodeStateMastered
	case seen:
		return api.NodeStateInProgress
	case g.graph.PrereqsMet(id, mastered, true):
		return api.NodeStateAvailable
	}
	return api.NodeStateLocked
}

// GetJourney answers GET /children/{childId}/journey?world=...: the path of chapters with their state.
func (s *Server) GetJourney(ctx context.Context, req api.GetJourneyRequestObject) (api.GetJourneyResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), false)
	if err != nil {
		return nil, err
	}
	g := s.game()
	world := string(req.Params.World)
	if len(g.graph.ByWorld(world)) == 0 {
		return nil, httpapi.Validation(plField("world", "unknown world"))
	}
	lang := gameLang(req.Params.AcceptLanguage, c.Locale)
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
	ids := g.graph.ByWorld(world)
	slices.SortStableFunc(ids, func(a, b string) int { // earliest school year first, then id
		sa, _ := g.graph.Get(a)
		sb, _ := g.graph.Get(b)
		return sa.YearIntro - sb.YearIntro
	})
	nodes := make([]api.JourneyNode, 0, len(ids))
	for i, id := range ids {
		n := api.JourneyNode{SkillId: id, Label: g.skillLabel(id, lang), State: g.skillState(prof, id, mastered, due)}
		n.Position.Row, n.Position.Col = i, journeyCols[i%len(journeyCols)]
		level := 0
		if e, ok := prof.States[id]; ok {
			level = e.Reviews
		}
		n.Level = &level
		if n.State == api.NodeStateMastered || n.State == api.NodeStateNeedsReview {
			if t, ok := prof.NextReview(id, now); ok {
				n.NextReviewAt = &t
			}
		}
		nodes = append(nodes, n)
	}
	out := api.Journey{World: req.Params.World, Nodes: nodes}
	if sel, err := engine.Choose(g.graph, prof, world, gradeOf(c), engine.NewRNG(g.seed()), now); err == nil {
		out.CurrentSkillId = &sel.SkillID
	}
	return api.GetJourney200JSONResponse(out), nil
}
