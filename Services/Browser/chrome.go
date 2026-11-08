package Browser

// Chrome is found the same way the PDF exporter finds it: probe a list of names
// and absolute paths and take the first that resolves. The launcher owns one
// dedicated instance with its own profile, which is what makes a debug port
// available at all — Chrome ignores --remote-debugging-port when it would have
// to share a profile with an already-running instance.
import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// profileDirName is where the agent's cookies, logins and cache live. Separate
	// from the user's own Chrome on purpose: the agent signs in as itself, and a
	// wiped agent profile never touches anyone's real browser.
	profileDirName = "browser"
)

const (
	// HeadlessEnv forces headless mode. Headed is the default because a visible
	// Chrome passes far fewer bot checks and the user can take over a page for a
	// sign-in or a CAPTCHA.
	HeadlessEnv = "ARGUS_BROWSER_HEADLESS"
	// ChromePathEnv overrides binary discovery entirely.
	ChromePathEnv = "ARGUS_CHROME_PATH"
)

// binaryTool mirrors the shape Services/Artifacts uses for optional external
// binaries, so both features explain themselves the same way.
type binaryTool struct {
	Label    string
	Install  string
	Probe    []string
	Required string
}

var chromeTool = binaryTool{
	Label:   "Chrome",
	Install: "Google Chrome (https://www.google.com/chrome/) or Chromium",
	Probe: []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"microsoft-edge", "microsoft-edge-stable", "brave-browser",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
		"/opt/google/chrome/chrome",
		"/snap/bin/chromium",
		"/var/lib/flatpak/exports/bin/org.chromium.Chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	},
	Required: "Browsing needs Chrome or Chromium on this machine.",
}

// findChrome returns the browser binary to drive, or "" when this machine has
// none. An explicit ARGUS_CHROME_PATH wins even when it is wrong, so a
// misconfiguration fails loudly instead of quietly picking something else.
func findChrome() string {
	if override := strings.TrimSpace(os.Getenv(ChromePathEnv)); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override
		}
		return ""
	}
	for _, candidate := range chromeTool.Probe {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

// ProfileDirEnv relocates the agent profile. The test suite uses it to keep a
// browser experiment out of the user's real config, and a user with a small home
// partition can point it somewhere roomier.
const ProfileDirEnv = "ARGUS_BROWSER_PROFILE_DIR"

// profileDir is the agent's own browser profile, next to the database.
func profileDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv(ProfileDirEnv)); override != "" {
		if err := os.MkdirAll(override, 0o700); err != nil {
			return "", fmt.Errorf("browser: could not create the profile dir: %w", err)
		}
		return override, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("browser: no writable config directory: %w", err)
	}
	dir := filepath.Join(base, "argus", profileDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("browser: could not create the profile dir: %w", err)
	}
	return dir, nil
}

// Headless reports whether the environment asks for a headless browser. Only an
// explicit 1/true/on counts, so a typo cannot silently change the mode.
func Headless() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(HeadlessEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Available answers two questions the UI needs: is there a browser to drive, and
// what do we say when there is not.
func Available() (bool, string) {
	if findChrome() != "" {
		return true, ""
	}
	return false, chromeTool.Required + " Install it, or point ARGUS_CHROME_PATH at the binary."
}

// removeProfile wipes the agent's browser data. Only ever called on an explicit
// reset: it is how a stuck login or a bad cookie is cleared without touching the
// user's own browser.
func removeProfile() error {
	dir, err := profileDir()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("browser: could not clear the profile: %w", err)
	}
	return nil
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
