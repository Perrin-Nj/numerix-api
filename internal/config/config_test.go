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
}
