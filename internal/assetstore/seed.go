package assetstore

// Seed loading of assets: seed/assets/assets.json (metadata) and seed/assets/out/*.webp (bytes), plus
// seed/assets/vocab.json (the controlled vocabulary). The files are produced by tools/assets (phase P3);
// the readers here are tolerant about small shape differences (bool as true or 1, tags as a map or a list,
// variants as file names or objects) but strict about the fields the database needs.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/internal/content"
)

// Flag is a boolean that also reads 0 and 1 from JSON.
type Flag bool

// UnmarshalJSON accepts true/false and 0/1.
func (f *Flag) UnmarshalJSON(b []byte) error {
	switch strings.TrimSpace(string(b)) {
	case "true", "1":
		*f = true
	case "false", "0", "null":
		*f = false
	default:
		return fmt.Errorf("expected a boolean, got %s", b)
	}
	return nil
}

// TagList reads tags written as {"topic":["a","b"]}, as [{"facet":"topic","value":"a"}] or as ["topic:a"].
type TagList map[string][]string

// UnmarshalJSON accepts the three shapes.
func (t *TagList) UnmarshalJSON(b []byte) error {
	out := TagList{}
	var asMap map[string][]string
	if json.Unmarshal(b, &asMap) == nil {
		*t = asMap
		return nil
	}
	var asObjs []struct{ Facet, Value string }
	if err := json.Unmarshal(b, &asObjs); err == nil && (len(asObjs) == 0 || asObjs[0].Facet != "") {
		for _, o := range asObjs {
			out[o.Facet] = append(out[o.Facet], o.Value)
		}
		*t = out
		return nil
	}
	var asStrs []string
	if err := json.Unmarshal(b, &asStrs); err != nil {
		return fmt.Errorf("tags must be an object, a list of {facet,value} or a list of \"facet:value\": %w", err)
	}
	for _, s := range asStrs {
		facet, val, ok := strings.Cut(s, ":")
		if !ok {
			return fmt.Errorf("tag %q must look like facet:value", s)
		}
		out[facet] = append(out[facet], val)
	}
	*t = out
	return nil
}

// Variant is one file of an asset: the width and the file name inside seed/assets/out.
type Variant struct {
	W    int    `json:"w"`
	File string `json:"file"`
}

var widthRe = regexp.MustCompile(`-(\d+)\.webp$`)

// UnmarshalJSON accepts "a_x-480.webp" (width read from the name) or {"w":480,"file":"..."}.
func (v *Variant) UnmarshalJSON(b []byte) error {
	var name string
	if json.Unmarshal(b, &name) == nil {
		m := widthRe.FindStringSubmatch(name)
		if m == nil {
			return fmt.Errorf("variant file %q must end with -<width>.webp", name)
		}
		v.W, _ = strconv.Atoi(m[1])
		v.File = name
		return nil
	}
	type plain Variant
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = Variant(p)
	return nil
}

// Doc is one entry of assets.json. Field names are the column names of table assets.
type Doc struct {
	ID                  string    `json:"id"`
	Alias               string    `json:"alias,omitempty"`
	Kind                string    `json:"kind"`
	WorldID             string    `json:"world_id,omitempty"`
	GradeBand           string    `json:"grade_band,omitempty"`
	Mood                string    `json:"mood,omitempty"`
	DominantColor       string    `json:"dominant_color"`
	LQIP                string    `json:"lqip"`
	Width               int       `json:"width"`
	Height              int       `json:"height"`
	AltFR               string    `json:"alt_fr"`
	AltEN               string    `json:"alt_en"`
	SourceName          string    `json:"source_name"`
	SourceURL           string    `json:"source_url"`
	Creator             string    `json:"creator"`
	CreatorURL          string    `json:"creator_url,omitempty"`
	Licence             string    `json:"licence"`
	LicenceURL          string    `json:"licence_url"`
	AttributionText     string    `json:"attribution_text"`
	AttributionRequired Flag      `json:"attribution_required"`
	PeopleFree          *Flag     `json:"people_free,omitempty"`
	BrandFree           *Flag     `json:"brand_free,omitempty"`
	ReviewStatus        string    `json:"review_status,omitempty"`
	ReviewedBy          string    `json:"reviewed_by,omitempty"`
	Tags                TagList   `json:"tags,omitempty"`
	Variants            []Variant `json:"variants,omitempty"`
}

