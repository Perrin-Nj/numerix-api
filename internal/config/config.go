// Package config reads and validates settings from environment variables.
//
// Go for Spring people: this replaces application.yml + @ConfigurationProperties.
// There is no container, so main() calls Load once and passes the Config struct to whoever needs it.
package config

import (
	"errors"  // errors.Join merges several validation problems into one error
	"fmt"     // Sprintf/Errorf formatting
	"strconv" // string -> number conversion
	"strings" // string helpers
	"time"    // durations for token lifetimes and timeouts
)

// Environment names accepted in APP_ENV.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// devJWTSecret is used only outside production when JWT_SECRET is not set. Never valid in production.
const devJWTSecret = "dev-only-secret-do-not-use-in-production-0123456789"

// Config holds every setting the service needs (Spring: a @ConfigurationProperties bean).
type Config struct {
	Port               string        // TCP port to listen on
	Env                string        // development, test or production
	DBPath             string        // SQLite file path
	JWTSecret          string        // HMAC key used to sign access tokens
	AllowedOrigins     []string      // browser origins allowed by CORS (admin app)
	AdminAPIToken      string        // static bearer token for /admin/v1 (empty = admin API disabled)
	FeatureWelcomeGift bool          // FEATURE_WELCOME_GIFT: the server grants the one-time welcome card (default off)
	TrustProxy         bool          // read the client IP from X-Forwarded-For (only behind a trusted proxy)
	AccessTTL          time.Duration // parent access token lifetime
	ChildTTL           time.Duration // child access token lifetime (contract: 8 h)
	RefreshTTL         time.Duration // parent refresh token lifetime (contract: 30 days)
	RequestTimeout     time.Duration // deadline attached to every request context
	MaxConcurrent      int           // max requests in flight before answering 429
	RateLimitIPPerMin  int           // RATE_LIMIT_IP_PER_MIN: per-IP ceiling for all requests (default 600; raise only for load tests)
	PprofEnabled       bool          // PPROF=true: expose /debug/pprof (refused in production)
	LLMProvider        string        // placeholder: "stub" until the narrator exists
	LLMAPIKey          string        // placeholder: secret for the LLM provider
	LLMModel           string        // placeholder: model name
	LLMMonthlyCap      float64       // placeholder: monthly budget in dollars
	WebAuthnRPID       string        // WEBAUTHN_RP_ID: site domain passkeys are bound to (empty in production = passkeys off)
	WebAuthnOrigins    []string      // WEBAUTHN_ORIGINS: browser origins allowed to run the ceremony
	WebAuthnRPName     string        // WEBAUTHN_RP_NAME: name shown by the browser prompt
	MongoURI           string        // MONGODB_URI: Atlas SRV string; empty = Mongo sync off
	MongoDB            string        // MONGODB_DB: database name (default numerix)
	MongoPseudoSecret  string        // MONGO_PSEUDO_SECRET: HMAC key for pseudonymous ids (min 32 chars when MONGODB_URI is set)
	AssetBaseURL       string        // ASSET_BASE_URL: path prefix of image URLs sent to clients (default /assets, rewritten by the web app to /v1/assets)
}

