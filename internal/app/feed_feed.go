// Operations of the endless discovery feed: getFeed, reactToFeedItem, answerFeedPuzzle.
//
// Go for Spring people: item number n is a pure function of n (see feedItem), so nothing is stored per item;
// only the child's private reactions and puzzle outcomes are rows. Methods with a (s *Server) receiver are
// Server's methods; they replace the 501 stubs of the embedded Unimplemented (like @Override).
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/auth"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// feedKinds is the repeating rhythm of the feed: item n has kind feedKinds[n % 6].
var feedKinds = []api.FeedItemKind{
	api.FeedItemKindStoryShort, api.FeedItemKindFunFact, api.FeedItemKindPuzzle,
	api.FeedItemKindChallenge, api.FeedItemKindStoryShort, api.FeedItemKindPuzzle,
}

// feedIDRe matches the only item ids the server ever hands out, e.g. "feed-12".
var feedIDRe = regexp.MustCompile(`^feed-(\d+)$`)

// feedPuzzle holds the numbers behind puzzle n. Only Choices ever leave the server, never Correct.
type feedPuzzle struct {
	A, B, Correct, Typical int
	Choices                []int
}

// puzzleFor builds the multiplication puzzle for item n (port of puzzleFor in mock/lib/feed.mjs).
func puzzleFor(n int) feedPuzzle {
	a := 2 + (n*7)%8                // first factor, 2..9
	b := 3 + (n*5)%7                // second factor, 3..9
	correct := a * b                // the right product
	typical := a + b                // the typical mistake: adding instead of multiplying
	pool := []int{correct, typical} // answer candidates, wrong ones follow
	for _, v := range []int{a * (b + 1), (a + 1) * b, a * (b - 1)} {
		if v != correct && v != typical { // keep only distinct wrong values
			pool = append(pool, v)
		}
	}
	if len(pool) > 4 { // at most four choices
		pool = pool[:4]
	}
	type keyed struct{ v, k int } // a value and its shuffle key
	ks := make([]keyed, len(pool))
	for i, v := range pool {
		ks[i] = keyed{v, (i*37 + n*11) % 17} // deterministic "shuffle" key per position
	}
	sort.SliceStable(ks, func(i, j int) bool { return ks[i].k < ks[j].k }) // stable like JS Array.sort
	choices := make([]int, len(ks))
	for i, x := range ks {
		choices[i] = x.v
	}
	return feedPuzzle{A: a, B: b, Correct: correct, Typical: typical, Choices: choices}
}

// feedFill replaces {n}, {W}, {dW}, {iW} in t and upper-cases the first letter.
func feedFill(t string, slots map[string]string) string {
	for k, v := range slots { // replace every slot name with its value
		t = strings.ReplaceAll(t, "{"+k+"}", v)
	}
	r, size := utf8.DecodeRuneInString(t) // first character, may be multi-byte
	if size == 0 {
		return t
	}
	return string(unicode.ToUpper(r)) + t[size:]
}

