package app

// GET /v1/config/public: the admin-controlled configuration the player app may read without logging in.
//
// Go for Spring people: the body is built once by appconfig (cached until the next admin save) as JSON, and decoded
// into the generated contract type so the contract and the Go struct cannot drift (a test checks every key).

import (
	"context"
	"encoding/json"

	"github.com/Perrin-Nj/numerix-api/api"
)

// publicConfigCacheControl lets browsers and the CDN reuse the answer for a minute.
const publicConfigCacheControl = "public, max-age=60"

// GetPublicConfig answers GET /config/public.
func (s *Server) GetPublicConfig(ctx context.Context, _ api.GetPublicConfigRequestObject) (api.GetPublicConfigResponseObject, error) {
	raw, err := s.Cfg.PublicJSON(ctx)
	if err != nil {
		return nil, s.plInternalErr(ctx, "load public config", err)
	}
	var body api.AppPublicConfig
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, s.plInternalErr(ctx, "decode public config", err)
	}
	cc := publicConfigCacheControl
	return api.GetPublicConfig200JSONResponse{Body: body, Headers: api.GetPublicConfig200ResponseHeaders{CacheControl: &cc}}, nil
}
