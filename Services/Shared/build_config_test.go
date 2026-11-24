package Shared

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetDotEnv(t *testing.T) {
	t.Helper()
	dotEnvOnce = sync.Once{}
}

func TestConfigValuePrefersEnvOverBaked(t *testing.T) {
	resetDotEnv(t)
	t.Setenv("ARGUS_LOAD_DOTENV", "")
	old := BuildGithubClientID
	BuildGithubClientID = "baked-id"
	t.Cleanup(func() { BuildGithubClientID = old })

	t.Setenv("ARGUS_GITHUB_CLIENT_ID", "")
	if got := ConfigValue("ARGUS_GITHUB_CLIENT_ID"); got != "baked-id" {
		t.Fatalf("baked value not used: got %q", got)
	}
	t.Setenv("ARGUS_GITHUB_CLIENT_ID", "env-id")
	if got := ConfigValue("ARGUS_GITHUB_CLIENT_ID"); got != "env-id" {
		t.Fatalf("env should win over baked: got %q", got)
	}
}

func TestDotEnvIgnoredUnlessOptedIn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ARGUS_GITHUB_CLIENT_ID=from-dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("ARGUS_GITHUB_CLIENT_ID", "")
	t.Setenv("ARGUS_LOAD_DOTENV", "")
	old := BuildGithubClientID
	BuildGithubClientID = ""
	t.Cleanup(func() { BuildGithubClientID = old })

	resetDotEnv(t)
	if got := ConfigValue("ARGUS_GITHUB_CLIENT_ID"); got != "" {
		t.Fatalf(".env must not load without ARGUS_LOAD_DOTENV=1, got %q", got)
	}
}

func TestDotEnvLoadedWhenOptedIn(t *testing.T) {
	dir := t.TempDir()
	body := "# comment\nexport ARGUS_GITHUB_CLIENT_ID=\"from-dotenv\"\nARGUS_GITHUB_CLIENT_SECRET='s3cret'\nARGUS_PUBLIC_URL=http://localhost:9245\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("ARGUS_GITHUB_CLIENT_ID", "")
	t.Setenv("ARGUS_GITHUB_CLIENT_SECRET", "")
	t.Setenv("ARGUS_PUBLIC_URL", "preset")
	t.Setenv("ARGUS_LOAD_DOTENV", "1")

	resetDotEnv(t)
	if got := ConfigValue("ARGUS_GITHUB_CLIENT_ID"); got != "from-dotenv" {
		t.Fatalf("client id: got %q", got)
	}
	if got := ConfigValue("ARGUS_GITHUB_CLIENT_SECRET"); got != "s3cret" {
		t.Fatalf("client secret: got %q", got)
	}
	if got := ConfigValue("ARGUS_PUBLIC_URL"); got != "preset" {
		t.Fatalf("real env must win over .env, got %q", got)
	}
}
