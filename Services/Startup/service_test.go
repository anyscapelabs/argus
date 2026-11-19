package Startup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallablePathSkipsEphemeralLocations(t *testing.T) {
	if installablePath("/Volumes/Argus/Argus Agent.app/Contents/MacOS/argus") {
		t.Fatal("a mounted DMG must not be registered as a login item")
	}
	if installablePath(filepath.Join(os.TempDir(), "argus")) {
		t.Fatal("a temporary build path must not be registered as a login item")
	}
	if !installablePath("/Applications/Argus Agent.app/Contents/MacOS/argus") {
		t.Fatal("an installed app must be registrable")
	}
	if !installablePath("/home/user/.local/bin/argus") {
		t.Fatal("a user-local install must be registrable")
	}
}

func TestEnableWithoutAManagerIsANoOp(t *testing.T) {
	if err := Enable(nil); err != nil {
		t.Fatalf("Enable(nil) = %v, want nil", err)
	}
}
