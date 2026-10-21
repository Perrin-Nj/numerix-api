package config

import (
	"os"
	"regexp"
	"testing"
)

func TestAPIEnvExamplesCoverRuntimeVariables(t *testing.T) {
	variable := regexp.MustCompile(`(?:get|getenv)\("([A-Z][A-Z0-9_]*)"`)
	sources := []string{"config.go", "../drafter/drafter.go"}
	examples := []string{
		"../../.env.example",
		"../../deploy/oracle/env.example",
		"../../deploy/oracle/api.staging.env.example",
		"../../deploy/oracle/api.production.env.example",
	}
	names := map[string]struct{}{}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range variable.FindAllSubmatch(data, -1) {
			names[string(match[1])] = struct{}{}
		}
	}
	for _, example := range examples {
		data, err := os.ReadFile(example)
		if err != nil {
			t.Fatal(err)
		}
		for name := range names {
			if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `=`).Match(data) {
				t.Errorf("%s is missing %s", example, name)
			}
		}
	}
	for _, example := range []string{"../../deploy/oracle/admin.env.example", "../../deploy/oracle/admin.staging.env.example", "../../deploy/oracle/admin.production.env.example"} {
		data, err := os.ReadFile(example)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"PLATFORM_API_URL", "ADMIN_API_TOKEN", "ADMIN_PASSWORD", "ADMIN_SESSION_SECRET"} {
			if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `=`).Match(data) {
				t.Errorf("%s is missing %s", example, name)
			}
		}
	}
	for _, example := range []string{"../../deploy/oracle/web.staging.env.example", "../../deploy/oracle/web.production.env.example"} {
		data, err := os.ReadFile(example)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`(?m)^API_URL=`).Match(data) {
			t.Errorf("%s is missing API_URL", example)
		}
	}
}
