package updater_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
	"github.com/ousiassllc/mam/backend/internal/version"
)

// signedReleaseMock serves a v0.2.0 release whose checksums.txt body is
// checksums and whose checksums.txt.sig body is signature (the asset is
// omitted from the release when signature is nil), for .
func signedReleaseMock(t *testing.T, checksums string, signature *string) *httptest.Server {
	t.Helper()
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/mam/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		assets := []updater.Asset{
			{Name: installerName, BrowserDownloadURL: serverURL + "/download/installer.exe"},
			{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
		}
		if signature != nil {
			assets = append(assets, updater.Asset{Name: "checksums.txt.sig", BrowserDownloadURL: serverURL + "/download/checksums.txt.sig"})
		}
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: assets})
	})
	mux.HandleFunc("/download/installer.exe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(installerBody))
	})
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("/download/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(*signature))
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func TestChecker_CheckForUpdate_SignatureVerification(t *testing.T) {
	setVersion(t, "v0.1.0")
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(key ed25519.PrivateKey, content string) string {
		return base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(content)))
	}

	good := fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)
	// A replaced release: checksums.txt was edited after it was signed.
	forged := good + "# tampered\n"
	goodSig := sign(priv, good)
	goodSigNewline := goodSig + "\n"
	otherKeySig := sign(otherPriv, good)
	notBase64 := "not-base64!!"
	tooShort := base64.StdEncoding.EncodeToString([]byte("short"))

	// A replaced installer under an untouched, validly signed checksums.txt: the signature holds, the hash does not.
	otherHash := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("the installer that was signed")), installerName)
	otherHashSig := sign(priv, otherHash)

	tests := []struct {
		name      string
		checksums string
		signature *string
		key       ed25519.PublicKey
		wantReady bool
	}{
		{name: "valid signature", checksums: good, signature: &goodSig, key: pub, wantReady: true},
		{name: "valid signature with trailing newline", checksums: good, signature: &goodSigNewline, key: pub, wantReady: true},
		{name: "checksums.txt altered after signing", checksums: forged, signature: &goodSig, key: pub},
		{name: "signed by another key", checksums: good, signature: &otherKeySig, key: pub},
		{name: "installer replaced after signing", checksums: otherHash, signature: &otherHashSig, key: pub},
		{name: "signature asset missing", checksums: good, signature: nil, key: pub},
		{name: "signature not base64", checksums: good, signature: &notBase64, key: pub},
		{name: "signature wrong length", checksums: good, signature: &tooShort, key: pub},
		{name: "malformed configured public key", checksums: good, signature: &goodSig, key: pub[:5]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			server := signedReleaseMock(t, tt.checksums, tt.signature)
			checker := updater.NewChecker(updater.Config{
				Owner: "ousiassllc", Repo: "mam", Gate: allowGate(),
				HTTPClient: server.Client(), BaseURL: server.URL,
				DownloadURLPrefix: server.URL + "/download/",
				PublicKey:         tt.key,
				Platform:          updater.PlatformWindowsAMD64,
			})

			result, err := checker.CheckForUpdate(context.Background())
			if tt.wantReady {
				if err != nil || !result.Ready {
					t.Fatalf("CheckForUpdate = %+v, %v; want Ready", result, err)
				}
				_ = os.RemoveAll(result.InstallerPath)
				return
			}
			if err == nil || result.Ready {
				t.Fatalf("CheckForUpdate = %+v, %v; want a verification error", result, err)
			}
			if got := checker.Status().ErrorKind; got != updater.ErrorVerification {
				t.Errorf("ErrorKind = %q, want %q (err: %v)", got, updater.ErrorVerification, err)
			}
			if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
				t.Errorf("temp dir not cleaned after rejected download: %d entries", len(entries))
			}
		})
	}
}

// TestChecker_CheckForUpdate_EmbeddedPublicKey covers the production path:
// the key comes from backend/internal/version.ReleasePublicKey (set by the release build's
// -ldflags), not Config.PublicKey. A malformed embedded key must fail
// closed instead of silently disabling verification.
func TestChecker_CheckForUpdate_EmbeddedPublicKey(t *testing.T) {
	setVersion(t, "v0.1.0")
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	checksums := fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)
	validSig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(checksums)))
	forgedSig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("other")))

	tests := []struct {
		name      string
		embedded  string
		signature string
		wantReady bool
	}{
		{name: "valid", embedded: base64.StdEncoding.EncodeToString(pub), signature: validSig, wantReady: true},
		{name: "forged", embedded: base64.StdEncoding.EncodeToString(pub), signature: forgedSig},
		{name: "embedded key not base64", embedded: "%%%", signature: validSig},
		{name: "embedded key wrong length", embedded: base64.StdEncoding.EncodeToString(pub[:8]), signature: validSig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := version.ReleasePublicKey
			version.ReleasePublicKey = tt.embedded
			t.Cleanup(func() { version.ReleasePublicKey = original })
			server := signedReleaseMock(t, checksums, &tt.signature)

			result, err := newChecker(server, allowGate()).CheckForUpdate(context.Background())
			if tt.wantReady {
				if err != nil || !result.Ready {
					t.Fatalf("CheckForUpdate = %+v, %v; want Ready", result, err)
				}
				_ = os.RemoveAll(result.InstallerPath)
				return
			}
			if err == nil || result.Ready {
				t.Fatalf("CheckForUpdate = %+v, %v; want a verification error", result, err)
			}
		})
	}
}