// feedItem builds item n for a child (port of feedItem in mock/lib/feed.mjs). Deterministic: same input, same item.
func feedItem(n int, en bool, liked, saved bool, name string, world api.WorldId, worldName string) api.FeedItem {
	l := 0 // index of the language inside the content rows: 0 fr, 1 en
	if en {
		l = 1
	}
	kind := feedKinds[n%len(feedKinds)]
	cycle := n / len(feedKinds)
	j := cycle // position inside the bank of this kind
	switch {
	case kind == api.FeedItemKindStoryShort && n%6 == 4:
		j = cycle*2 + 1
	case kind == api.FeedItemKindStoryShort:
		j = cycle * 2
	case kind == api.FeedItemKindPuzzle && n%6 == 5:
		j = cycle*2 + 1
	case kind == api.FeedItemKindPuzzle:
		j = cycle * 2
	}
	lumi := kind == api.FeedItemKindPuzzle // Lumi writes puzzles, Pip everything else
	opens, closes := feedPipOpen, feedPipClose
	if lumi {
		opens, closes = feedLumiOpen, feedLumiClose
	}
	var seed [4]string // title fr, text fr, title en, text en (puzzles only use the first two slots)
	var lap int        // how many times the bank was already used
	if lumi {
		lap = j / len(feedPuzzles)
		p := feedPuzzles[j%len(feedPuzzles)]
		seed = [4]string{p[0], p[0], p[1], p[1]}
	} else {
		bank := feedChallenges
		if kind == api.FeedItemKindStoryShort {
			bank = feedStories
		} else if kind == api.FeedItemKindFunFact {
			bank = feedFacts
		}
		lap = j / len(bank)
		seed = bank[j%len(bank)]
	}
	article := "le monde " + worldName // slots by language, as in the mock
	dW, iW := "du monde "+worldName, "dans le monde "+worldName
	if en {
		article, dW, iW = "the "+worldName+" world", "of the "+worldName+" world", "in the "+worldName+" world"
	}
	slots := map[string]string{"n": name, "W": article, "dW": dW, "iW": iW}
	mid := seed[2*l+1] // the seed text in the right language
	if lumi {
		mid = seed[2*l] // puzzle intros are stored as (fr, en); seed[0] and seed[2] hold them
	}
	text := strings.Join([]string{
		feedFill(opens[(j+lap)%len(opens)][l], slots),
		feedFill(mid, slots),
		feedFill(closes[(j*3+lap)%len(closes)][l], slots),
	}, " ")
	item := api.FeedItem{
		Id: fmt.Sprintf("feed-%d", n), Kind: kind, World: world, AiGenerated: api.FeedItemAiGeneratedTrue,
		Liked: liked, Saved: saved, Text: text,
	}
	by := "numerix-seed" // honest label: seeded content, no LLM
	item.GeneratedBy = &by
	pip, lumiName, fox, owl := "Pip", "Lumi", "🦊", "🦉"
	item.Author = &struct {
		Emoji *string `json:"emoji,omitempty"`
		Name  *string `json:"name,omitempty"`
	}{Emoji: &fox, Name: &pip}
	if lumi {
		item.Author.Emoji, item.Author.Name = &owl, &lumiName
		item.Title = map[bool]string{true: "Lumi’s puzzle", false: "Le casse-tête de Lumi"}[en]
		p := puzzleFor(n)
		q := api.Question{MathText: fmt.Sprintf("%d × %d = ?", p.A, p.B)}
		for _, c := range p.Choices { // choices as numbers, never the answer flag
			var av api.AnswerValue
			_ = av.FromAnswerValue0(c) // an int always marshals
			q.Choices = append(q.Choices, av)
		}
		item.Puzzle = &q
		return item
	}
	item.Title = seed[2*l]
	if kind != api.FeedItemKindFunFact { // stories and challenges invite the child to play that world
		label := "Jouer à " + worldName
		if en {
			label = "Play " + worldName
		}
		item.Cta = &struct {
			Label   string       `json:"label"`
			SkillId *api.SkillId `json:"skill_id,omitempty"`
			World   api.WorldId  `json:"world"`
		}{Label: label, World: world}
	}
	return item
}

// feedConsented loads the caller's child and refuses (403 consent_missing) when no parent consent is active.
func (s *Server) feedConsented(ctx context.Context, childID string) (plChild, auth.Principal, error) {
	p, err := plCaller(ctx) // who is calling (Spring: SecurityContextHolder)
	if err != nil {
		return plChild{}, p, err
	}
	c, err := s.plOwnedChild(ctx, p, childID) // 403 for another child, 404 for another family
	if err != nil {
		return plChild{}, p, err
	}
	if !c.Consented {
		return plChild{}, p, httpapi.NewError(http.StatusForbidden, "consent_missing", "A parent must give consent before this child can play.")
	}
	return c, p, nil
}

