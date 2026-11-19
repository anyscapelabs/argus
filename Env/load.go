// Package Env loads local .env files once at process startup. Services only
// read the resulting environment; none of them parse dotenv files.
package Env

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func Load() {
	if !allowed() {
		return
	}
	paths := []string{".env", ".env.local", "config/.env", "../.env", "../../.env"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, ".env"),
			filepath.Join(dir, "..", ".env"),
			filepath.Join(dir, "..", "..", ".env"),
		)
	}
	for _, p := range paths {
		loadFile(p)
	}
}

func loadFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, "=")
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		value := strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
