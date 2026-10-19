package app

import (
	"context"
	"slices"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/contentdb"
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

// ListWorlds answers GET /worlds: the twenty worlds (from the database) with art and skill counts.
func (s *Server) ListWorlds(ctx context.Context, req api.ListWorldsRequestObject) (api.ListWorldsResponseObject, error) {
	if _, err := plCaller(ctx); err != nil {
		return nil, err
	}
	lang := gameLang(req.Params.AcceptLanguage, "fr-CA")
	items, err := s.worldViews(ctx, lang)
	if err != nil {
		return nil, err
	}
	return api.ListWorlds200JSONResponse{Items: items}, nil
}

// ListSkills answers GET /skills with the optional filters world, grade and implemented.
func (s *Server) ListSkills(ctx context.Context, req api.ListSkillsRequestObject) (api.ListSkillsResponseObject, error) {
	if _, err := plCaller(ctx); err != nil {
		return nil, err
	}
	g := s.game()
	s.syncBank(ctx)
	items := []api.Skill{} // never nil: JSON gives [] not null
	for _, id := range g.graph.All() {
		sk, _ := g.graph.Get(id)
		skillWorlds := g.graph.WorldsOf(id)
		if req.Params.World != nil && !slices.Contains(skillWorlds, string(*req.Params.World)) {
			continue
		}
		if req.Params.Grade != nil && (sk.YearIntro > *req.Params.Grade || sk.YearMastery < *req.Params.Grade) {
			continue
		}
		if req.Params.Implemented != nil && *req.Params.Implemented != g.graph.IsImplemented(id) {
			continue
		}
		worlds := make([]api.WorldId, 0, len(skillWorlds))
		for _, w := range skillWorlds {
			worlds = append(worlds, api.WorldId(w))
		}
		prereq := sk.Prerequisites
		if prereq == nil {
			prereq = []string{}
		}
		items = append(items, api.Skill{
			Id: sk.ID, Label: sk.Label, RefPda: sk.RefPDA, Section: sk.Section, Cycle: sk.Cycle,
			GradeIntro: sk.YearIntro, GradeMastery: sk.YearMastery, Implemented: g.graph.IsImplemented(id),
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
// A node is a quest of the world that can be played (its skill has a generator or enough published bank
// questions), then the generator skills the curriculum tags with the world that have no quest.
func (s *Server) GetJourney(ctx context.Context, req api.GetJourneyRequestObject) (api.GetJourneyResponseObject, error) {
	c, _, err := s.gameChild(ctx, plUID(req.ChildId), false)
	if err != nil {
		return nil, err
	}
	g := s.game()
	world := string(req.Params.World)
	if ok, werr := s.worldExists(ctx, world); werr != nil {
		return nil, werr
	} else if !ok {
		return nil, httpapi.Validation(plField("world", "unknown world"))
	}
	s.syncBank(ctx)
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
	quests, err := s.Content.Quests(ctx, s.Store.R, world)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list quests", err)
	}
	type node struct {
		skill string
		quest *contentdb.QuestRow
	}
	var order []node
	covered := map[string]bool{}
	for i := range quests {
		if g.graph.PlayableIn(world, quests[i].Skill) {
			order = append(order, node{quests[i].Skill, &quests[i]})
			covered[quests[i].Skill] = true
		}
	}
	rest := g.graph.ByWorld(world)
	slices.SortStableFunc(rest, func(a, b string) int { // earliest school year first, then id
		sa, _ := g.graph.Get(a)
		sb, _ := g.graph.Get(b)
		return sa.YearIntro - sb.YearIntro
	})
	for _, id := range rest {
		if !covered[id] {
			order = append(order, node{skill: id})
		}
	}
	base := s.Config.AssetBaseURL
	if base == "" {
		base = "/assets"
	}
	nodes := make([]api.JourneyNode, 0, len(order))
	for i, nd := range order {
		label := g.skillLabel(nd.skill, lang)
		if nd.quest != nil {
			label = nd.quest.Name.Get(lang)
		}
		n := api.JourneyNode{SkillId: nd.skill, Label: label, State: g.skillState(prof, nd.skill, mastered, due)}
		n.Position.Row, n.Position.Col = i, journeyCols[i%len(journeyCols)]
		if nd.quest != nil {
			n.Quest = &api.JourneyQuest{Id: nd.quest.ID, Name: label, Goal: nd.quest.Goal.Get(lang)}
			if nd.quest.IconAssetID != "" {
				if a, aerr := s.Assets.Get(ctx, s.Store.R, nd.quest.IconAssetID); aerr == nil && a.Public() {
					if m, merr := toAPIMedia(a.Ref(lang, base)); merr == nil {
						n.Media = &m
					}
				}
			}
		}
		level := 0
		if e, ok := prof.States[nd.skill]; ok {
			level = e.Reviews
		}
		n.Level = &level
		if n.State == api.NodeStateMastered || n.State == api.NodeStateNeedsReview {
			if t, ok := prof.NextReview(nd.skill, now); ok {
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
