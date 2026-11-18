package Browser

// A fresh profile is the cheapest way for a site to distrust this browser: no
// cookie history, no login, no purchase record — exactly the shape of traffic
// fraud filters exist for, and no amount of human-like input after takeover
// repairs a session that was born anonymous. The fix that actually moves the
// needle is identity: browsing from a profile the site has already learned to
// trust. This file copies the user's own Chrome profile into the agent's
// dedicated one — cookies, Local State (which carries the cookie encryption
// key), logins, and the aged history that makes a checkout look like a returning
// customer — while skipping the megabytes of cache Chrome rebuilds anyway and
// the lock files of a Chrome that is still running.
//
// It deliberately copies rather than points --user-data-dir at the real
// profile: Chrome refuses a second debugging port on a profile another
// instance owns, and a shared live profile would let the agent's actions write
// into the user's own browsing history.

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// importSkip names what is never copied from a real profile: caches Chrome
// rebuilds on first launch, and the files a running Chrome holds or rewrites.
var importSkip = map[string]bool{
	"cache":              true,
	"code cache":         true,
	"gpucache":           true,
	"dawncache":          true,
	"dawngraphitecache":  true,
	"dawnwebgpucache":    true,
	"grshadercache":      true,
	"graphitedawncache":  true,
	"shadercache":        true,
	"cachestorage":       true,
	"scriptcache":        true,
	"sessions":           true, // rebuilds from the copy's own prefs; stale tabs are noise
	"singletoncookie":    true,
	"singletonlock":      true,
	"singletonsocket":    true,
	"lock":               true,
	"lockfile":           true,
	"devtoolsactiveport": true,
}

// skipImport reports whether a path component inside a profile is skipped.
// "Singleton*" matches with any suffix; everything else is an exact
// case-insensitive name.
func skipImport(name string) bool {
	lower := strings.ToLower(name)
	if importSkip[lower] {
		return true
	}
	return strings.HasPrefix(lower, "singleton")
}

// defaultUserDataDirs lists where Chromium-family browsers keep their profiles,
// Google Chrome first: its profile is the one whose cookies decrypt where the
// user expects them to.
func defaultUserDataDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		return []string{
			filepath.Join(base, "Google", "Chrome"),
			filepath.Join(base, "Chromium"),
			filepath.Join(base, "Microsoft Edge"),
		}
	case "windows":
		base := os.Getenv("LocalAppData")
		if base == "" {
			return nil
		}
		return []string{
			filepath.Join(base, "Google", "Chrome", "User Data"),
			filepath.Join(base, "Chromium", "User Data"),
			filepath.Join(base, "Microsoft", "Edge", "User Data"),
		}
	default:
		base := filepath.Join(home, ".config")
		return []string{
			filepath.Join(base, "google-chrome"),
			filepath.Join(base, "google-chrome-beta"),
			filepath.Join(base, "chromium"),
			filepath.Join(base, "microsoft-edge"),
			filepath.Join(base, "BraveSoftware", "Brave-Browser"),
		}
	}
}

// profileInfo mirrors the part of Local State that names profiles.
type profileInfo struct {
	Profile struct {
		InfoCache map[string]struct {
			Name string `json:"name"`
		} `json:"info_cache"`
	} `json:"profile"`
}

// resolveUserProfile finds the user-data-dir and profile directory to import.
// An empty name means Chrome's first profile ("Default", the one the user
// actually browses in); otherwise the name matches a directory or its display
// name from Local State, case-insensitively.
func resolveUserProfile(name string) (root, dir, display string, err error) {
	want := strings.ToLower(strings.TrimSpace(name))
	var lastErr error
	for _, root = range defaultUserDataDirs() {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			lastErr = readErr
			continue
		}
		// Local State maps directory names to the display names the user sees.
		names := map[string]string{}
		if raw, err := os.ReadFile(filepath.Join(root, "Local State")); err == nil {
			var state profileInfo
			if json.Unmarshal(raw, &state) == nil {
				for d, info := range state.Profile.InfoCache {
					display := strings.TrimSpace(info.Name)
					if display == "" {
						display = d
					}
					names[strings.ToLower(d)] = display
					if !strings.EqualFold(display, d) {
						names[strings.ToLower(display)] = display
					}
				}
			}
		}
		var fallback string
		for _, entry := range entries {
			if !entry.IsDir() || !isProfileDirName(entry.Name()) {
				continue
			}
			known, ok := names[strings.ToLower(entry.Name())]
			if !ok {
				known = entry.Name()
			}
			if want != "" &&
				(want == strings.ToLower(entry.Name()) || want == strings.ToLower(known)) {
				return root, entry.Name(), known, nil
			}
			// No name given: the first conventional profile wins, and "Default"
			// beats "Profile 1" in the order Chrome itself creates them.
			if fallback == "" || (strings.EqualFold(entry.Name(), "default") && !strings.EqualFold(fallback, "default")) {
				fallback = entry.Name()
			}
		}
		if want == "" && fallback != "" {
			known, ok := names[strings.ToLower(fallback)]
			if !ok {
				known = fallback
			}
			return root, fallback, known, nil
		}
		if want != "" {
			lastErr = fmt.Errorf("no profile named %q in %s", name, root)
		}
	}
	if lastErr != nil {
		return "", "", "", fmt.Errorf("browser: could not find a Chrome profile to import: %v", lastErr)
	}
	return "", "", "", fmt.Errorf("browser: no Chrome profile found to import — looked in %s",
		strings.Join(defaultUserDataDirs(), ", "))
}

