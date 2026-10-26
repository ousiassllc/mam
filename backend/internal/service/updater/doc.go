// Package updater is the automatic update check and the verified download of the Windows desktop
// build (spec/architecture/desktop.md §Auto update). Only the Wails front wires it in: cmd/server
// is headless, has no installer, and never does.
//
// Checker.CheckForUpdate runs the whole read-only-until-verified pipeline once: fetch the latest
// release of this repository's own Releases (version.GitHubOwner / version.GitHubRepo), compare its
// tag_name with version.Version (golang.org/x/mod/semver; no hand-rolled parsing), and - only once
// Gate.SafeToUpdate says that no work a restart would lose is in flight (SafeGate) - download the
// installer asset (named exactly mam-<tag_name without its leading v>-windows-amd64-setup.exe: an
// installer of another version never matches), verify it against the checksums.txt line of that same
// name and that file's detached signature, and report the verified local path in its Result.
// The gate is evaluated a second time right after the download: it takes minutes, and the caller
// quits and installs as soon as the Result is Ready, so work that started meanwhile turns the check
// into Status.Blocked instead (the installer is discarded and the next check downloads it again).
//
// A build whose version.Version is not a semver ("dev") skips the check entirely.
//
// Fetch hardening: every request carries a context deadline (Config.MetadataTimeout for the release
// lookup, Config.DownloadTimeout for the whole download phase), so a stalled connection cannot hold
// Checker's checkMu forever. Asset downloads are size-capped (maxInstallerBytes / maxChecksumsBytes,
// tightened to Asset.Size when GitHub reports one) and Asset.BrowserDownloadURL must live under
// https://github.com/<owner>/<repo>/releases/download/.
//
// Release authenticity: checksums.txt comes from the same release as the installer, so SHA-256 alone
// only detects corruption, not a replaced release (a compromised token or release job). The build
// job of .github/workflows/release.yml therefore also signs checksums.txt, in its last step
// (Sign checksums, the only one that sees the signing secret; it verifies the signature with the
// public key in the same step), and the release job only publishes the resulting checksums.txt.sig:
// a base64 detached ed25519 signature of
// checksums.txt. The matching public key is embedded in the
// binary (version.ReleasePublicKey, overridable by Config.PublicKey). When a key is configured,
// downloadAndVerify verifies the signature right after fetching checksums.txt - before the installer
// is downloaded or run - and a missing, malformed or mismatching signature (or a malformed key) is a
// Permanent ErrorVerification that removes every downloaded file. Known limitation: a build without
// a public key (a dev build) skips the signature check, logs a warning and relies on SHA-256 alone;
// Authenticode code signing is not implemented.
//
// A 404 on the latest-release lookup (no release published) is not a failure: the check succeeds with
// Status.NoRelease set and is not retried with backoff. A refused lookup (401/403) or asset download
// (401/403/404) is ErrorAccess, distinct from ErrorRelease (fetched but unusable content); a
// rate-limited one (429, or 403 with Retry-After) is ErrorRateLimit. ErrorAccess / ErrorVerification /
// ErrorRelease report Permanent(), so the retry with backoff (service/appupdate) does not repeat them.
//
// Checker itself never runs the installer or quits the process: service/appupdate hands the verified
// path to the front (backend/internal/desktop), which stops the server and runs the installer.
//
// This package depends on the domain (the job statuses SafeGate reads) and its own I/O (net/http to
// GitHub) only. It lives in the service layer's tree, so the web layer reaches it through
// service/appupdate, never directly.
package updater
