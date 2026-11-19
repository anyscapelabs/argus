package Skills

import (
	"fmt"
	"regexp"
	"time"
)

const (
	IndexSchema    = "argus-skills/v1"
	maxIndexBytes  = 8 << 20
	maxBundleBytes = 256 << 10
	maxDescription = 1200
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	versionPattern  = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([\-+][0-9A-Za-z.\-]+)?$`)
	versionPrefixRe = regexp.MustCompile(`^[vV]`)
	versionSuffixRe = regexp.MustCompile(`[\-+]`)
)

// SkillMeta is the Level-1 record: one line in the system prompt, the index
// entry used by search_skills, and the integrity data for its Level-2 bundle.
type SkillMeta struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Connectors    []string `json:"connectors"`
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	AutoEnable    bool     `json:"auto_enable"`
	Version       string   `json:"version,omitempty"`
	MinAppVersion string   `json:"min_app_version,omitempty"`
	SHA256        string   `json:"sha256,omitempty"`
	Size          int64    `json:"size,omitempty"`
	Channel       string   `json:"channel,omitempty"`
	Publisher     string   `json:"publisher,omitempty"`
	Revoked       bool     `json:"revoked,omitempty"`
	ReplacedBy    string   `json:"replaced_by,omitempty"`
}

type SkillDoc struct {
	Meta SkillMeta `json:"meta"`
	Body string    `json:"body"`
}

// Index is the signed manifest published to the CDN. The signature covers the
// exact bytes of the file it ships next to; bundles are verified by sha256.
type Index struct {
	Schema      string      `json:"schema"`
	GeneratedAt string      `json:"generated_at"`
	Skills      []SkillMeta `json:"skills"`
}

// Bundle is an immutable Level-2 document addressed by <id>@<version>.
type Bundle struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Body    string `json:"body"`
}

func (idx Index) Validate() error {
	if idx.Schema != IndexSchema {
		return fmt.Errorf("unsupported index schema %q", idx.Schema)
	}
	if _, err := time.Parse(time.RFC3339, idx.GeneratedAt); err != nil {
		return fmt.Errorf("invalid generated_at: %w", err)
	}
	seen := map[string]bool{}
	for _, m := range idx.Skills {
		if !idPattern.MatchString(m.ID) {
			return fmt.Errorf("invalid skill id %q", m.ID)
		}
		if seen[m.ID] {
			return fmt.Errorf("duplicate skill id %q", m.ID)
		}
		seen[m.ID] = true
		if m.Name == "" || m.Description == "" || len(m.Description) > maxDescription {
			return fmt.Errorf("skill %q has an invalid name or description", m.ID)
		}
		if m.Version != "" && !versionPattern.MatchString(m.Version) {
			return fmt.Errorf("skill %q has an invalid version %q", m.ID, m.Version)
		}
		if len(m.SHA256) != 0 && len(m.SHA256) != 64 {
			return fmt.Errorf("skill %q has an invalid sha256", m.ID)
		}
		if m.Size < 0 || m.Size > maxBundleBytes {
			return fmt.Errorf("skill %q has an invalid bundle size", m.ID)
		}
		if m.Channel != "" && m.Channel != "stable" && m.Channel != "beta" {
			return fmt.Errorf("skill %q has an invalid channel %q", m.ID, m.Channel)
		}
		if m.MinAppVersion != "" && !versionPattern.MatchString(m.MinAppVersion) {
			return fmt.Errorf("skill %q has an invalid min_app_version", m.ID)
		}
	}
	return nil
}

// GeneratedTime parses the index timestamp; an invalid value fails validation
// before this is called.
func (idx Index) GeneratedTime() time.Time {
	t, _ := time.Parse(time.RFC3339, idx.GeneratedAt)
	return t.UTC()
}

// CompareVersions orders two semver-like strings; "" sorts before everything.
func CompareVersions(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	an := parseVersion(a)
	bn := parseVersion(b)
	for i := 0; i < 3; i++ {
		if an[i] < bn[i] {
			return -1
		}
		if an[i] > bn[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = versionPrefixRe.ReplaceAllString(v, "")
	if i := versionSuffixRe.FindStringIndex(v); i != nil {
		v = v[:i[0]]
	}
	var out [3]int
	for i, part := range splitN(v, '.', 3) {
		if i > 2 {
			break
		}
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}

func splitN(s string, sep byte, n int) []string {
	var out []string
	start := 0
	for i := 0; i < len(s) && len(out) < n-1; i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