// feedWorld picks the world of item n and its name in the right language.
func feedWorld(n int, en bool) (api.WorldId, string) {
	w := feedWorlds[n%len(feedWorlds)]
	if en {
		return w.ID, w.EN
	}
	return w.ID, w.FR
}

// GetFeed answers GET /children/{childId}/feed: a page of items starting at the cursor "i<N>".
func (s *Server) GetFeed(ctx context.Context, req api.GetFeedRequestObject) (api.GetFeedResponseObject, error) {
	c, _, err := s.feedConsented(ctx, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	en := plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA" // language of the texts
	start := 0                                                       // first item number of this page
	if req.Params.Cursor != nil {
		if v, err := strconv.Atoi(strings.TrimPrefix(*req.Params.Cursor, "i")); err == nil && v > 0 {
			start = v
		}
	}
	limit := 5 // contract default
	if req.Params.Limit != nil {
		limit = min(20, max(1, *req.Params.Limit)) // contract: 1..20
	}
	ids := make([]any, limit) // item ids of the page, for one IN query
	for k := range ids {
		ids[k] = fmt.Sprintf("feed-%d", start+k)
	}
	marks, err := s.feedReactionMap(ctx, c.ID, ids)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load reactions", err)
	}
	first := strings.Fields(c.DisplayName) // only the first name goes into the text
	name := c.DisplayName
	if len(first) > 0 {
		name = first[0]
	}
	items := make([]api.FeedItem, limit)
	for k := range items {
		w, wn := feedWorld(start+k, en)
		m := marks[ids[k].(string)] // zero value when the child never reacted
		items[k] = feedItem(start+k, en, m[0], m[1], name, w, wn)
	}
	return api.GetFeed200JSONResponse{Items: items, NextCursor: fmt.Sprintf("i%d", start+limit)}, nil
}

