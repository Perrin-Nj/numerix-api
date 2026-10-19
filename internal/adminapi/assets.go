package adminapi

// Media endpoints of the admin API: list and filter, read, review (approve or reject, edit alt text and tags),
// search and preview by criteria. Uploading a master image is not built: assets come from tools/assets and
// cmd/seed (the server has no WebP encoder, and the plan keeps one pipeline for licence checks).

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/content"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// assetView is the admin representation of an asset, with its MediaRef.
type assetView struct {
	ID                  string              `json:"id"`
	Alias               string              `json:"alias,omitempty"`
	Kind                string              `json:"kind"`
	World               string              `json:"world,omitempty"`
	GradeBand           string              `json:"grade_band"`
	Mood                string              `json:"mood,omitempty"`
	AltFR               string              `json:"alt_fr"`
	AltEN               string              `json:"alt_en"`
	Licence             string              `json:"licence"`
	LicenceURL          string              `json:"licence_url"`
	Creator             string              `json:"creator"`
	SourceName          string              `json:"source_name"`
	SourceURL           string              `json:"source_url"`
	AttributionText     string              `json:"attribution_text"`
	AttributionRequired bool                `json:"attribution_required"`
	PeopleFree          bool                `json:"people_free"`
	BrandFree           bool                `json:"brand_free"`
	ReviewStatus        string              `json:"review_status"`
	ReviewedBy          string              `json:"reviewed_by,omitempty"`
	Tags                map[string][]string `json:"tags"`
	Media               assetstore.MediaRef `json:"media"`
}

func toAssetView(a assetstore.Asset, base string) assetView {
	if a.Tags == nil {
		a.Tags = map[string][]string{}
	}
	return assetView{ID: a.ID, Alias: a.Alias, Kind: a.Kind, World: a.WorldID, GradeBand: a.GradeBand, Mood: a.Mood, AltFR: a.AltFR, AltEN: a.AltEN,
		Licence: a.Licence, LicenceURL: a.LicenceURL, Creator: a.Creator, SourceName: a.SourceName, SourceURL: a.SourceURL, AttributionText: a.AttributionText,
		AttributionRequired: a.AttributionRequired, PeopleFree: a.PeopleFree, BrandFree: a.BrandFree, ReviewStatus: a.ReviewStatus, ReviewedBy: a.ReviewedBy,
		Tags: a.Tags, Media: a.Ref("fr", base)}
}

const adminAssetBase = "/assets"

// GET /assets?kind=&world=&status=&licence=&facet=&value=&q=
func (a *API) listAssets(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	pg, size, _ := page(r)
	list, total, err := a.assets.List(r.Context(), assetstore.Filter{Kind: qs.Get("kind"), World: qs.Get("world"), Status: qs.Get("status"), Licence: qs.Get("licence"),
		Facet: qs.Get("facet"), Value: qs.Get("value"), Text: qs.Get("q"), Page: pg, PageSize: size})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	items := make([]assetView, len(list))
	for i := range list {
		items[i] = toAssetView(list[i], adminAssetBase)
	}
	writeJSON(w, http.StatusOK, pageOf[assetView]{Items: items, Page: pg, PageSize: size, Total: total})
}

// GET /assets/{id}
func (a *API) getAsset(w http.ResponseWriter, r *http.Request) {
	as, err := a.assets.Get(r.Context(), a.db.R, r.PathValue("id"))
	if errors.Is(err, assetstore.ErrNotFound) {
		a.fail(w, r, httpapi.ErrNotFound)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAssetView(as, adminAssetBase))
}

// PATCH /assets/{id} {alt_fr, alt_en, review_status, people_free, brand_free, tags}
func (a *API) patchAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		AltFR        *string             `json:"alt_fr"`
		AltEN        *string             `json:"alt_en"`
		ReviewStatus *string             `json:"review_status"`
		PeopleFree   *bool               `json:"people_free"`
		BrandFree    *bool               `json:"brand_free"`
		Tags         map[string][]string `json:"tags"`
	}
	if err := decode(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	if rs := in.ReviewStatus; rs != nil && *rs != "pending" && *rs != "approved" && *rs != "rejected" {
		a.fail(w, r, bad("review_status must be pending, approved or rejected"))
		return
	}
	for facet := range in.Tags {
		switch facet {
		case "world", "topic", "object", "mood", "setting":
		default:
			a.fail(w, r, bad("unknown tag facet "+facet))
			return
		}
	}
	who := actor(r)
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) error {
		reviewer := ""
		if in.ReviewStatus != nil {
			reviewer = who
		}
		if err := a.assets.Apply(r.Context(), tx, id, assetstore.Patch{AltFR: in.AltFR, AltEN: in.AltEN, ReviewStatus: in.ReviewStatus, ReviewedBy: reviewer,
			PeopleFree: in.PeopleFree, BrandFree: in.BrandFree, Tags: in.Tags}); err != nil {
			return err
		}
		return a.writeAudit(tx, who, "asset.update", "asset", id, "")
	})
	switch {
	case errors.Is(err, assetstore.ErrNotFound):
		a.fail(w, r, httpapi.ErrNotFound)
		return
	case err != nil:
		a.fail(w, r, bad(err.Error()))
		return
	}
	as, err := a.assets.Get(r.Context(), a.db.R, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.content.AfterCommit() // an approval changes what the world pages show
	writeJSON(w, http.StatusOK, toAssetView(as, adminAssetBase))
}

// POST /assets: not built (see the file comment).
func (a *API) createAsset(w http.ResponseWriter, r *http.Request) {
	a.fail(w, r, httpapi.NewError(http.StatusNotImplemented, "not_implemented", "Images are loaded by tools/assets and cmd/seed, not uploaded here."))
}

// criteriaFrom reads the media criteria from the query string.
func criteriaFrom(r *http.Request) (content.Criteria, string) {
	q := r.URL.Query()
	c := content.Criteria{World: q.Get("world"), Topic: q.Get("topic"), Object: q.Get("object"), Mood: q.Get("mood"), Kind: q.Get("kind"), Setting: q.Get("setting")}
	band := ""
	if g, err := strconv.Atoi(q.Get("grade")); err == nil && g >= 1 && g <= 6 {
		band = assetstore.GradeBand(g)
	}
	return c, band
}

// GET /assets/search?world=&topic=&object=&mood=&setting=&kind=&grade=&limit=: the scored candidates for the media picker.
func (a *API) searchAssets(w http.ResponseWriter, r *http.Request) {
	c, band := criteriaFrom(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 24
	}
	res, err := a.assets.Search(r.Context(), a.db.R, c, band, limit)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	type item struct {
		Score int       `json:"score"`
		Asset assetView `json:"asset"`
	}
	items := make([]item, len(res))
	for i, s := range res {
		items[i] = item{s.Score, toAssetView(s.Asset, adminAssetBase)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /assets/resolve?...&child=&question=: the asset a child would get for these criteria (empty child = the best one).
func (a *API) resolveAsset(w http.ResponseWriter, r *http.Request) {
	c, band := criteriaFrom(r)
	as, err := a.assets.Resolve(r.Context(), a.db.R, c, r.URL.Query().Get("child"), r.URL.Query().Get("question"), band)
	if errors.Is(err, assetstore.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"asset": nil, "fallback": "gradient"})
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset": toAssetView(as, adminAssetBase)})
}
