package updater

import (
	"os"
	"testing"
)

func TestSelectAssets(t *testing.T) {
	const (
		tag   = "v0.2.0"
		setup = "mam-0.2.0-windows-amd64-setup.exe"
	)
	asset := func(name string) Asset {
		return Asset{Name: name, BrowserDownloadURL: "https://example.invalid/" + name}
	}
	tests := []struct {
		name    string
		assets  []Asset
		wantErr bool
		wantSig bool
	}{
		{name: "installer and checksums", assets: []Asset{asset(setup), asset("checksums.txt")}},
		{name: "with the signature", assets: []Asset{asset(setup), asset("checksums.txt"), asset("checksums.txt.sig")}, wantSig: true},
		{name: "the portable exe is never the installer", assets: []Asset{asset("mam-0.2.0-windows-amd64.exe"), asset("checksums.txt")}, wantErr: true},
		{name: "the portable exe next to the installer is ignored", assets: []Asset{asset("mam-0.2.0-windows-amd64.exe"), asset(setup), asset("checksums.txt")}},
		{name: "another platform's installer", assets: []Asset{asset("mam-0.2.0-linux-amd64-setup.exe"), asset("checksums.txt")}, wantErr: true},
		{name: "an installer of another version", assets: []Asset{asset("mam-0.3.0-windows-amd64-setup.exe"), asset("checksums.txt")}, wantErr: true},
		{name: "the installer of the tag is picked among other versions", assets: []Asset{asset("mam-0.3.0-windows-amd64-setup.exe"), asset(setup), asset("checksums.txt")}},
		{name: "no checksums", assets: []Asset{asset(setup)}, wantErr: true},
		{name: "no assets", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectAssets(tt.assets, tag, PlatformWindowsAMD64)
			if (err != nil) != tt.wantErr {
				t.Fatalf("selectAssets error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.installer.Name != setup || got.checksums.Name != "checksums.txt" || (got.signature.Name != "") != tt.wantSig {
				t.Fatalf("selectAssets = %+v", got)
			}
		})
	}
}

// The installer is named after the release tag, not after the running build or a glob: a release
// tagged v99.0.0 that only carries the 0.2.0 installer is refused.
func TestSelectAssets_ExactNameOfTheReleaseTag(t *testing.T) {
	asset := func(name string) Asset { return Asset{Name: name} }
	stale := []Asset{asset("mam-0.2.0-windows-amd64-setup.exe"), asset("checksums.txt")}
	if _, err := selectAssets(stale, "v99.0.0", PlatformWindowsAMD64); err == nil {
		t.Fatal("selectAssets(v99.0.0) with only the 0.2.0 installer: err = nil, want an error")
	}
	right := []Asset{asset("mam-99.0.0-windows-amd64-setup.exe"), asset("mam-99.0.0-windows-amd64.exe"), asset("checksums.txt")}
	got, err := selectAssets(right, "v99.0.0", PlatformWindowsAMD64)
	if err != nil || got.installer.Name != "mam-99.0.0-windows-amd64-setup.exe" {
		t.Fatalf("selectAssets(v99.0.0) = %+v, %v; want the mam-99.0.0 installer", got, err)
	}
	portableOnly := []Asset{asset("mam-99.0.0-windows-amd64.exe"), asset("checksums.txt")}
	if _, err := selectAssets(portableOnly, "v99.0.0", PlatformWindowsAMD64); err == nil {
		t.Fatal("selectAssets(v99.0.0) with only the portable exe: err = nil, want an error")
	}
	prerelease := []Asset{asset("mam-1.0.0-rc.1-windows-amd64-setup.exe"), asset("checksums.txt")}
	if _, err := selectAssets(prerelease, "v1.0.0-rc.1", PlatformWindowsAMD64); err != nil {
		t.Fatalf("selectAssets(v1.0.0-rc.1): %v", err)
	}
}

func TestReadChecksum_KeyedOnTheInstallerName(t *testing.T) {
	// Only the installer's line counts; the portable exe and a comment are skipped.
	dir := t.TempDir()
	path := dir + "/checksums.txt"
	body := "# header\n" +
		"aaaa  mam-0.2.0-windows-amd64.exe\n" +
		"bbbb  mam-0.2.0-windows-amd64-setup.exe\n"
	if err := writeFile(path, body); err != nil {
		t.Fatal(err)
	}
	got, err := readChecksum(path, "mam-0.2.0-windows-amd64-setup.exe")
	if err != nil || got != "bbbb" {
		t.Fatalf("readChecksum = %q, %v; want bbbb", got, err)
	}
	if _, err := readChecksum(path, "mam-9.9.9-windows-amd64-setup.exe"); err == nil {
		t.Fatal("readChecksum for a missing name: err = nil, want an error")
	}
}

