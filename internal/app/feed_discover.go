// Operations of the Discover feed: getDiscover (200 curated photos, animations and videos in a private random order,
// puzzles interleaved) and getFeedMedia (the media files, with ETag and HTTP Range).
//
// Go for Spring people: nothing about a child's position is stored. The cursor holds the position; the order is a
// pure function of (child id, position), see internal/feedmedia/order.go. Reactions reuse feed_reactions, so likes
// and saves of media items and of puzzles go through the same endpoints.
package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/crc32"
	"net/http"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/feedmedia"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

const (
	discoverPageMax   = 12 // contract: 1..12, default 12
	discoverPuzzleGap = 8  // every 8th slot of the feed is a puzzle: positions 7, 15, 23, ...
)

// discoverCursor builds the opaque cursor for a position: base64url of "<pos>.<crc>". The checksum is bound to the
// child, so a cursor copied from another child or edited by hand is refused.
func discoverCursor(childID string, pos int) string {
	sum := crc32.ChecksumIEEE([]byte("discover:" + childID + ":" + strconv.Itoa(pos)))
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d.%08x", pos, sum)))
}

// parseDiscoverCursor reads a cursor back; ok is false when it is not one this server issued for this child.
func parseDiscoverCursor(childID, cursor string) (pos int, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, false
	}
	num, _, found := strings.Cut(string(raw), ".")
	if !found {
		return 0, false
	}
	pos, err = strconv.Atoi(num)
	if err != nil || pos < 0 || pos > 10_000_000 || discoverCursor(childID, pos) != cursor {
		return 0, false
	}
	return pos, true
}

// discoverSlot is what one position of the feed shows.
type discoverSlot struct {
	pos    int
	puzzle int // puzzle item number n (feed-n) when media is nil
	media  *feedmedia.Item
}

// discoverMediaCounter is how many media slots come before position p (puzzles do not count).
func discoverMediaCounter(p int) int { return p - p/discoverPuzzleGap }

// discoverPuzzleNumber maps the k-th puzzle slot to a feed item number whose kind is puzzle (feedKinds: 2 and 5 of 6).
func discoverPuzzleNumber(k int) int { return 6*(k/2) + 2 + 3*(k%2) }

// discoverSlots lays out `limit` slots from pos for a child whose eligible items are `items`.
func discoverSlots(childID string, pos, limit int, items []feedmedia.Item) []discoverSlot {
	kinds := make([]string, len(items))
	for i, it := range items {
		kinds[i] = it.Kind
	}
	seed := feedmedia.SeedOf(childID)
	out := make([]discoverSlot, limit)
	for k := range out {
		p := pos + k
		if p%discoverPuzzleGap == discoverPuzzleGap-1 {
			out[k] = discoverSlot{pos: p, puzzle: discoverPuzzleNumber(p / discoverPuzzleGap)}
			continue
		}
		idx, _ := feedmedia.At(seed, discoverMediaCounter(p), kinds)
		out[k] = discoverSlot{pos: p, media: &items[idx]}
	}
	return out
}

// discoverEligible keeps the items one year of stretch above the child's grade (an unknown grade counts as 6: all).
func discoverEligible(all []feedmedia.Item, grade int, fc appconfig.Feed) []feedmedia.Item {
	hidden := make(map[string]bool, len(fc.HiddenItemIDs))
	for _, id := range fc.HiddenItemIDs {
		hidden[id] = true
	}
	out := make([]feedmedia.Item, 0, len(all))
	for _, it := range all {
		if it.GradeMin <= grade+fc.MaxGradeStretch && !hidden[it.ID] {
			out = append(out, it)
		}
	}
	return out
}

// mediaID is the item id used for reactions.
func mediaID(slug string) string { return "m-" + slug }