// isProfileDirName recognises the directories Chrome keeps profiles in, so a
// random folder in the user-data-dir is never mistaken for one.
func isProfileDirName(name string) bool {
	lower := strings.ToLower(name)
	if lower == "default" {
		return true
	}
	return strings.HasPrefix(lower, "profile ")
}

// copyProfile copies one profile directory into dest/Default, skipping caches
// and locks. Files a running Chrome holds are reported, not fatal: on Linux
// almost nothing is locked, and the copy is still a usable identity.
func copyProfile(root, dir, dest string) (files int, bytes int64, skipped []string, err error) {
	src := filepath.Join(root, dir)
	target := filepath.Join(dest, "Default")
	if err := os.MkdirAll(target, 0o700); err != nil {
		return 0, 0, nil, fmt.Errorf("browser: could not create the import target: %w", err)
	}
	// Local State carries the cookie encryption key on Linux and Windows; without
	// it the copied Cookies file is unreadable and the import is worthless.
	localState := filepath.Join(root, "Local State")
	if raw, err := os.ReadFile(localState); err == nil {
		if writeErr := os.WriteFile(filepath.Join(dest, "Local State"), raw, 0o600); writeErr != nil {
			return 0, 0, nil, fmt.Errorf("browser: could not copy Local State: %w", writeErr)
		}
	}

	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			skipped = append(skipped, filepath.Base(path))
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		for _, part := range parts {
			if skipImport(part) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		target := filepath.Join(target, rel)
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err == nil {
				_ = os.Symlink(link, target)
			}
			return nil
		}
		if d.IsDir() {
			if mkErr := os.MkdirAll(target, 0o700); mkErr != nil {
				skipped = append(skipped, rel)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		n, copyErr := copyFile(path, target, 0o600)
		if copyErr != nil {
			skipped = append(skipped, rel)
			return nil
		}
		files++
		bytes += n
		return nil
	})
	if walkErr != nil {
		return files, bytes, skipped, walkErr
	}
	return files, bytes, skipped, nil
}

// copyFile copies one regular file, creating parent directories as needed.
func copyFile(src, dest string, mode fs.FileMode) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return n, err
	}
	if closeErr != nil {
		return n, closeErr
	}
	if err := os.Chmod(dest, mode); err != nil {
		return n, err
	}
	return n, nil
}

// importProfile replaces the agent's browser profile with a copy of the user's
// real Chrome profile. Callers hold opMu and have already stopped Chrome: a
// profile being copied while Chrome runs in it is corruption, not a race to
// recover from.
func (s *Service) importProfile(name string) (string, error) {
	root, dir, display, err := resolveUserProfile(name)
	if err != nil {
		return "", err
	}
	// Belt and braces: the source must never be the agent profile itself, even
	// when the environment points ARGUS_BROWSER_PROFILE_DIR at real Chrome.
	if dest, err := profileDir(); err == nil {
		if sameDir(dest, root) {
			return "", fmt.Errorf("browser: the agent profile already is %s — refusing to import it into itself", root)
		}
	}
	if err := removeProfile(); err != nil {
		return "", err
	}
	dest, err := profileDir()
	if err != nil {
		return "", err
	}
	files, bytes, skipped, err := copyProfile(root, dir, dest)
	if err != nil {
		return "", err
	}
	if files == 0 {
		return "", fmt.Errorf("browser: nothing could be copied from %s — is that Chrome running with the profile locked?", filepath.Join(root, dir))
	}
	note := fmt.Sprintf("imported the Chrome profile %q (%s) into the agent browser: %d files, %s. "+
		"The next open starts with those cookies and logins; sites that know this user now see a returning visitor.",
		display, filepath.Join(root, dir), files, humanBytes(bytes))
	if len(skipped) > 0 {
		note += fmt.Sprintf(" Skipped %d locked or cached files (Chrome rebuilds them): %s.",
			len(skipped), shorten(strings.Join(skipped[:min(5, len(skipped))], ", "), 120))
	}
	return note, nil
}

// sameDir answers whether two paths name the same directory, tolerating
// symlinks and trailing separators.
func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	abs := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return resolved
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		return abs
	}
	return abs(a) == abs(b)
}

// humanBytes renders a size the way a user reads it.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	units := []string{"KB", "MB", "GB"}
	for i := range units {
		if value < unit {
			return fmt.Sprintf("%.0f %s", value, units[i])
		}
		value /= unit
	}
	return fmt.Sprintf("%.1f GB", value)
}
