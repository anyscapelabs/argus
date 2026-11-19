package Shared

import "os"

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

// ConfigValue resolves a configuration key from the runtime environment first,
// then from the value baked into the binary at build time.
func ConfigValue(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if p := buildValues[key]; p != nil {
		return *p
	}
	return ""
}
