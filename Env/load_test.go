package Env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsDotEnvAtStartup(t *testing.T) {
	if !allowed() {
		t.Skip("production builds ignore .env unless explicitly enabled")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("ARGUS_TEST_ENV=loaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("ARGUS_TEST_ENV", "")
	Load()
	if got := os.Getenv("ARGUS_TEST_ENV"); got != "loaded" {
		t.Fatalf("value = %q, want loaded", got)
	}
}
