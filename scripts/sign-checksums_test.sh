#!/usr/bin/env bash
# Tests for scripts/sign-checksums.sh with throwaway ed25519 keys (never the release key).
# Run: bash scripts/sign-checksums_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/sign-checksums.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0

# pubkey_b64 <pem>: the base64 of the raw 32-byte public key (the last 32 bytes of the DER SubjectPublicKeyInfo).
pubkey_b64() { openssl pkey -in "$1" -pubout -outform DER | tail -c 32 | base64 -w0; }

openssl genpkey -algorithm ed25519 -out "$work/key.pem"
openssl genpkey -algorithm ed25519 -out "$work/other.pem"
good_pub="$(pubkey_b64 "$work/key.pem")"
other_pub="$(pubkey_b64 "$work/other.pem")"

fresh_dir() {
  rm -rf "$work/dist" && mkdir "$work/dist"
  echo "0123  mam-1.2.3-windows-amd64-setup.exe" >"$work/dist/checksums.txt"
}

# run <key pem> <verify public key>: runs the script in a fresh dir with the key in the environment.
run() {
  fresh_dir
  RELEASE_SIGNING_KEY="$(cat "$1")" VERIFY_PUBLIC_KEY="$2" bash "$script" "$work/dist" >/dev/null 2>&1
}

check() { # check <name> <expected: pass|fail> <command...>
  local name="$1" want="$2"
  shift 2
  local got=fail
  if "$@"; then got=pass; fi
  if [[ "$got" == "$want" ]]; then echo "ok: $name"; else echo "FAIL: $name (wanted $want)"; fails=$((fails + 1)); fi
}

# sig_is_64_raw_bytes: after a successful run, checksums.txt.sig is the base64 of 64 raw bytes.
sig_is_64_raw_bytes() { run "$work/key.pem" "$good_pub" && [[ "$(base64 -d "$work/dist/checksums.txt.sig" | wc -c)" -eq 64 ]]; }
no_signing_key() { fresh_dir && RELEASE_SIGNING_KEY="" VERIFY_PUBLIC_KEY="$good_pub" bash "$script" "$work/dist" >/dev/null 2>&1; }
no_checksums() { RELEASE_SIGNING_KEY=x VERIFY_PUBLIC_KEY=x bash "$script" "$work/nodir" >/dev/null 2>&1; }

check "a matching key pair signs and verifies" pass run "$work/key.pem" "$good_pub"
check "the signature is base64 of 64 raw bytes (the app's format)" pass sig_is_64_raw_bytes
check "a public key of another pair fails" fail run "$work/key.pem" "$other_pub"
check "an empty public key fails" fail run "$work/key.pem" ""
check "a public key that is not 32 bytes fails" fail run "$work/key.pem" "$(printf 'short' | base64 -w0)"
check "an empty signing key fails" fail no_signing_key
check "a missing checksums.txt fails" fail no_checksums

if [[ "$fails" -gt 0 ]]; then
  echo "$fails failed"
  exit 1
fi
