// Package assetstore keeps reusable images (photos, illustrations, sprites) in SQLite with their licence,
// tags and bytes, picks the right one for a question from criteria, and builds the MediaRef the API sends.
//
// Go for Spring people: Store is a @Repository. Images are rows of three tables (assets, asset_tags,
// asset_variants); the bytes are BLOBs, so one database file is one backup. An asset id is a hash of the
// image, so an id never changes meaning and its URL can be cached forever (Cache-Control immutable).
package assetstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

// tsLayout is the timestamp text stored in the database.
const tsLayout = "2006-01-02T15:04:05.000Z"

// ErrNotFound is returned for an unknown or not publicly visible asset.
var ErrNotFound = errors.New("assetstore: not found")

// Store is the asset repository.
type Store struct {
	DB  *store.Store
	Now func() time.Time
}

// New builds a Store.
func New(db *store.Store) *Store {
	return &Store{DB: db, Now: func() time.Time { return time.Now().UTC() }}
}

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Asset is one stored image (without bytes).
type Asset struct {
	ID, Alias, Kind, WorldID, GradeBand, Mood, DominantColor, LQIP string
	Width, Height                                                  int
	AltFR, AltEN                                                   string
	SourceName, SourceURL, Creator, CreatorURL                     string
	Licence, LicenceURL, AttributionText                           string
	AttributionRequired, PeopleFree, BrandFree                     bool
	ReviewStatus, ReviewedBy                                       string
	Tags                                                           map[string][]string // facet -> values
	Widths                                                         []int               // available variant widths, ascending
}

// Alt returns the alternative text in the language.
func (a Asset) Alt(lang string) string {
	if lang == "en" {
		return a.AltEN
	}
	return a.AltFR
}

// Public reports whether the asset may be shown to children (approved and safe).
func (a Asset) Public() bool { return a.ReviewStatus == "approved" && a.PeopleFree && a.BrandFree }

const assetCols = `id, COALESCE(alias,''), kind, COALESCE(world_id,''), grade_band, COALESCE(mood,''), dominant_color, lqip, width, height, alt_fr, alt_en,
	source_name, source_url, creator, COALESCE(creator_url,''), licence, licence_url, attribution_text, attribution_required, people_free, brand_free,
	review_status, COALESCE(reviewed_by,'')`

