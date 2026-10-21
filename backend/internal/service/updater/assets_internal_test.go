package updater

import (
	"os"
	"testing"
)

func TestSelectAssets(t *testing.T) {
	const setup = "mam-0.2.0-windows-amd64-setup.exe"
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
		{name: "no checksums", assets: []Asset{asset(setup)}, wantErr: true},
		{name: "two installers are ambiguous", assets: []Asset{asset(setup), asset("mam-0.3.0-windows-amd64-setup.exe"), asset("checksums.txt")}, wantErr: true},
		{name: "no assets", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectAssets(tt.assets)
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
