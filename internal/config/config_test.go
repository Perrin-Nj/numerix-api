package config

import (
	"strings"
	"testing"
	"time"
)

// env turns a map into a lookup function, so tests never touch the real environment.
func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoad(t *testing.T) {
	// Table test: one row per scenario (Spring: JUnit @ParameterizedTest).
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string // substring of the expected error, empty = success
	}{
		{"defaults in development", map[string]string{}, ""},
		{"production without secret", map[string]string{"APP_ENV": "production"}, "JWT_SECRET"},
		{"production short secret", map[string]string{"APP_ENV": "production", "JWT_SECRET": "short"}, "JWT_SECRET"},
		{"production ok", map[string]string{"APP_ENV": "production", "JWT_SECRET": strings.Repeat("x", 32)}, ""},
		{"bad env", map[string]string{"APP_ENV": "staging"}, "APP_ENV"},
		{"bad port", map[string]string{"PORT": "abc"}, "PORT"},
		{"bad ttl", map[string]string{"ACCESS_TTL_SECONDS": "x"}, "ACCESS_TTL_SECONDS"},
		{"zero ip limit", map[string]string{"RATE_LIMIT_IP_PER_MIN": "0"}, "RATE_LIMIT_IP_PER_MIN"},
		{"pprof in production", map[string]string{"APP_ENV": "production", "JWT_SECRET": strings.Repeat("x", 32), "PPROF": "true"}, "PPROF"},
		{"mongo uri without pseudo secret", map[string]string{"MONGODB_URI": "mongodb+srv://x"}, "MONGO_PSEUDO_SECRET"},
		{"mongo uri short pseudo secret", map[string]string{"MONGODB_URI": "mongodb+srv://x", "MONGO_PSEUDO_SECRET": "short"}, "MONGO_PSEUDO_SECRET"},
		{"mongo uri ok", map[string]string{"MONGODB_URI": "mongodb+srv://x", "MONGO_PSEUDO_SECRET": strings.Repeat("p", 32)}, ""},
		{"short admin token", map[string]string{"ADMIN_API_TOKEN": "abc"}, "ADMIN_API_TOKEN"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.env))
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadValues(t *testing.T) {
	cfg, err := Load(env(map[string]string{"PORT": "9000", "ALLOWED_ORIGINS": "https://a.test, https://b.test,", "CHILD_TTL_SECONDS": "60"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9000" || cfg.DBPath != "./data/numerix.db" || cfg.ChildTTL != time.Minute {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("origins: %v", cfg.AllowedOrigins)
	}
	if cfg.AssetBaseURL != "/assets" {
		t.Fatalf("asset base url: %q", cfg.AssetBaseURL)
	}
}

// QA-BE-005: in dev the passkey origin list covers any localhost port (web app on 13xxx); production gets none by default.
func TestWebAuthnDevOriginsAnyLocalhostPort(t *testing.T) {
	cfg, err := Load(env(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	has := func(o string) bool {
		for _, x := range cfg.WebAuthnOrigins {
			if x == o {
				return true
			}
		}
		return false
	}
	if cfg.WebAuthnRPID != "localhost" || !has("http://localhost:3000") || !has("http://localhost:13150") || has("https://evil.example") {
		t.Fatalf("dev webauthn: rp %q, %d origins", cfg.WebAuthnRPID, len(cfg.WebAuthnOrigins))
	}
	prod, err := Load(env(map[string]string{"APP_ENV": "production", "JWT_SECRET": strings.Repeat("x", 32)}))
	if err != nil || prod.WebAuthnRPID != "" || len(prod.WebAuthnOrigins) != 0 {
		t.Fatalf("prod webauthn must be off by default: %v %q %d", err, prod.WebAuthnRPID, len(prod.WebAuthnOrigins))
	}
}
