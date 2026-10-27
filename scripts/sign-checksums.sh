#!/usr/bin/env bash
# Signs <dir>/checksums.txt with the release signing key and verifies the signature (release.yml, `sign` job, last step that runs a script).
#
# Usage: scripts/sign-checksums.sh <dir>        writes <dir>/checksums.txt.sig
# Env:
#   RELEASE_SIGNING_KEY  ed25519 private key (PEM). Never printed; written to a 0600 temp file that is removed right after signing.
#   VERIFY_PUBLIC_KEY    base64 of the raw 32-byte ed25519 public key that matches RELEASE_SIGNING_KEY (the one the exe embeds,
#                        except in the one release of a key rotation: spec/environment/setup.md "自動更新の配布物と署名").
# The signature is the base64 of the raw 64-byte ed25519 signature (the format of the app's verifier). It is verified here with the
# public key (raw 32 bytes -> DER by the fixed ed25519 SubjectPublicKeyInfo prefix), so a key pair that does not match fails the
# build instead of publishing a release every installed app rejects.
set -euo pipefail

[[ $# -eq 1 && -f "$1/checksums.txt" ]] || { echo "usage: $0 <dir containing checksums.txt>" >&2; exit 2; }
dir="$1"

if [[ -z "${RELEASE_SIGNING_KEY:-}" ]]; then
  echo "::error::the secret RELEASE_SIGNING_KEY is not set: the release cannot be signed"
  exit 1
fi

work="$(umask 077 && mktemp -d)"
trap 'rm -rf "$work"' EXIT
(umask 077 && printf '%s\n' "$RELEASE_SIGNING_KEY" >"$work/key.pem")
openssl pkeyutl -sign -rawin -inkey "$work/key.pem" -in "$dir/checksums.txt" | base64 -w0 >"$dir/checksums.txt.sig"
rm -f "$work/key.pem"

printf '%s' "${VERIFY_PUBLIC_KEY:-}" | base64 -d >"$work/public.raw" 2>/dev/null || true
if [[ "$(wc -c <"$work/public.raw")" -ne 32 ]]; then
  echo "::error::the verification public key (RELEASE_SIGNING_VERIFY_PUBLIC_KEY or RELEASE_SIGNING_PUBLIC_KEY) is not the base64 of a raw 32-byte ed25519 public key"
  exit 1
fi
{ printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; cat "$work/public.raw"; } >"$work/public.der"
base64 -d "$dir/checksums.txt.sig" >"$work/checksums.sig"
openssl pkeyutl -verify -rawin -pubin -keyform DER -inkey "$work/public.der" -in "$dir/checksums.txt" -sigfile "$work/checksums.sig"
