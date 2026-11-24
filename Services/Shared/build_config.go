package Shared

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

// Build-time configuration. These variables are set by the release workflow
// with `go build -ldflags -X` from GitHub Actions secrets/variables; no value
// is ever committed to source. Runtime environment variables still win, so a
// developer or a staged build can override anything without recompiling.
var (
	BuildGoogleOAuthClientID     string
	BuildGoogleOAuthClientSecret string
	BuildArgusGoogleClientID     string
	BuildArgusGoogleClientSecret string
	BuildGithubClientID          string
	BuildGithubClientSecret      string
	BuildPublicURL               string
)

var buildValues = map[string]*string{
	"GOOGLE_OAUTH_CLIENT_ID":     &BuildGoogleOAuthClientID,
	"GOOGLE_OAUTH_CLIENT_SECRET": &BuildGoogleOAuthClientSecret,
	"ARGUS_GOOGLE_CLIENT_ID":     &BuildArgusGoogleClientID,
	"ARGUS_GOOGLE_CLIENT_SECRET": &BuildArgusGoogleClientSecret,
	"ARGUS_GITHUB_CLIENT_ID":     &BuildGithubClientID,
	"ARGUS_GITHUB_CLIENT_SECRET": &BuildGithubClientSecret,
	"ARGUS_PUBLIC_URL":           &BuildPublicURL,
}

// ConfigValue resolves a configuration key from the runtime environment first
// (including a development .env when ARGUS_LOAD_DOTENV=1), then from the value
// baked into the binary at build time.
func ConfigValue(key string) string {
	LoadDotEnv()
	if v := os.Getenv(key); v != "" {
		return v
	}
	if p := buildValues[key]; p != nil {
		return *p
	}
	return ""
}

var dotEnvOnce sync.Once

// dotEnvPaths are searched relative to the working directory. `task dev` and
// `wails3 dev` run from the repo root, so ".env" is the usual hit.
var dotEnvPaths = []string{".env", ".env.local", "config/.env", "../.env"}

// LoadDotEnv copies KEY=VALUE pairs from a development .env into the process
// environment, once. It is OFF unless ARGUS_LOAD_DOTENV=1, so a packaged app
// never picks up a developer's secrets from whatever directory it was launched
// in. Variables already set in the real environment always win.
func LoadDotEnv() {
	dotEnvOnce.Do(func() {
		if os.Getenv("ARGUS_LOAD_DOTENV") != "1" {
			return
		}
		for _, p := range dotEnvPaths {
			f, err := os.Open(p)
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				line = strings.TrimPrefix(line, "export ")
				i := strings.Index(line, "=")
				if i <= 0 {
					continue
				}
				k := strings.TrimSpace(line[:i])
				v := strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
				if k != "" && os.Getenv(k) == "" {
					os.Setenv(k, v)
				}
			}
			f.Close()
		}
	})
}
