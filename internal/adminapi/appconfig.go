package adminapi

// Runtime configuration endpoints: GET/PUT a namespace, history, rollback, plus the read-only environment view.
// Auth runs first (the guard wrapper), then the body is validated, then the change and its audit rows commit together.
//
// Go for Spring people: each method is a @RestController handler. errors.As is "catch (ValidationException e)".

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Perrin-Nj/numerix-api/internal/appconfig"
	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// cfgErr maps appconfig errors to the admin problem codes.
func (a *API) cfgErr(w http.ResponseWriter, r *http.Request, err error) {
	var ve *appconfig.ValidationError
	switch {
	case errors.As(err, &ve):
		a.fail(w, r, bad(ve.Msg))
	case errors.Is(err, appconfig.ErrUnknown):
		a.fail(w, r, httpapi.ErrNotFound)
	case errors.Is(err, appconfig.ErrConflict):
		a.fail(w, r, httpapi.NewError(http.StatusConflict, "version_conflict", err.Error()))
	case errors.Is(err, appconfig.ErrNothingToRevert):
		a.fail(w, r, httpapi.NewError(http.StatusConflict, "nothing_to_roll_back", err.Error()))
	default:
		a.fail(w, r, err)
	}
}

// GET /admin/v1/appconfig: every namespace with its version, plus the environment-only settings (read-only).
func (a *API) listAppConfig(w http.ResponseWriter, r *http.Request) {
	type row struct {
		Name      string `json:"name"`
		Version   int    `json:"version"`
		UpdatedAt string `json:"updated_at"`
		UpdatedBy string `json:"updated_by"`
	}
	rows := []row{}
	for _, ns := range a.cfg.Names() {
		d, err := a.cfg.Current(r.Context(), ns)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		rows = append(rows, row{d.Name, d.Version, d.UpdatedAt, d.UpdatedBy})
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": rows, "env": a.envView()})
}

// GET /admin/v1/appconfig/{ns}: the current document, its version and the built-in defaults.
func (a *API) getAppConfig(w http.ResponseWriter, r *http.Request) {
	d, err := a.cfg.Current(r.Context(), r.PathValue("ns"))
	if err != nil {
		a.cfgErr(w, r, err)
		return
	}
	def, _ := a.cfg.Defaults(d.Name)
	writeJSON(w, http.StatusOK, map[string]any{"name": d.Name, "version": d.Version, "updated_at": d.UpdatedAt,
		"updated_by": d.UpdatedBy, "value": d.Value, "defaults": def})
}

// PUT /admin/v1/appconfig/{ns} body {"value": {...}, "expected_version": n?}.
func (a *API) putAppConfig(w http.ResponseWriter, r *http.Request) {
	var b struct {
		RawValue        json.RawMessage `json:"value"`
		ExpectedVersion *int            `json:"expected_version"`
	}
	if err := decode(r, &b); err != nil {
		a.fail(w, r, err)
		return
	}
	if len(b.RawValue) == 0 {
		a.fail(w, r, bad("body must be {\"value\": {...}}"))
		return
	}
	d, err := a.cfg.Set(r.Context(), r.PathValue("ns"), b.RawValue, actor(r), b.ExpectedVersion)
	if err != nil {
		a.cfgErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// GET /admin/v1/appconfig/{ns}/versions?limit=50
func (a *API) appConfigVersions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	v, err := a.cfg.Versions(r.Context(), r.PathValue("ns"), min(200, max(1, orDefault(limit, 50))))
	if err != nil {
		a.cfgErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": v})
}

// POST /admin/v1/appconfig/{ns}/rollback body {"to_version": n?}: saves the previous value as a new version.
func (a *API) rollbackAppConfig(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ToVersion *int `json:"to_version"`
	}
	if err := decode(r, &b); err != nil {
		a.fail(w, r, err)
		return
	}
	d, err := a.cfg.Rollback(r.Context(), r.PathValue("ns"), actor(r), b.ToVersion)
	if err != nil {
		a.cfgErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// envItem is one environment-only setting. Secrets show only whether they are set, never the value.
type envItem struct {
	Key    string  `json:"key"`
	Set    bool    `json:"set"`
	Secret bool    `json:"secret"`
	Value  *string `json:"value,omitempty"` // non-secret settings only
}

// envView lists the settings that stay in the environment (restart needed, or too sensitive for a web form).
func (a *API) envView() []envItem {
	out := []envItem{}
	add := func(key string, secret bool) {
		v := strings.TrimSpace(a.getenv(key))
		it := envItem{Key: key, Set: v != "", Secret: secret}
		if !secret && v != "" {
			it.Value = &v
		}
		out = append(out, it)
	}
	for _, k := range []string{"JWT_SECRET", "ADMIN_API_TOKEN", "MONGODB_URI", "MONGO_PSEUDO_SECRET", "LLM_API_KEY", "ANTHROPIC_API_KEY"} {
		add(k, true)
	}
	for _, k := range []string{"APP_ENV", "PORT", "DB_PATH", "ALLOWED_ORIGINS", "ADMIN_ORIGIN", "TRUST_PROXY", "RATE_LIMIT_IP_PER_MIN",
		"MAX_CONCURRENT_REQUESTS", "REQUEST_TIMEOUT_SECONDS", "ACCESS_TTL_SECONDS", "CHILD_TTL_SECONDS", "REFRESH_TTL_SECONDS",
		"WEBAUTHN_RP_ID", "WEBAUTHN_ORIGINS", "ASSET_BASE_URL", "MEDIA_DIR", "MEDIA_BASE_URL", "NARRATOR_MODEL", "FEATURE_WELCOME_GIFT", "PPROF"} {
		add(k, false)
	}
	return out
}

// GET /admin/v1/config: the platform document (kept for the first admin screens; same as appconfig/platform).
func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	d, err := a.cfg.Current(r.Context(), appconfig.NSPlatform)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(d.Value)
}

// PUT /admin/v1/config: partial update of the platform document, every key validated like a full save.
func (a *API) putConfig(w http.ResponseWriter, r *http.Request) {
	patch := map[string]json.RawMessage{}
	if err := decode(r, &patch); err != nil {
		a.fail(w, r, err)
		return
	}
	cur, err := a.cfg.Current(r.Context(), appconfig.NSPlatform)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	merged := map[string]json.RawMessage{}
	_ = json.Unmarshal(cur.Value, &merged)
	for k, v := range patch {
		merged[k] = v
	}
	raw, _ := json.Marshal(merged)
	d, err := a.cfg.Set(r.Context(), appconfig.NSPlatform, raw, actor(r), nil)
	if err != nil {
		a.cfgErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(d.Value)
}