// feedReactionMap returns {itemID: [liked, saved]} for the given item ids of one child.
func (s *Server) feedReactionMap(ctx context.Context, childID string, ids []any) (map[string][2]bool, error) {
	q := `SELECT item_id, liked, saved FROM feed_reactions WHERE child_id = ? AND item_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	rows, err := s.Store.R.QueryContext(ctx, q, append([]any{childID}, ids...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // always release the connection (Spring: try-with-resources)
	out := map[string][2]bool{}
	for rows.Next() {
		var id string
		var l, sv int
		if err := rows.Scan(&id, &l, &sv); err != nil {
			return nil, err
		}
		out[id] = [2]bool{l != 0, sv != 0}
	}
	return out, rows.Err()
}

// ReactToFeedItem answers PUT /children/{childId}/feed/{itemId}/reaction: like/save, private to the child.
func (s *Server) ReactToFeedItem(ctx context.Context, req api.ReactToFeedItemRequestObject) (api.ReactToFeedItemResponseObject, error) {
	c, _, err := s.feedConsented(ctx, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpapi.Validation(plField("body", "required"))
	}
	if !feedIDRe.MatchString(req.ItemId) { // never store ids the server did not generate
		return nil, httpapi.ErrNotFound
	}
	now := time.Now().UTC().Format(plTSLayout)
	b := *req.Body
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error { // Spring: @Transactional
		if !b.Liked && !b.Saved { // nothing left to remember: delete the row
			_, e := tx.ExecContext(ctx, `DELETE FROM feed_reactions WHERE child_id = ? AND item_id = ?`, c.ID, req.ItemId)
			return e
		}
		likedAt := any(nil) // liked_at stays NULL unless liked
		if b.Liked {
			likedAt = now
		}
		// Upsert: keep the first liked_at while the like lasts, reset it when unliked.
		_, e := tx.ExecContext(ctx, `INSERT INTO feed_reactions (child_id, item_id, liked, saved, liked_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(child_id, item_id) DO UPDATE SET liked = excluded.liked, saved = excluded.saved,
				liked_at = CASE WHEN excluded.liked = 1 THEN COALESCE(feed_reactions.liked_at, excluded.liked_at) ELSE NULL END,
				updated_at = excluded.updated_at`, c.ID, req.ItemId, b2i(b.Liked), b2i(b.Saved), likedAt, now)
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "save reaction", err)
	}
	out := api.ReactToFeedItem200JSONResponse{Liked: b.Liked, Saved: b.Saved}
	if rw, err := s.settleChallenges(ctx, c); err != nil { // a like may complete feed_fan
		return nil, err
	} else if len(rw) > 0 {
		out.Rewards = &rw
	}
	return out, nil
}

// b2i turns a bool into 0/1 for SQLite.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// feedAnswerNumber reads the child's answer (a JSON number or numeric string) as a float; ok is false otherwise.
func feedAnswerNumber(v api.AnswerValue) (float64, bool) {
	raw, err := v.MarshalJSON() // the union keeps the raw JSON
	if err != nil {
		return 0, false
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil { // a JSON number
		return f, true
	}
	var str string
	if json.Unmarshal(raw, &str) == nil { // a JSON string such as "48" or "4,8"
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(str), ",", "."), 64)
		return f, err == nil
	}
	return 0, false
}

// AnswerFeedPuzzle answers POST /children/{childId}/feed/{itemId}/answer: graded by the same numbers that built the puzzle.
func (s *Server) AnswerFeedPuzzle(ctx context.Context, req api.AnswerFeedPuzzleRequestObject) (api.AnswerFeedPuzzleResponseObject, error) {
	c, _, err := s.feedConsented(ctx, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, httpapi.Validation(plField("answer", "required"))
	}
	m := feedIDRe.FindStringSubmatch(req.ItemId)
	if m == nil {
		return nil, httpapi.ErrNotFound
	}
	n, _ := strconv.Atoi(m[1])
	if feedKinds[n%len(feedKinds)] != api.FeedItemKindPuzzle { // only puzzles have a question
		return nil, httpapi.ErrNotFound
	}
	p := puzzleFor(n)
	given, ok := feedAnswerNumber(req.Body.Answer)
	en := plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA"
	var outcome api.Outcome
	switch {
	case ok && given == float64(p.Correct):
		outcome = api.Correct
	case ok && given == float64(p.Typical):
		outcome = api.ErreurTypique
	default:
		outcome = api.ErreurAleatoire
	}
	err = s.Store.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO feed_answers (child_id, item_id, outcome, created_at) VALUES (?, ?, ?, ?)`,
			c.ID, req.ItemId, string(outcome), time.Now().UTC().Format(plTSLayout))
		return e
	})
	if err != nil {
		return nil, s.plInternalErr(ctx, "save puzzle answer", err)
	}
	out := api.AnswerFeedPuzzle200JSONResponse{Outcome: outcome, Message: feedPuzzleMessage(outcome, en)}
	if rw, err := s.settleChallenges(ctx, c); err != nil { // a right answer may complete puzzle_pro
		return nil, err
	} else if len(rw) > 0 {
		out.Rewards = &rw
	}
	return out, nil
}

// feedPuzzleMessage is the short story line that replaces a score (no red X, no points).
func feedPuzzleMessage(o api.Outcome, en bool) string {
	switch {
	case o == api.Correct && en:
		return "The gate swings open and the hero walks through. Your math did that."
	case o == api.Correct:
		return "La porte s’ouvre et le héros passe. C’est ton calcul qui a fait ça."
	case o == api.ErreurTypique && en:
		return "The gate wobbles and a side path appears. Same numbers, another way to combine them."
	case o == api.ErreurTypique:
		return "La porte vacille et un petit chemin apparaît. Mêmes nombres, une autre façon de les combiner."
	case en:
		return "A breeze blows the gate shut, then a new lock appears. Another try is waiting."
	}
	return "Un coup de vent referme la porte, puis une nouvelle serrure apparaît. Un autre essai t’attend."
}
