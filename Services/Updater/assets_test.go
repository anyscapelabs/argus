package Updater

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// releaseAssets mirrors what .github/workflows/build.yml uploads for a tagged
// release, plus the checksum sidecar the updater verifies against.
func releaseAssets() []github.ReleaseAsset {
	names := []string{
		"SHA256SUMS",
		"argus-linux-x86_64-amd64",
		"argus-linux-arm64",
		"argus-linux-x86_64-amd64.AppImage",
		"argus-linux-arm64.AppImage",
		"argus-linux-x86_64-amd64.deb",
		"argus-linux-arm64.deb",
		"argus-linux-x86_64-amd64.rpm",
		"argus-linux-arm64.rpm",
		"argus-linux-x86_64-amd64-argus-0.2.0-1-x86_64.pkg.tar.zst",
		"argus-macos-intel-x64",
		"argus-macos-apple-silicon-arm64",
		"argus-macos-intel-x64.app.zip",
		"argus-macos-apple-silicon-arm64.app.zip",
		"argus-macos-intel-x64.dmg",
		"argus-macos-apple-silicon-arm64.dmg",
		"argus-windows-x64-amd64.exe",
		"argus-windows-arm64.exe",
		"argus-windows-x64-amd64-installer.exe",
	}
	out := make([]github.ReleaseAsset, len(names))
	for i, n := range names {
		out[i] = github.ReleaseAsset{Name: n}
	}
	return out
}

func TestAssetMatcherPicksTheArtifactThisInstallCanSwap(t *testing.T) {
	tests := []struct {
		name           string
		platform, arch string
		appImage       bool
		want           string
	}{
		{
			name:     "macOS arm64 install takes the app bundle, not the dmg",
			platform: "darwin", arch: "arm64",
			want: "argus-macos-apple-silicon-arm64.app.zip",
		},
		{
			name:     "macOS amd64 install takes the app bundle",
			platform: "darwin", arch: "amd64",
			want: "argus-macos-intel-x64.app.zip",
		},
		{
			name:     "linux amd64 binary install takes the raw binary, never a package",
			platform: "linux", arch: "amd64",
			want: "argus-linux-x86_64-amd64",
		},
		{
			name:     "linux arm64 binary install takes the raw binary",
			platform: "linux", arch: "arm64",
			want: "argus-linux-arm64",
		},
		{
			name:     "linux running from an AppImage must take the AppImage",
			platform: "linux", arch: "arm64", appImage: true,
			want: "argus-linux-arm64.AppImage",
		},
		{
			name:     "windows amd64 takes the bare exe, not the installer",
			platform: "windows", arch: "amd64",
			want: "argus-windows-x64-amd64.exe",
		},
		{
			name:     "windows arm64 takes its own exe",
			platform: "windows", arch: "arm64",
			want: "argus-windows-arm64.exe",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.appImage {
				t.Setenv("APPIMAGE", "/tmp/argus.AppImage")
			} else {
				t.Setenv("APPIMAGE", "")
			}
			assets := releaseAssets()
			idx := AssetMatcher(updater.CheckRequest{Platform: tc.platform, Arch: tc.arch}, assets)
			if idx < 0 || idx >= len(assets) {
				t.Fatalf("matcher returned %d, want %q", idx, tc.want)
			}
			if got := assets[idx].Name; got != tc.want {
				t.Fatalf("picked %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssetMatcherRejectsChecksumsAndUnknownPlatforms(t *testing.T) {
	assets := []github.ReleaseAsset{{Name: "SHA256SUMS"}, {Name: "argus-0.2.0.tar.gz"}}
	if idx := AssetMatcher(updater.CheckRequest{Platform: "plan9", Arch: "mips"}, assets); idx != -1 {
		t.Fatalf("matcher returned %d for an unsupported platform, want -1", idx)
	}
	if idx := AssetMatcher(updater.CheckRequest{Platform: "linux", Arch: "amd64"}, []github.ReleaseAsset{{Name: "SHA256SUMS"}}); idx != -1 {
		t.Fatalf("matcher picked the checksum sidecar (index %d)", idx)
	}
}