// IsProduction tells whether strict production rules apply.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load builds a Config from a lookup function. Passing os.Getenv in main and a map lookup in
// tests keeps the function pure and easy to test (no global state).
func Load(getenv func(string) string) (Config, error) {
	// get returns the variable, or the default when it is empty.
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	var errs []error // collect every problem so the operator fixes them in one pass

	// Numbers: parse and remember the error instead of failing on the first one.
	atoi := func(key string, def int) int {
		v := get(key, strconv.Itoa(def))
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be an integer, got %q", key, v))
			return def
		}
		return n
	}
	seconds := func(key string, def int) time.Duration { return time.Duration(atoi(key, def)) * time.Second }

	cfg := Config{
		Port:               get("PORT", "8081"), // 8081 matches API_URL in the web app contract
		Env:                get("APP_ENV", EnvDevelopment),
		DBPath:             get("DB_PATH", "./data/numerix.db"),
		JWTSecret:          get("JWT_SECRET", ""),
		AdminAPIToken:      get("ADMIN_API_TOKEN", ""),
		FeatureWelcomeGift: get("FEATURE_WELCOME_GIFT", "false") == "true",
		TrustProxy:         get("TRUST_PROXY", "false") == "true",
		AccessTTL:          seconds("ACCESS_TTL_SECONDS", 900),
		ChildTTL:           seconds("CHILD_TTL_SECONDS", 28800),
		RefreshTTL:         seconds("REFRESH_TTL_SECONDS", 30*24*3600),
		RequestTimeout:     seconds("REQUEST_TIMEOUT_SECONDS", 10),
		MaxConcurrent:      atoi("MAX_CONCURRENT_REQUESTS", 256),
		RateLimitIPPerMin:  atoi("RATE_LIMIT_IP_PER_MIN", 600), // contract section 16: 600 per minute per IP
		PprofEnabled:       get("PPROF", "false") == "true",
		LLMProvider:        get("LLM_PROVIDER", "stub"),
		LLMAPIKey:          get("LLM_API_KEY", ""),
		LLMModel:           get("LLM_MODEL", ""),
		MongoURI:           get("MONGODB_URI", ""),
		MongoDB:            get("MONGODB_DB", "numerix"),
		MongoPseudoSecret:  get("MONGO_PSEUDO_SECRET", ""),
		AssetBaseURL:       strings.TrimRight(get("ASSET_BASE_URL", "/assets"), "/"),
	}
	// Comma separated list -> slice, dropping blanks.
	for _, o := range strings.Split(get("ALLOWED_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
		}
	}
	if v := get("LLM_MONTHLY_CAP", "0"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("LLM_MONTHLY_CAP must be a number, got %q", v))
		}
		cfg.LLMMonthlyCap = f
	}

	// Passkeys: localhost defaults outside production so dev works at once; production must opt in.
	defRP, defOrigin := "", ""
	if !cfg.IsProduction() {
		defRP, defOrigin = "localhost", "http://localhost:3000"
	}
	cfg.WebAuthnRPID = get("WEBAUTHN_RP_ID", defRP)
	cfg.WebAuthnRPName = get("WEBAUTHN_RP_NAME", "Numerix")
	for _, o := range strings.Split(get("WEBAUTHN_ORIGINS", defOrigin), ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.WebAuthnOrigins = append(cfg.WebAuthnOrigins, o)
		}
	}

	// Validation.
	switch cfg.Env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be development, test or production, got %q", cfg.Env))
	}
	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		errs = append(errs, fmt.Errorf("PORT must be 1-65535, got %q", cfg.Port))
	}
	if cfg.JWTSecret == "" && !cfg.IsProduction() {
		cfg.JWTSecret = devJWTSecret // convenient locally, refused in production below
	}
	if cfg.IsProduction() && len(cfg.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET is required in production and must be at least 32 characters"))
	}
	if cfg.AccessTTL <= 0 || cfg.ChildTTL <= 0 || cfg.RefreshTTL <= 0 || cfg.RequestTimeout <= 0 {
		errs = append(errs, errors.New("TTL and timeout values must be positive"))
	}
	if cfg.MaxConcurrent < 1 {
		errs = append(errs, errors.New("MAX_CONCURRENT_REQUESTS must be at least 1"))
	}
	if cfg.RateLimitIPPerMin < 1 {
		errs = append(errs, errors.New("RATE_LIMIT_IP_PER_MIN must be at least 1"))
	}
	if cfg.PprofEnabled && cfg.IsProduction() {
		errs = append(errs, errors.New("PPROF must not be enabled in production"))
	}
	// The secret is demanded in every environment (not only production) so a dev machine can never write
	// guessable pseudonyms into a shared Atlas cluster.
	if cfg.MongoURI != "" && len(cfg.MongoPseudoSecret) < 32 {
		errs = append(errs, errors.New("MONGO_PSEUDO_SECRET is required when MONGODB_URI is set and must be at least 32 characters"))
	}
	if cfg.AdminAPIToken != "" && len(cfg.AdminAPIToken) < 16 {
		errs = append(errs, errors.New("ADMIN_API_TOKEN must be at least 16 characters when set"))
	}
	return cfg, errors.Join(errs...) // nil when errs is empty
}
