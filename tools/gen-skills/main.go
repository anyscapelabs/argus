// Command gen-skills regenerates the embedded offline baseline from skills/.
// It is the single source of truth for both the app and the publishing job.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anyscapelabs/argus/Services/Skills"
)

func main() {
	root, err := findRoot()
	if err != nil {
		fail(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "skills"))
	if err != nil {
		fail(err)
	}
	outDir := filepath.Join(root, "Services", "Skills", "data")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}
	metas := []Skills.SkillMeta{}
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "third_party" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, "skills", e.Name(), "SKILL.md"))
		if err != nil {
			fail(err)
		}
		body := raw
		var meta Skills.SkillMeta
		metaPath := filepath.Join(root, "skills", e.Name(), "skill.json")
		if encodedMeta, err := os.ReadFile(metaPath); err == nil {
			if err := json.Unmarshal(encodedMeta, &meta); err != nil {
				fail(fmt.Errorf("%s: invalid skill.json: %w", e.Name(), err))
			}
		} else {
			fm, frontBody, err := split(raw)
			if err != nil {
				fail(fmt.Errorf("%s: %w", e.Name(), err))
			}
			body = frontBody
			if err := json.Unmarshal(fm, &meta); err != nil {
				fail(fmt.Errorf("%s: invalid frontmatter: %w", e.Name(), err))
			}
		}
		if meta.ID != e.Name() || seen[meta.ID] || meta.Description == "" || strings.TrimSpace(string(body)) == "" {
			fail(fmt.Errorf("%s: id, description or body is invalid", e.Name()))
		}
		seen[meta.ID] = true
		metas = append(metas, meta)
		target := filepath.Join(outDir, meta.ID)
		if err := os.MkdirAll(target, 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(target, "SKILL.md"), raw, 0o644); err != nil {
			fail(err)
		}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].ID < metas[j].ID })
	encoded, err := json.MarshalIndent(metas, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "skills.json"), append(encoded, '\n'), 0o644); err != nil {
		fail(err)
	}
	existing, _ := os.ReadDir(outDir)
	for _, e := range existing {
		if e.IsDir() && !seen[e.Name()] {
			_ = os.RemoveAll(filepath.Join(outDir, e.Name()))
		}
	}
	fmt.Printf("regenerated %d embedded skills\n", len(metas))
}

func split(raw []byte) ([]byte, []byte, error) {
	text := strings.TrimLeft(string(raw), "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		return nil, nil, fmt.Errorf("missing JSON frontmatter")
	}
	rest := text[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, nil, fmt.Errorf("unterminated frontmatter")
	}
	return []byte(strings.TrimSpace(rest[:idx])), []byte(strings.TrimSpace(rest[idx+4:])), nil
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "skills")); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("cannot find repository root from %s", dir)
		}
		dir = parent
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-skills:", err)
	os.Exit(1)
}
