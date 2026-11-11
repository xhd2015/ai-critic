package services

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// servicesJSONGolden is the services.json body master wrote for the definitions
// in TestMigrationKeepsServicesJSONBytes, with timestamps and $HOME scrubbed.
// The extraction to dot-pkgs/go-pkgs/service must not change a single byte a
// user's config file sees, so this is compared verbatim.
const servicesJSONGolden = `[
  {
    "id": "svc-web",
    "name": "web",
    "command": "npm run dev",
    "workingDir": "/tmp/apps/web",
    "extraEnv": {
      "API_URL": "http://127.0.0.1:8080",
      "NODE_ENV": "development"
    },
    "extraPath": "/opt/homebrew/bin:/usr/local/bin",
    "portForward": {
      "port": 3000,
      "label": "web",
      "provider": "cloudflare",
      "subdomain": "web"
    },
    "enabled": true,
    "createdAt": "<ts>",
    "updatedAt": "<ts>"
  },
  {
    "id": "svc-api",
    "name": "api",
    "command": "go run ./cmd/api",
    "workingDir": "<home>",
    "createdAt": "<ts>",
    "updatedAt": "<ts>"
  },
  {
    "id": "svc-tiny",
    "name": "tiny",
    "command": "sleep 1",
    "workingDir": "<home>",
    "upgradeTarget": "/tmp/bin/tiny",
    "upgradePreStopCmds": [
      "echo pre"
    ],
    "upgradePostStopCmds": [
      "echo post"
    ],
    "createdAt": "<ts>",
    "updatedAt": "<ts>"
  }
]`

// TestMigrationKeepsServicesJSONBytes pins the on-disk contract while the
// normalization helpers move to go-pkgs/service: an empty name must stay empty
// (NormalizeDefinition would default it to the id), an empty working dir must
// still resolve to $HOME, and the field order must not change.
func TestMigrationKeepsServicesJSONBytes(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerAt(dir)
	enabled := true
	defs := []ServiceDefinition{
		{
			ID:          "svc-web",
			Name:        "web",
			Command:     "npm run dev",
			WorkingDir:  "/tmp/apps/web",
			ExtraEnv:    map[string]string{"NODE_ENV": "development", "API_URL": "http://127.0.0.1:8080"},
			ExtraPath:   "/opt/homebrew/bin:/usr/local/bin",
			Enabled:     &enabled,
			PortForward: &ServicePortForward{Port: 3000, Label: "web", Provider: "cloudflare", Subdomain: "web"},
		},
		{
			ID:      "svc-api",
			Name:    "api",
			Command: "go run ./cmd/api",
		},
		{
			ID:                  "svc-tiny",
			Name:                "tiny",
			Command:             "sleep 1",
			UpgradeTarget:       "/tmp/bin/tiny",
			UpgradePreStopCmds:  []string{"echo pre"},
			UpgradePostStopCmds: []string{"echo post"},
		},
	}
	for _, def := range defs {
		if _, err := m.createOrUpdate(def, false); err != nil {
			t.Fatalf("createOrUpdate(%s) error = %v", def.ID, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "services.json"))
	if err != nil {
		t.Fatalf("read services.json: %v", err)
	}
	got := scrubServicesJSON(t, string(data))
	if got != servicesJSONGolden {
		t.Fatalf("services.json changed:\n--- got ---\n%s\n--- want ---\n%s", got, servicesJSONGolden)
	}
}

func scrubServicesJSON(t *testing.T, body string) string {
	t.Helper()
	ts := regexp.MustCompile(`"(createdAt|updatedAt)": "[^"]*"`)
	body = ts.ReplaceAllString(body, `"$1": "<ts>"`)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if home != "" {
		body = strings.ReplaceAll(body, home, "<home>")
	}
	return body
}
