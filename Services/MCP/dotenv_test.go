package MCP

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGithubCredsResolveFromEnv(t *testing.T) {
	t.Setenv("ARGUS_GITHUB_CLIENT_ID", "dummy-id")
	t.Setenv("ARGUS_GITHUB_CLIENT_SECRET", "dummy-secret")
	creds, err := githubClient()
	if err != nil {
		t.Fatalf("githubClient: %v", err)
	}
	if creds.ClientID != "dummy-id" {
		t.Fatalf("client id = %q, want dummy-id", creds.ClientID)
	}
	if creds.ClientSecretAuth == nil || creds.ClientSecretAuth.ClientSecret != "dummy-secret" {
		t.Fatal("github client secret was not resolved")
	}
}

func TestLoadDotEnvFromCheckout(t *testing.T) {
	if !devDotEnvEnabled() {
		t.Skip("production builds ignore .env")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ARGUS_TEST_DOTENV=loaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("ARGUS_TEST_DOTENV", "")
	loadDotEnv()
	if got := os.Getenv("ARGUS_TEST_DOTENV"); got != "loaded" {
		t.Fatalf("dotenv value = %q, want loaded", got)
	}
}
