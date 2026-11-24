package Browser

// The import is the one feature here that copies personal data, so its tests
// lean on the two properties that matter: only the agent's own profile is ever
// written, and nothing the agent later injects into a page is visible in that
// page's DOM.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkipImportMatchesCachesAndLocks(t *testing.T) {
	skipped := []string{"Cache", "Code Cache", "GPUCache", "SingletonLock", "lockfile", "DevToolsActivePort", "ShaderCache"}
	for _, name := range skipped {
		if !skipImport(name) {
			t.Errorf("skipImport(%q) = false, want true", name)
		}
	}
	kept := []string{"Cookies", "Preferences", "Login Data", "History", "Local State", "Bookmarks"}
	for _, name := range kept {
		if skipImport(name) {
			t.Errorf("skipImport(%q) = true, want false — that file is the identity being imported", name)
		}
	}
}

// fakeUserChrome builds a user-data-dir shaped enough for resolveUserProfile and
// copyProfile: a Local State naming one profile, that profile's identity files,
// and the caches the import must leave behind. It returns the home directory it
// created the profile under, since defaultUserDataDirs derives from home.
func fakeUserChrome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".config", "google-chrome")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	state := `{"profile":{"info_cache":{"Default":{"name":"Personal"}}}}`
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "Default")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Cookies", "Preferences", "Login Data"} {
		if err := os.WriteFile(filepath.Join(profile, name), []byte("identity:"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(profile, "Cache", "entries")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "blob"), []byte("big"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte("9222\n/devtools/browser/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// setHome points the discovery path at a fixture and reports it.
func setHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
}

func TestResolveUserProfileFindsDefaultAndByName(t *testing.T) {
	setHome(t, fakeUserChrome(t))

	_, dir, display, err := resolveUserProfile("")
	if err != nil {
		t.Fatalf("resolveUserProfile(\"\"): %v", err)
	}
	if dir != "Default" || display != "Personal" {
		t.Errorf("unnamed import resolved to %q (%q), want Default (Personal)", dir, display)
	}

	_, dir, display, err = resolveUserProfile("personal")
	if err != nil {
		t.Fatalf("resolveUserProfile(\"personal\"): %v", err)
	}
	if dir != "Default" || display != "Personal" {
		t.Errorf("import by display name resolved to %q (%q), want Default (Personal)", dir, display)
	}

	if _, _, _, err := resolveUserProfile("no-such-profile"); err == nil {
		t.Error("an unknown profile name resolved instead of failing")
	}
}

func TestImportProfileCopiesIdentityAndSkipsCaches(t *testing.T) {
	setHome(t, fakeUserChrome(t))
	t.Setenv(ProfileDirEnv, t.TempDir())

	var service Service
	note, err := service.importProfile("")
	if err != nil {
		t.Fatalf("importProfile: %v", err)
	}
	if !strings.Contains(note, `"Personal"`) || !strings.Contains(note, "logins") {
		t.Errorf("the import note does not say what happened: %s", note)
	}

	dir, err := profileDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Cookies", "Preferences", "Login Data"} {
		raw, err := os.ReadFile(filepath.Join(dir, "Default", name))
		if err != nil {
			t.Fatalf("the imported profile lost %s: %v", name, err)
		}
		if string(raw) != "identity:"+name {
			t.Errorf("%s was copied with the wrong content: %q", name, raw)
		}
	}
	// Local State carries the cookie encryption key, so it belongs at the
	// user-data-dir root, where Chrome will look for it.
	if raw, err := os.ReadFile(filepath.Join(dir, "Local State")); err != nil {
		t.Fatalf("the imported profile lost Local State: %v", err)
	} else if !strings.Contains(string(raw), "info_cache") {
		t.Errorf("Local State was copied with the wrong content: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "Default", "Cache")); !os.IsNotExist(err) {
		t.Error("the Cache directory was copied; only identity should cross")
	}
	if _, err := os.Stat(filepath.Join(dir, "Default", "DevToolsActivePort")); !os.IsNotExist(err) {
		t.Error("DevToolsActivePort was copied; a stale port file would poison the next launch")
	}
	// The marker is what lets a later removeProfile wipe this directory, so the
	// import must have preserved it.
	if _, err := os.Stat(filepath.Join(dir, profileMarker)); err != nil {
		t.Errorf("the imported profile is unmarked: %v", err)
	}
}

func TestImportProfileRefusesAnUnknownProfile(t *testing.T) {
	setHome(t, fakeUserChrome(t))
	t.Setenv(ProfileDirEnv, t.TempDir())

	var service Service
	if _, err := service.importProfile("Work"); err == nil {
		t.Fatal("importing a profile that does not exist succeeded")
	}
}

// TestSnapshotLeavesNoTraceInTheDOM is the regression guard for the read-only
// snapshot: the page pass must never write an attribute, class or style into
// the page it observes, because a bot check that inspects its own DOM would
// see them. Identity lives in JavaScript maps on window instead.
func TestSnapshotLeavesNoTraceInTheDOM(t *testing.T) {
	for _, marker := range []string{"setAttribute", "removeAttribute", "data-af-", "className =", "style."} {
		if strings.Contains(snapshotScript, marker) {
			t.Errorf("the snapshot script mutates the page: it contains %q", marker)
		}
	}
	// The maps the action path resolves through must still be there.
	for _, needed := range []string{"__argus_refs__", "__argus_visited__", "__argus_idx__", "__argus_reassign__"} {
		if !strings.Contains(snapshotScript, needed) {
			t.Errorf("the snapshot script lost %q; refs could not resolve", needed)
		}
	}
}

// TestSignatureScriptDoesNotReadOurMarkers pins the same rule on the freshness
// signature: it once counted the data-af-ref attributes the snapshot stamped;
// now that the snapshot writes nothing, the signature reads only the page.
func TestSignatureScriptDoesNotReadOurMarkers(t *testing.T) {
	if strings.Contains(signatureScript, "data-af") {
		t.Error("the signature still reads the removed ref attributes")
	}
	if !strings.Contains(signatureScript, "getElementsByTagName") {
		t.Error("the signature should count elements cheaply")
	}
}

// TestLocalStateShapeMatchesChrome keeps the import honest about the file it
// parses: if Chrome renames the profile cache, the import must fail loudly in
// review rather than silently copying nothing.
func TestLocalStateShapeMatchesChrome(t *testing.T) {
	var state profileInfo
	if err := json.Unmarshal([]byte(`{"profile":{"info_cache":{"Default":{"name":"Personal"}}}}`), &state); err != nil {
		t.Fatalf("Local State shape no longer parses: %v", err)
	}
	if state.Profile.InfoCache["Default"].Name != "Personal" {
		t.Errorf("info_cache name came back %q", state.Profile.InfoCache["Default"].Name)
	}
}