// licences are the values the database accepts; aliases map common spellings onto them.
var licences = map[string]string{
	"CC0": "CC0", "PDM": "PDM", "CC-BY-4.0": "CC-BY-4.0", "CC-BY-3.0": "CC-BY-3.0", "CC-BY-2.0": "CC-BY-2.0", "MIT": "MIT", "PEXELS": "PEXELS", "OWNED": "OWNED",
	"CC BY 4.0": "CC-BY-4.0", "CC BY 3.0": "CC-BY-3.0", "CC BY 2.0": "CC-BY-2.0", "PUBLIC DOMAIN": "PDM", "PEXELS LICENSE": "PEXELS", "CC-BY": "CC-BY-4.0",
}

// Normalise fills defaults and maps the licence spelling. It returns an error naming the first bad field.
func (d *Doc) Normalise() error {
	if !strings.HasPrefix(d.ID, "a_") || len(d.ID) < 6 {
		return fmt.Errorf("asset id %q must start with a_", d.ID)
	}
	lic, ok := licences[strings.ToUpper(strings.TrimSpace(d.Licence))]
	if !ok {
		return fmt.Errorf("asset %s: licence %q is not allowed (CC0, PDM, CC-BY-x, MIT, PEXELS, OWNED)", d.ID, d.Licence)
	}
	d.Licence = lic
	if d.GradeBand == "" {
		d.GradeBand = "all"
	}
	if d.ReviewStatus == "" {
		d.ReviewStatus = "pending"
	}
	for field, v := range map[string]string{"kind": d.Kind, "dominant_color": d.DominantColor, "lqip": d.LQIP, "alt_fr": d.AltFR, "alt_en": d.AltEN,
		"source_name": d.SourceName, "source_url": d.SourceURL, "creator": d.Creator, "licence_url": d.LicenceURL, "attribution_text": d.AttributionText} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("asset %s: %s is required", d.ID, field)
		}
	}
	if d.Width <= 0 || d.Height <= 0 {
		return fmt.Errorf("asset %s: width and height are required", d.ID)
	}
	if d.ReviewStatus == "approved" {
		if (d.PeopleFree != nil && !bool(*d.PeopleFree)) || (d.BrandFree != nil && !bool(*d.BrandFree)) {
			return fmt.Errorf("asset %s: an approved asset must be people_free and brand_free", d.ID)
		}
		if d.Licence == "PEXELS" && d.CreatorURL == "" {
			return fmt.Errorf("asset %s: a Pexels photo needs creator_url before approval", d.ID)
		}
	}
	return nil
}

// Seed is what a seed/assets directory contains.
type Seed struct {
	Assets []Doc
	Vocab  []VocabEntry
	Dir    string // seed/assets, for reading the variant files
}

// VocabEntry is one allowed facet value.
type VocabEntry struct {
	Facet, Value, LabelFR, LabelEN, Parent string
}

