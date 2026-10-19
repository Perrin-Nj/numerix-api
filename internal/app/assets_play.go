package app

// Public image endpoints: GET /assets/{file} and GET /credits (docs/CONTENT_PLAN.md section 4.5).
//
// Go for Spring people: the generated strict server gives a typed response per status code. Returning
// GetAsset200ImagewebpResponse with a Body (an io.Reader) streams the bytes; the Headers struct sets ETag and
// Cache-Control. Nothing here needs a login: the asset id is a hash, so it cannot be guessed, and only
// approved, people-free, brand-free assets are ever served.

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/api"
	"github.com/Perrin-Nj/numerix-api/internal/assetstore"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// assetFileRe splits "a_<20 hex>" and the optional "-<width>" from the file name.
var assetFileRe = regexp.MustCompile(`^(a_[0-9a-f]{20})(?:-([0-9]{2,4}))?\.webp$`)

// assetCacheControl: the id is a content hash, so the bytes behind a URL never change.
const assetCacheControl = "public, max-age=31536000, immutable"

// GetAsset answers GET /assets/{file}.
func (s *Server) GetAsset(ctx context.Context, req api.GetAssetRequestObject) (api.GetAssetResponseObject, error) {
	m := assetFileRe.FindStringSubmatch(req.File)
	if m == nil {
		return nil, httpapi.ErrNotFound
	}
	w := 0
	if m[2] != "" {
		w, _ = strconv.Atoi(m[2]) // digits only, checked by the pattern
	}
	data, etag, err := s.Assets.Variant(ctx, m[1], w)
	if errors.Is(err, assetstore.ErrNotFound) {
		return nil, httpapi.ErrNotFound
	}
	if err != nil {
		return nil, s.plInternalErr(ctx, "read asset", err)
	}
	cc := assetCacheControl
	if etagMatches(req.Params.IfNoneMatch, etag) {
		return api.GetAsset304Response{Headers: api.GetAsset304ResponseHeaders{CacheControl: &cc, ETag: &etag}}, nil
	}
	return api.GetAsset200ImagewebpResponse{
		Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		Headers: api.GetAsset200ResponseHeaders{CacheControl: &cc, ETag: &etag},
	}, nil
}

// etagMatches implements If-None-Match for a strong ETag: a list of tags, "*" or weak tags (W/"x") match.
func etagMatches(header *string, etag string) bool {
	if header == nil {
		return false
	}
	for _, part := range strings.Split(*header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}

// ListCredits answers GET /credits: the attribution lines of the approved images.
func (s *Server) ListCredits(ctx context.Context, _ api.ListCreditsRequestObject) (api.ListCreditsResponseObject, error) {
	list, err := s.Assets.Credits(ctx)
	if err != nil {
		return nil, s.plInternalErr(ctx, "list credits", err)
	}
	base := s.Config.AssetBaseURL
	if base == "" {
		base = "/assets"
	}
	items := make([]api.CreditItem, 0, len(list))
	for _, a := range list {
		ref := a.Ref("fr", base)
		thumb := ""
		if len(ref.SrcSet) > 0 {
			thumb = ref.SrcSet[0].URL // the smallest variant
		}
		it := api.CreditItem{Id: a.ID, Url: thumb, Text: a.AttributionText, Licence: a.Licence}
		if a.SourceURL != "" {
			u := a.SourceURL
			it.SourceUrl = &u
		}
		if a.LicenceURL != "" {
			u := a.LicenceURL
			it.LicenceUrl = &u
		}
		items = append(items, it)
	}
	cc := "public, max-age=3600"
	out := api.ListCredits200JSONResponse{Headers: api.ListCredits200ResponseHeaders{CacheControl: &cc}}
	out.Body.Items = items
	return out, nil
}