func writeFile(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }

func TestPlatformInstallerName(t *testing.T) {
	tests := []struct {
		platform Platform
		tag      string
		want     string
	}{
		{PlatformWindowsAMD64, "v0.2.0", "mam-0.2.0-windows-amd64-setup.exe"},
		{PlatformDarwinARM64, "v0.2.0", "mam-0.2.0-darwin-arm64.dmg"},
		{PlatformDarwinARM64, "v1.0.0-rc.1", "mam-1.0.0-rc.1-darwin-arm64.dmg"},
	}
	for _, tt := range tests {
		if got := tt.platform.InstallerName(tt.tag); got != tt.want {
			t.Errorf("%+v.InstallerName(%q) = %q, want %q", tt.platform, tt.tag, got, tt.want)
		}
	}
}

func TestDefaultPlatform(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         Platform
	}{
		{"darwin", "arm64", PlatformDarwinARM64},
		{"windows", "amd64", PlatformWindowsAMD64},
		// No installer is released for these: never another platform's installer, the zero Platform instead.
		{"linux", "amd64", Platform{}},
		{"darwin", "amd64", Platform{}},  // no Intel build exists; never the dmg nor the Windows installer
		{"windows", "arm64", Platform{}}, // the installer is an amd64 build
		{"windows", "386", Platform{}},
		{"freebsd", "amd64", Platform{}},
		{"", "", Platform{}},
	}
	for _, tt := range tests {
		got := DefaultPlatform(tt.goos, tt.goarch)
		if got != tt.want {
			t.Errorf("DefaultPlatform(%q, %q) = %+v, want %+v", tt.goos, tt.goarch, got, tt.want)
		}
		if got.Supported() != (tt.want != Platform{}) {
			t.Errorf("DefaultPlatform(%q, %q).Supported() = %v", tt.goos, tt.goarch, got.Supported())
		}
	}
}

func TestPlatform_Supported(t *testing.T) {
	if !PlatformWindowsAMD64.Supported() || !PlatformDarwinARM64.Supported() {
		t.Error("a platform a release carries an installer for must be supported")
	}
	for _, p := range []Platform{{}, {OS: "linux", Arch: "amd64"}, {OS: "darwin", Arch: "amd64"}, {OS: "windows"}} {
		if p.Supported() {
			t.Errorf("%+v.Supported() = true, want false", p)
		}
	}
}

func TestSelectAssets_DarwinPicksOnlyTheDmgOfTheTag(t *testing.T) {
	asset := func(name string) Asset { return Asset{Name: name} }
	const dmg = "mam-0.2.0-darwin-arm64.dmg"
	all := []Asset{
		asset("mam-0.2.0-windows-amd64-setup.exe"), asset("mam-0.2.0-windows-amd64.exe"),
		asset("mam-0.1.0-darwin-arm64.dmg"), asset(dmg), asset("checksums.txt"), asset("checksums.txt.sig"),
	}
	got, err := selectAssets(all, "v0.2.0", PlatformDarwinARM64)
	if err != nil || got.installer.Name != dmg || got.checksums.Name != "checksums.txt" || got.signature.Name != "checksums.txt.sig" {
		t.Fatalf("selectAssets(darwin) = %+v, %v; want the %s", got, err, dmg)
	}
	// The Windows installer is never the macOS installer, nor the other way round.
	if _, err := selectAssets([]Asset{asset("mam-0.2.0-windows-amd64-setup.exe"), asset("checksums.txt")}, "v0.2.0", PlatformDarwinARM64); err == nil {
		t.Fatal("selectAssets(darwin) with only the Windows installer: err = nil, want an error")
	}
	if _, err := selectAssets([]Asset{asset(dmg), asset("checksums.txt")}, "v0.2.0", PlatformWindowsAMD64); err == nil {
		t.Fatal("selectAssets(windows) with only the dmg: err = nil, want an error")
	}
	// A release without the dmg (a Windows-only release) is an error for the Mac app, not an update to nothing.
	if _, err := selectAssets([]Asset{asset("mam-0.2.0-windows-amd64-setup.exe"), asset("checksums.txt")}, "v0.2.0", PlatformDarwinARM64); err == nil {
		t.Fatal("expected an error for a release without the dmg")
	}
}
