// Package Startup keeps Argus available when the user is not looking at it.
//
// The app registers itself to launch when the user signs in, so routines and
// reminders keep running without anyone opening a window first, and the login
// launch passes LaunchAtLoginFlag so main.go can open the window hidden — the
// app waits in the system tray until it is asked for.
package Startup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// LaunchAtLoginFlag is appended to the executable path in the login item.
// main.go reads it at startup and opens the window hidden.
const LaunchAtLoginFlag = "--background"

// Identifier names the registration on every platform (reverse-DNS so it reads
// as the app on macOS and matches the bundle id).
const Identifier = "com.anyscapelabs.argus"

// Enable registers the app to launch at login. Re-registering is safe and
// overwrites the previous entry, which repairs a registration whose executable
// path moved (an update, a new install location).
//
// A path that will not exist at the next sign-in is left alone: a DMG mount on
// macOS and a temporary/dev build directory both disappear, and registering
// them would only leave a broken login item behind.
func Enable(autostart *application.AutostartManager) error {
	if autostart == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !installablePath(exe) {
		return nil
	}
	return autostart.EnableWithOptions(application.AutostartOptions{
		Identifier: Identifier,
		Arguments:  []string{LaunchAtLoginFlag},
	})
}

// installablePath reports whether exe looks like a durable install rather than
// a mounted disk image or a temporary build.
func installablePath(exe string) bool {
	clean := filepath.Clean(exe)
	if strings.HasPrefix(clean, "/Volumes/") {
		return false
	}
	if tmp := filepath.Clean(os.TempDir()); tmp != "" && strings.HasPrefix(clean, tmp+string(filepath.Separator)) {
		return false
	}
	return true
}