// LoadSeed reads assets.json and vocab.json from dir. A missing file is not an error (the P3 output may not exist yet).
func LoadSeed(dir string) (Seed, error) {
	s := Seed{Dir: dir}
	raw, err := os.ReadFile(filepath.Join(dir, "assets.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return s, fmt.Errorf("read assets.json: %w", err)
	default:
		var wrapped struct {
			Assets []Doc `json:"assets"`
		}
		if json.Unmarshal(raw, &wrapped) == nil && wrapped.Assets != nil {
			s.Assets = wrapped.Assets
		} else if err := json.Unmarshal(raw, &s.Assets); err != nil {
			return s, fmt.Errorf("parse assets.json: %w", err)
		}
		for i := range s.Assets {
			if err := s.Assets[i].Normalise(); err != nil {
				return s, err
			}
		}
	}
	raw, err = os.ReadFile(filepath.Join(dir, "vocab.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return s, fmt.Errorf("read vocab.json: %w", err)
	default:
		if s.Vocab, err = parseVocab(raw); err != nil {
			return s, fmt.Errorf("parse vocab.json: %w", err)
		}
	}
	return s, nil
}

// parseVocab reads {"topic":[{"value":"baking","label_fr":..}]}, {"topic":{"baking":{"fr":..,"en":..}}} or a flat list of entries.
func parseVocab(raw []byte) ([]VocabEntry, error) {
	type item struct {
		Facet   string            `json:"facet"`
		Value   string            `json:"value"`
		LabelFR string            `json:"label_fr"`
		LabelEN string            `json:"label_en"`
		Label   map[string]string `json:"label"`
		Parent  string            `json:"parent"`
	}
	toEntry := func(facet string, it item) VocabEntry {
		e := VocabEntry{Facet: facet, Value: it.Value, LabelFR: it.LabelFR, LabelEN: it.LabelEN, Parent: it.Parent}
		if e.LabelFR == "" {
			e.LabelFR = it.Label["fr"]
		}
		if e.LabelEN == "" {
			e.LabelEN = it.Label["en"]
		}
		return e
	}
	var flat []item
	if json.Unmarshal(raw, &flat) == nil {
		out := make([]VocabEntry, 0, len(flat))
		for _, it := range flat {
			out = append(out, toEntry(it.Facet, it))
		}
		return out, nil
	}
	var byFacet map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byFacet); err != nil {
		return nil, err
	}
	var out []VocabEntry
	facets := make([]string, 0, len(byFacet))
	for f := range byFacet {
		facets = append(facets, f)
	}
	sort.Strings(facets)
	for _, facet := range facets {
		var list []item
		if json.Unmarshal(byFacet[facet], &list) == nil {
			for _, it := range list {
				out = append(out, toEntry(facet, it))
			}
			continue
		}
		var asMap map[string]map[string]string
		if err := json.Unmarshal(byFacet[facet], &asMap); err != nil {
			return nil, fmt.Errorf("facet %s: %w", facet, err)
		}
		vals := make([]string, 0, len(asMap))
		for v := range asMap {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		for _, v := range vals {
			out = append(out, toEntry(facet, item{Value: v, Label: asMap[v]}))
		}
	}
	return out, nil
}

// Hash is the change detector of a doc (metadata only).
func (d Doc) Hash() (string, error) { return content.Hash(d) }

// UpsertDoc inserts a new asset with its variants, or refreshes the metadata of a known one when the doc
// hash changed. Bytes are never replaced (the id is a hash of the image). files maps a width to its bytes.
func (s *Store) UpsertDoc(ctx context.Context, tx *sql.Tx, d Doc, files map[int][]byte) (created, changed bool, err error) {
	hash, err := d.Hash()
	if err != nil {
		return false, false, err
	}
	var cur string
	err = tx.QueryRowContext(ctx, `SELECT content_hash FROM assets WHERE id = ?`, d.ID).Scan(&cur)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, false, fmt.Errorf("read asset %s: %w", d.ID, err)
	}
	people, brand := true, true
	if d.PeopleFree != nil {
		people = bool(*d.PeopleFree)
	}
	if d.BrandFree != nil {
		brand = bool(*d.BrandFree)
	}
	if !exists || cur != hash {
		now := s.Now().UTC().Format(tsLayout)
		var world, alias, mood, creatorURL any
		if d.WorldID != "" {
			world = d.WorldID
		}
		if d.Alias != "" {
			alias = d.Alias
		}
		if d.Mood != "" {
			mood = d.Mood
		}
		if d.CreatorURL != "" {
			creatorURL = d.CreatorURL
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assets (id, alias, kind, world_id, grade_band, mood, dominant_color, lqip, width, height, alt_fr, alt_en, source_name, source_url,
			creator, creator_url, licence, licence_url, attribution_text, attribution_required, people_free, brand_free, review_status, reviewed_by, content_hash, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET alias=excluded.alias, kind=excluded.kind, world_id=excluded.world_id, grade_band=excluded.grade_band, mood=excluded.mood,
			  dominant_color=excluded.dominant_color, lqip=excluded.lqip, width=excluded.width, height=excluded.height, alt_fr=excluded.alt_fr, alt_en=excluded.alt_en,
			  source_name=excluded.source_name, source_url=excluded.source_url, creator=excluded.creator, creator_url=excluded.creator_url, licence=excluded.licence,
			  licence_url=excluded.licence_url, attribution_text=excluded.attribution_text, attribution_required=excluded.attribution_required,
			  people_free=excluded.people_free, brand_free=excluded.brand_free, review_status=excluded.review_status, reviewed_by=excluded.reviewed_by,
			  content_hash=excluded.content_hash`,
			d.ID, alias, d.Kind, world, d.GradeBand, mood, d.DominantColor, d.LQIP, d.Width, d.Height, d.AltFR, d.AltEN, d.SourceName, d.SourceURL,
			d.Creator, creatorURL, d.Licence, d.LicenceURL, d.AttributionText, b2i(bool(d.AttributionRequired)), b2i(people), b2i(brand), d.ReviewStatus,
			nullStr(d.ReviewedBy), hash, now)
		if err != nil {
			return false, false, fmt.Errorf("write asset %s: %w", d.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id = ?`, d.ID); err != nil {
			return false, false, err
		}
		for facet, vals := range d.Tags {
			for _, v := range vals {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO asset_tags (asset_id, facet, value) VALUES (?,?,?)`, d.ID, facet, v); err != nil {
					return false, false, fmt.Errorf("write tag %s=%s on %s: %w", facet, v, d.ID, err)
				}
			}
		}
		changed = true
	}
	for w, b := range files { // bytes: only new widths are inserted
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO asset_variants (asset_id, w, format, bytes, size) VALUES (?,?,'webp',?,?)`, d.ID, w, b, len(b))
		if err != nil {
			return false, false, fmt.Errorf("write variant %s-%d: %w", d.ID, w, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			changed = true
		}
	}
	return !exists, changed, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ReplaceVocab upserts the controlled vocabulary and returns how many rows changed.
func (s *Store) ReplaceVocab(ctx context.Context, tx *sql.Tx, list []VocabEntry) (int, error) {
	n := 0
	for _, e := range list {
		res, err := tx.ExecContext(ctx, `INSERT INTO asset_vocab (facet, value, label_fr, label_en, parent) VALUES (?,?,?,?,?)
			ON CONFLICT(facet, value) DO UPDATE SET label_fr=excluded.label_fr, label_en=excluded.label_en, parent=excluded.parent
			WHERE asset_vocab.label_fr IS NOT excluded.label_fr OR asset_vocab.label_en IS NOT excluded.label_en OR asset_vocab.parent IS NOT excluded.parent`,
			e.Facet, e.Value, nullStr(e.LabelFR), nullStr(e.LabelEN), nullStr(e.Parent))
		if err != nil {
			return n, fmt.Errorf("write vocab %s=%s: %w", e.Facet, e.Value, err)
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, nil
}

// VocabSet loads the vocabulary as a lookup for the validator.
func (s *Store) VocabSet(ctx context.Context, q Querier) (map[[2]string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT facet, value FROM asset_vocab`)
	if err != nil {
		return nil, fmt.Errorf("list vocab: %w", err)
	}
	defer rows.Close()
	out := map[[2]string]bool{}
	for rows.Next() {
		var f, v string
		if err := rows.Scan(&f, &v); err != nil {
			return nil, err
		}
		out[[2]string{f, v}] = true
	}
	return out, rows.Err()
}

// ReadFiles loads the bytes of a doc's variants from <dir>/out. A missing listed file is an error.
func (sd Seed) ReadFiles(d Doc) (map[int][]byte, error) {
	out := map[int][]byte{}
	for _, v := range d.Variants {
		b, err := os.ReadFile(filepath.Join(sd.Dir, "out", v.File))
		if err != nil {
			return nil, fmt.Errorf("asset %s: read %s: %w", d.ID, v.File, err)
		}
		if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
			return nil, fmt.Errorf("asset %s: %s is not a WebP file", d.ID, v.File)
		}
		out[v.W] = b
	}
	return out, nil
}
