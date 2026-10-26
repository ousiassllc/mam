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
			got, err := selectAssets(tt.assets, tag)
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
	if _, err := selectAssets(stale, "v99.0.0"); err == nil {
		t.Fatal("selectAssets(v99.0.0) with only the 0.2.0 installer: err = nil, want an error")
	}
	right := []Asset{asset("mam-99.0.0-windows-amd64-setup.exe"), asset("mam-99.0.0-windows-amd64.exe"), asset("checksums.txt")}
	got, err := selectAssets(right, "v99.0.0")
	if err != nil || got.installer.Name != "mam-99.0.0-windows-amd64-setup.exe" {
		t.Fatalf("selectAssets(v99.0.0) = %+v, %v; want the mam-99.0.0 installer", got, err)
	}
	portableOnly := []Asset{asset("mam-99.0.0-windows-amd64.exe"), asset("checksums.txt")}
	if _, err := selectAssets(portableOnly, "v99.0.0"); err == nil {
		t.Fatal("selectAssets(v99.0.0) with only the portable exe: err = nil, want an error")
	}
	prerelease := []Asset{asset("mam-1.0.0-rc.1-windows-amd64-setup.exe"), asset("checksums.txt")}
	if _, err := selectAssets(prerelease, "v1.0.0-rc.1"); err != nil {
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