// toAPIMedia converts a stored item to the contract shape, in one language, with URLs under the media base path.
func (s *Server) toAPIMedia(it feedmedia.Item, en bool) api.MediaItem {
	t := it.FR
	if en {
		t = it.EN
	}
	base := s.Config.MediaBaseURL
	if base == "" {
		base = "/feed-media"
	}
	url := func(f string) string { return base + "/" + f }
	opt := func(f string) *string {
		if f == "" {
			return nil
		}
		u := url(f)
		return &u
	}
	m := api.MediaItem{
		Id: it.ID, MediaKind: api.MediaItemMediaKind(it.Kind), Title: t.Title, Caption: t.Caption, Alt: t.Alt,
		World: api.WorldId(it.World), Topic: it.Topic, Tags: it.Tags, GradeMin: it.GradeMin,
		Src: url(it.Src), SrcSmall: opt(it.SrcSmall), Poster: opt(it.Poster), Width: it.Width, Height: it.Height, Lqip: it.LQIP,
		SourceUrl: it.SourceURL, Creator: it.Creator, Licence: it.Licence, Attribution: it.Attribution,
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	if it.DurationMS > 0 {
		d := it.DurationMS
		m.DurationMs = &d
	}
	if it.HasAudio {
		b := true
		m.HasAudio = &b
	}
	if it.LicenceURL != "" {
		u := it.LicenceURL
		m.LicenceUrl = &u
	}
	if it.MotionNote != "" {
		n := it.MotionNote
		m.MotionNote = &n
	}
	if t.Question != "" {
		m.Question = &struct {
			Answer string `json:"answer"`
			Prompt string `json:"prompt"`
		}{Answer: t.Answer, Prompt: t.Question}
	}
	if len(it.Captions) > 0 {
		cs := make([]struct {
			Label string `json:"label"`
			Lang  string `json:"lang"`
			Src   string `json:"src"`
		}, len(it.Captions))
		for i, c := range it.Captions {
			cs[i].Label, cs[i].Lang, cs[i].Src = c.Label, c.Lang, url(c.Src)
		}
		m.Captions = &cs
	}
	return m
}

// GetDiscover answers GET /children/{childId}/discover.
func (s *Server) GetDiscover(ctx context.Context, req api.GetDiscoverRequestObject) (api.GetDiscoverResponseObject, error) {
	c, _, err := s.feedConsented(ctx, plUID(req.ChildId))
	if err != nil {
		return nil, err
	}
	en := plLocaleOf(req.Params.AcceptLanguage, c.Locale) == "en-CA"
	pos := 0
	if req.Params.Cursor != nil && *req.Params.Cursor != "" {
		var ok bool
		if pos, ok = parseDiscoverCursor(c.ID, *req.Params.Cursor); !ok {
			return nil, httpapi.Validation(plField("cursor", "invalid"))
		}
	}
	limit := discoverPageMax
	if req.Params.Limit != nil {
		limit = min(discoverPageMax, max(1, *req.Params.Limit))
	}
	all, err := s.Feed.All(ctx)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load discover items", err)
	}
	fc := appconfig.Get[appconfig.Feed](ctx, s.Cfg, appconfig.NSFeed)
	if !fc.DiscoverEnabled { // the admin switched Discover off
		return nil, httpapi.NewError(http.StatusServiceUnavailable, "feed_empty", "The Discover feed is switched off.")
	}
	items := discoverEligible(all, gradeOf(c), fc)
	if len(items) == 0 {
		return nil, httpapi.NewError(http.StatusServiceUnavailable, "feed_empty", "The Discover feed has no items yet.")
	}
	slots := discoverSlots(c.ID, pos, limit, items)
	ids := make([]any, len(slots)) // item ids of the page, for one reactions query
	for i, sl := range slots {
		if sl.media != nil {
			ids[i] = mediaID(sl.media.ID)
		} else {
			ids[i] = fmt.Sprintf("feed-%d", sl.puzzle)
		}
	}
	marks, err := s.feedReactionMap(ctx, c.ID, ids)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load reactions", err)
	}
	first := strings.Fields(c.DisplayName) // only the first name goes into puzzle texts
	name := c.DisplayName
	if len(first) > 0 {
		name = first[0]
	}
	page := api.DiscoverPage{
		Items:      make([]api.DiscoverItem, len(slots)),
		NextCursor: discoverCursor(c.ID, pos+limit),
		Cycle:      discoverMediaCounter(pos) / len(items),
		CycleSize:  len(items),
	}
	for i, sl := range slots {
		id := ids[i].(string)
		m := marks[id]
		di := api.DiscoverItem{Id: id, Liked: m[0], Saved: m[1]}
		if sl.media != nil {
			di.Kind = api.DiscoverItemKind(sl.media.Kind)
			mi := s.toAPIMedia(*sl.media, en)
			di.Media = &mi
		} else {
			w, wn := feedWorld(sl.puzzle, en)
			fi := feedItem(sl.puzzle, en, m[0], m[1], name, w, wn)
			di.Kind, di.PuzzleItem = api.DiscoverItemKind("puzzle"), &fi
		}
		page.Items[i] = di
	}
	return api.GetDiscover200JSONResponse(page), nil
}

// feedMediaResponse adapts the file response to the generated interface (the generated types cannot express Range).
type feedMediaResponse struct{ feedmedia.Response }

// VisitGetFeedMediaResponse writes the answer (Spring: the body of a ResponseEntity<Resource>).
func (r feedMediaResponse) VisitGetFeedMediaResponse(w http.ResponseWriter) error { return r.Write(w) }

// GetFeedMedia answers GET /feed-media/{file}. Public: file names are content hashes of curated, people-free media.
func (s *Server) GetFeedMedia(ctx context.Context, req api.GetFeedMediaRequestObject) (api.GetFeedMediaResponseObject, error) {
	f, err := feedmedia.Open(s.Config.MediaDir, req.File)
	if err != nil {
		return nil, httpapi.ErrNotFound
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return feedMediaResponse{feedmedia.Response{
		File: f, IfNoneMatch: str(req.Params.IfNoneMatch), Range: str(req.Params.Range), IfRange: str(req.Params.IfRange),
	}}, nil
}

// isDiscoverID tells whether id is "m-<slug>" of a curated item that exists (a reaction is stored only for those).
func (s *Server) isDiscoverID(ctx context.Context, id string) bool {
	slug, ok := strings.CutPrefix(id, "m-")
	if !ok || s.Feed == nil {
		return false
	}
	all, err := s.Feed.All(ctx)
	if err != nil {
		return false
	}
	for _, it := range all {
		if it.ID == slug {
			return true
		}
	}
	return false
}