// scanAsset reads one row selected with assetCols.
func scanAsset(row interface{ Scan(...any) error }) (Asset, error) {
	var a Asset
	var req, ppl, brand int
	err := row.Scan(&a.ID, &a.Alias, &a.Kind, &a.WorldID, &a.GradeBand, &a.Mood, &a.DominantColor, &a.LQIP, &a.Width, &a.Height, &a.AltFR, &a.AltEN,
		&a.SourceName, &a.SourceURL, &a.Creator, &a.CreatorURL, &a.Licence, &a.LicenceURL, &a.AttributionText, &req, &ppl, &brand, &a.ReviewStatus, &a.ReviewedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	a.AttributionRequired, a.PeopleFree, a.BrandFree = req == 1, ppl == 1, brand == 1
	return a, err
}

// Get reads one asset with its tags and variant widths.
func (s *Store) Get(ctx context.Context, q Querier, id string) (Asset, error) {
	a, err := scanAsset(q.QueryRowContext(ctx, `SELECT `+assetCols+` FROM assets WHERE id = ?`, id))
	if err != nil {
		return Asset{}, err
	}
	if err := s.fill(ctx, q, []*Asset{&a}); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// fill loads tags and variant widths for the assets (two queries for any number of assets).
func (s *Store) fill(ctx context.Context, q Querier, list []*Asset) error {
	if len(list) == 0 {
		return nil
	}
	byID := map[string]*Asset{}
	args := make([]any, len(list))
	for i, a := range list {
		byID[a.ID] = a
		args[i] = a.ID
		a.Tags = map[string][]string{}
	}
	in := "(" + strings.TrimSuffix(strings.Repeat("?,", len(list)), ",") + ")"
	rows, err := q.QueryContext(ctx, `SELECT asset_id, facet, value FROM asset_tags WHERE asset_id IN `+in+` ORDER BY facet, value`, args...)
	if err != nil {
		return fmt.Errorf("read tags: %w", err)
	}
	for rows.Next() {
		var id, facet, val string
		if err := rows.Scan(&id, &facet, &val); err != nil {
			_ = rows.Close()
			return err
		}
		byID[id].Tags[facet] = append(byID[id].Tags[facet], val)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT asset_id, w FROM asset_variants WHERE asset_id IN `+in+` AND format = 'webp' ORDER BY w`, args...)
	if err != nil {
		return fmt.Errorf("read variants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var w int
		if err := rows.Scan(&id, &w); err != nil {
			return err
		}
		byID[id].Widths = append(byID[id].Widths, w)
	}
	return rows.Err()
}

// Variant returns the bytes of one width (0 = the widest) of a publicly visible asset, and its ETag.
func (s *Store) Variant(ctx context.Context, id string, w int) (data []byte, etag string, err error) {
	var gotW int
	var q string
	args := []any{id}
	if w > 0 {
		q = `SELECT v.bytes, v.w FROM asset_variants v JOIN assets a ON a.id = v.asset_id
			WHERE v.asset_id = ? AND v.w = ? AND v.format = 'webp' AND a.review_status = 'approved' AND a.people_free = 1 AND a.brand_free = 1`
		args = append(args, w)
	} else {
		q = `SELECT v.bytes, v.w FROM asset_variants v JOIN assets a ON a.id = v.asset_id
			WHERE v.asset_id = ? AND v.format = 'webp' AND a.review_status = 'approved' AND a.people_free = 1 AND a.brand_free = 1
			ORDER BY v.w DESC LIMIT 1`
	}
	err = s.DB.R.QueryRowContext(ctx, q, args...).Scan(&data, &gotW)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("read asset %s: %w", id, err)
	}
	return data, `"` + id + "-" + strconv.Itoa(gotW) + `"`, nil
}

// SrcSet is one responsive image candidate.
type SrcSet struct {
	W   int    `json:"w"`
	URL string `json:"url"`
}

// Credit is the attribution line of a MediaRef.
type Credit struct {
	Text       string `json:"text"`
	SourceURL  string `json:"source_url,omitempty"`
	Licence    string `json:"licence"`
	LicenceURL string `json:"licence_url,omitempty"`
}

// MediaRef is the picture the API sends to the client (docs/CONTENT_PLAN.md section 4.5).
type MediaRef struct {
	ID     string   `json:"id"`
	Alt    string   `json:"alt"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Color  string   `json:"color"`
	LQIP   string   `json:"lqip"`
	SrcSet []SrcSet `json:"srcset"`
	Credit *Credit  `json:"credit,omitempty"`
}

// Ref builds the MediaRef of an asset. baseURL is the public path of the asset route ("/assets").
func (a Asset) Ref(lang, baseURL string) MediaRef {
	m := MediaRef{ID: a.ID, Alt: a.Alt(lang), Width: a.Width, Height: a.Height, Color: a.DominantColor, LQIP: a.LQIP, SrcSet: []SrcSet{}}
	for _, w := range a.Widths {
		m.SrcSet = append(m.SrcSet, SrcSet{W: w, URL: strings.TrimRight(baseURL, "/") + "/" + a.ID + "-" + strconv.Itoa(w) + ".webp"})
	}
	if a.AttributionRequired {
		m.Credit = &Credit{Text: a.AttributionText, SourceURL: a.SourceURL, Licence: a.Licence, LicenceURL: a.LicenceURL}
	}
	return m
}

// Credits lists every approved asset whose licence asks for attribution, for the credits page.
func (s *Store) Credits(ctx context.Context) ([]Asset, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+assetCols+` FROM assets WHERE review_status = 'approved' AND people_free = 1 AND brand_free = 1
		AND attribution_required = 1 ORDER BY creator, id`)
	if err != nil {
		return nil, fmt.Errorf("list credits: %w", err)
	}
	var list []Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, a)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	ptrs := make([]*Asset, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	return list, s.fill(ctx, s.DB.R, ptrs)
}

// Filter selects assets for the admin media screen and the credits.
type Filter struct {
	Kind, World, Status, Licence, Text string
	Facet, Value                       string
	Page, PageSize                     int
}

// List returns a page of assets (with tags and widths) and the total matches.
func (s *Store) List(ctx context.Context, f Filter) ([]Asset, int, error) {
	var conds []string
	var args []any
	add := func(c string, a ...any) { conds, args = append(conds, c), append(args, a...) }
	if f.Kind != "" {
		add("kind = ?", f.Kind)
	}
	if f.World != "" {
		add("world_id = ?", f.World)
	}
	if f.Status != "" {
		add("review_status = ?", f.Status)
	}
	if f.Licence != "" {
		add("licence = ?", f.Licence)
	}
	if f.Facet != "" && f.Value != "" {
		add("EXISTS (SELECT 1 FROM asset_tags t WHERE t.asset_id = assets.id AND t.facet = ? AND t.value = ?)", f.Facet, f.Value)
	}
	if f.Text != "" {
		add("(instr(lower(alt_fr), lower(?)) > 0 OR instr(lower(alt_en), lower(?)) > 0 OR instr(lower(creator), lower(?)) > 0 OR instr(id, ?) > 0)", f.Text, f.Text, f.Text, strings.ToLower(f.Text))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM assets`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count assets: %w", err)
	}
	size := min(100, max(1, f.PageSize))
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+assetCols+` FROM assets`+where+` ORDER BY id LIMIT ? OFFSET ?`, append(args, size, max(0, f.Page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list assets: %w", err)
	}
	list := []Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		list = append(list, a)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	ptrs := make([]*Asset, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	return list, total, s.fill(ctx, s.DB.R, ptrs)
}

// Patch holds the fields an admin may change on an asset. Nil means "leave as is".
type Patch struct {
	AltFR, AltEN          *string
	ReviewStatus          *string
	ReviewedBy            string
	PeopleFree, BrandFree *bool
	Tags                  map[string][]string // replaces all tags when non nil
}

// Apply updates an asset inside the caller's transaction.
func (s *Store) Apply(ctx context.Context, tx *sql.Tx, id string, p Patch) error {
	cur, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if p.AltFR != nil {
		cur.AltFR = *p.AltFR
	}
	if p.AltEN != nil {
		cur.AltEN = *p.AltEN
	}
	if p.PeopleFree != nil {
		cur.PeopleFree = *p.PeopleFree
	}
	if p.BrandFree != nil {
		cur.BrandFree = *p.BrandFree
	}
	if p.ReviewStatus != nil {
		cur.ReviewStatus = *p.ReviewStatus
	}
	if cur.ReviewStatus == "approved" && (!cur.PeopleFree || !cur.BrandFree || strings.TrimSpace(cur.AltFR) == "" || strings.TrimSpace(cur.AltEN) == "") {
		return fmt.Errorf("an asset can only be approved with alt text in both languages and people_free and brand_free set")
	}
	if cur.ReviewStatus == "approved" && cur.Licence == "PEXELS" && cur.CreatorURL == "" {
		return fmt.Errorf("a Pexels photo needs creator_url before approval")
	}
	reviewedAt := s.Now().UTC().Format(tsLayout)
	if _, err := tx.ExecContext(ctx, `UPDATE assets SET alt_fr=?, alt_en=?, people_free=?, brand_free=?, review_status=?, reviewed_by=CASE WHEN ? <> '' THEN ? ELSE reviewed_by END,
		reviewed_at=CASE WHEN ? <> '' THEN ? ELSE reviewed_at END WHERE id=?`,
		cur.AltFR, cur.AltEN, b2i(cur.PeopleFree), b2i(cur.BrandFree), cur.ReviewStatus, p.ReviewedBy, p.ReviewedBy, p.ReviewedBy, reviewedAt, id); err != nil {
		return fmt.Errorf("update asset: %w", err)
	}
	if p.Tags != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id = ?`, id); err != nil {
			return err
		}
		for facet, vals := range p.Tags {
			for _, v := range vals {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO asset_tags (asset_id, facet, value) VALUES (?,?,?)`, id, facet, v); err != nil {
					return fmt.Errorf("write tag %s=%s: %w", facet, v, err)
				}
			}
		}
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scoreOf rates an asset against criteria (plan 4.4): object 5, topic 4, world 3, setting 2, mood 2, exact grade band 1.
// hits counts the content dimensions (object, topic, world, setting, mood) that matched.
func scoreOf(a Asset, c content.Criteria, band string) (score, hits int) {
	has := func(facet, v string) bool { return v != "" && slices.Contains(a.Tags[facet], v) }
	if has("object", c.Object) {
		score, hits = score+5, hits+1
	}
	if has("topic", c.Topic) {
		score, hits = score+4, hits+1
	}
	if c.World != "" && (a.WorldID == c.World || has("world", c.World)) {
		score, hits = score+3, hits+1
	}
	if has("setting", c.Setting) {
		score, hits = score+2, hits+1
	}
	if c.Mood != "" && (a.Mood == c.Mood || has("mood", c.Mood)) {
		score, hits = score+2, hits+1
	}
	if band != "" && a.GradeBand == band {
		score++
	}
	return score, hits
}

// rendezvous is the stable pseudo random weight of an asset for one (child, question).
func rendezvous(childID, questionID, assetID string) uint64 {
	sum := sha256.Sum256([]byte(childID + "|" + questionID + "|" + assetID))
	return binary.BigEndian.Uint64(sum[:8])
}

// GradeBand maps a grade to the asset band.
func GradeBand(grade int) string {
	switch {
	case grade <= 2:
		return "1-2"
	case grade <= 4:
		return "3-4"
	}
	return "5-6"
}

// Resolve picks the asset for criteria. The same (child, question) always gets the same asset; an empty
// childID (admin preview) gives the best asset. It returns ErrNotFound when nothing fits, after trying to
// relax mood, then setting, then object, then the world hero (the caller draws a gradient when still empty).
func (s *Store) Resolve(ctx context.Context, q Querier, c content.Criteria, childID, questionID, band string) (Asset, error) {
	kind := c.Kind
	rows, err := q.QueryContext(ctx, `SELECT `+assetCols+` FROM assets WHERE review_status = 'approved' AND people_free = 1 AND brand_free = 1
		AND (? = '' OR kind = ?) AND grade_band IN ('all', ?)`, kind, kind, band)
	if err != nil {
		return Asset{}, fmt.Errorf("list candidates: %w", err)
	}
	var cands []Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			_ = rows.Close()
			return Asset{}, err
		}
		cands = append(cands, a)
	}
	if err := rows.Close(); err != nil {
		return Asset{}, err
	}
	ptrs := make([]*Asset, len(cands))
	for i := range cands {
		ptrs[i] = &cands[i]
	}
	if err := s.fill(ctx, q, ptrs); err != nil {
		return Asset{}, err
	}
	relax := []func(content.Criteria) content.Criteria{
		func(x content.Criteria) content.Criteria { return x },
		func(x content.Criteria) content.Criteria { x.Mood = ""; return x },
		func(x content.Criteria) content.Criteria { x.Mood, x.Setting = "", ""; return x },
		func(x content.Criteria) content.Criteria { x.Mood, x.Setting, x.Object = "", "", ""; return x },
	}
	for _, step := range relax {
		cr := step(c)
		best := -1
		type scored struct {
			a     Asset
			score int
		}
		var pool []scored
		for _, a := range cands {
			sc, hits := scoreOf(a, cr, band)
			if hits == 0 {
				continue // nothing in common with the request
			}
			pool = append(pool, scored{a, sc})
			best = max(best, sc)
		}
		pool = slices.DeleteFunc(pool, func(x scored) bool { return x.score < best-2 })
		if len(pool) == 0 {
			continue
		}
		pick := slices.MaxFunc(pool, func(x, y scored) int {
			wx, wy := rendezvous(childID, questionID, x.a.ID), rendezvous(childID, questionID, y.a.ID)
			switch {
			case wx > wy:
				return 1
			case wx < wy:
				return -1
			}
			return strings.Compare(y.a.ID, x.a.ID) // equal weights: smallest id wins, deterministic
		})
		return pick.a, nil
	}
	if c.World != "" { // fall back to the hero of the world
		var hero sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT hero_asset_id FROM worlds WHERE id = ?`, c.World).Scan(&hero); err == nil && hero.Valid {
			if a, err := s.Get(ctx, q, hero.String); err == nil && a.Public() {
				return a, nil
			}
		}
	}
	return Asset{}, ErrNotFound
}

// MediaFor resolves the media slots of a question (explicit assets first, criteria next) into MediaRefs.
// Slots that cannot be resolved are skipped: the client then draws a gradient from the world colour.
func (s *Store) MediaFor(ctx context.Context, q Querier, qu *content.Question, childID, lang, baseURL string) []MediaRefWithRole {
	out := []MediaRefWithRole{}
	band := GradeBand(qu.Grade)
	for _, m := range qu.Media {
		var a Asset
		var err error
		switch {
		case m.Asset != "":
			var id string
			if e := q.QueryRowContext(ctx, `SELECT id FROM assets WHERE id = ? OR alias = ?`, m.Asset, m.Asset).Scan(&id); e == nil {
				a, err = s.Get(ctx, q, id)
				if err == nil && !a.Public() {
					err = ErrNotFound
				}
			} else {
				err = ErrNotFound
			}
		case m.Criteria != nil:
			a, err = s.Resolve(ctx, q, *m.Criteria, childID, qu.ID, band)
		default:
			continue
		}
		if err != nil {
			continue
		}
		out = append(out, MediaRefWithRole{Role: m.Role, MediaRef: a.Ref(lang, baseURL)})
	}
	return out
}

// MediaRefWithRole is a MediaRef and the slot it fills (scene, object, visual, choice:<id>).
type MediaRefWithRole struct {
	Role string `json:"role"`
	MediaRef
}

// WorldHero returns the hero asset of a world, if one is approved.
func (s *Store) WorldHero(ctx context.Context, q Querier, world string) (Asset, bool) {
	var hero sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT hero_asset_id FROM worlds WHERE id = ?`, world).Scan(&hero); err != nil || !hero.Valid {
		return Asset{}, false
	}
	a, err := s.Get(ctx, q, hero.String)
	return a, err == nil && a.Public()
}

// SceneAssets returns up to n approved scene photos tagged with the world, in a stable order.
func (s *Store) SceneAssets(ctx context.Context, q Querier, world string, n int) []Asset {
	rows, err := q.QueryContext(ctx, `SELECT `+assetCols+` FROM assets a WHERE review_status = 'approved' AND people_free = 1 AND brand_free = 1
		AND kind = 'photo' AND (world_id = ? OR EXISTS (SELECT 1 FROM asset_tags t WHERE t.asset_id = a.id AND t.facet = 'world' AND t.value = ?))
		AND id NOT IN (SELECT COALESCE(hero_asset_id,'') FROM worlds WHERE id = ?) ORDER BY id LIMIT ?`, world, world, world, n)
	if err != nil {
		return nil
	}
	var list []Asset
	for rows.Next() {
		if a, err := scanAsset(rows); err == nil {
			list = append(list, a)
		}
	}
	_ = rows.Close()
	ptrs := make([]*Asset, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	if s.fill(ctx, q, ptrs) != nil {
		return nil
	}
	return list
}

// JSONTags renders tags for logs and audit details.
func JSONTags(t map[string][]string) string {
	b, _ := json.Marshal(t) // a map of string slices always marshals
	return string(b)
}
