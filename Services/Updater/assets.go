package Updater

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// AssetMatcher picks the one release asset this installation can actually
// replace. The framework's default matcher looks for the Go platform name
// ("darwin") in the filename, but the release workflow labels macOS builds
// "macos"; it also cannot tell a raw binary from an AppImage or a package, and
// the updater can only swap the artifact it is running from.
//
// It is exported so main.go can hand it to github.Config as AssetMatcher.
func AssetMatcher(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	// A running AppImage must be replaced by the next AppImage: the mounted
	// binary inside squashfs is read-only, and the framework swaps $APPIMAGE.
	runningAppImage := os.Getenv("APPIMAGE") != ""

	best, bestRank := -1, 1<<30
	for i, a := range assets {
		rank, ok := rankAsset(strings.ToLower(a.Name), strings.ToLower(req.Platform), strings.ToLower(req.Arch), runningAppImage)
		if !ok {
			continue
		}
		if rank < bestRank {
			best, bestRank = i, rank
		}
	}
	return best
}

// rankAsset returns how well name fits the running platform and whether it
// fits at all. Lower is better.
func rankAsset(name, platform, arch string, runningAppImage bool) (int, bool) {
	if isSidecar(name) || !containsPlatform(name, platform) || !containsArch(name, arch) {
		return 0, false
	}
	switch platform {
	case "darwin":
		switch {
		case strings.HasSuffix(name, ".app.zip"):
			return 0, true // the swappable bundle, extracted by the framework
		case strings.HasSuffix(name, ".dmg"):
			return 1, true
		case isBareBinary(name):
			return 2, true
		}
	case "linux":
		appImage := strings.HasSuffix(name, ".appimage")
		switch {
		case runningAppImage && appImage:
			return 0, true
		case !runningAppImage && isBareBinary(name):
			return 0, true
		case appImage:
			return 1, true
		}
	case "windows":
		if strings.HasSuffix(name, ".exe") && !strings.Contains(name, "installer") && !strings.HasSuffix(name, ".msix") {
			return 0, true
		}
	}
	return 0, false
}

// isSidecar reports whether the asset is a signature or checksum file rather
// than a build.
func isSidecar(name string) bool {
	switch {
	case strings.HasSuffix(name, ".sig"), strings.HasSuffix(name, ".asc"):
		return true
	case strings.HasSuffix(name, ".sha256"), strings.HasSuffix(name, ".sha512"):
		return true
	case strings.HasSuffix(name, ".sums"), strings.HasSuffix(name, ".checksum"), strings.HasSuffix(name, ".checksums"):
		return true
	case name == "sha256sums" || strings.HasPrefix(name, "sha256sums."):
		return true
	}
	return false
}

// isBareBinary reports a filename with no archive, package or installer
// extension — the raw executable the release workflow uploads alongside the
// platform packages.
func isBareBinary(name string) bool {
	for _, ext := range []string{
		".appimage", ".deb", ".rpm", ".dmg", ".pkg", ".msix", ".msi",
		".zip", ".gz", ".xz", ".zst", ".tar", ".exe", ".app",
	} {
		if strings.HasSuffix(name, ext) {
			return false
		}
	}
	return true
}

// containsPlatform matches the OS token the release workflow puts in asset
// names ("linux", "macos", "windows") against Go's platform name.
func containsPlatform(name, platform string) bool {
	switch platform {
	case "darwin":
		return strings.Contains(name, "macos") || strings.Contains(name, "darwin") || strings.Contains(name, "osx")
	case "windows":
		return strings.Contains(name, "windows") || strings.Contains(name, "win")
	default:
		return platform == "" || strings.Contains(name, platform)
	}
}

// containsArch mirrors the framework's architecture aliases.
func containsArch(name, arch string) bool {
	switch arch {
	case "":
		return true
	case "amd64":
		return strings.Contains(name, "amd64") || strings.Contains(name, "x86_64") || strings.Contains(name, "x64")
	case "arm64":
		return strings.Contains(name, "arm64") || strings.Contains(name, "aarch64")
	case "386":
		if strings.Contains(name, "x86_64") || strings.Contains(name, "x64") || strings.Contains(name, "amd64") {
			return false
		}
		return strings.Contains(name, "i386") || strings.Contains(name, "ia32") || strings.Contains(name, "x86")
	default:
		return strings.Contains(name, arch)
	}
}
