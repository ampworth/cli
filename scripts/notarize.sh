#!/bin/sh
# Signs and notarises a macOS binary with Apple's own tools, using only the
# keychain: the Developer ID Application certificate for codesign, and a
# notarytool keychain profile (created by `make notary-setup`) holding the
# Apple ID, team ID and app-specific password.
#
# GoReleaser runs this on the universal binary, before archiving.
#
#   scripts/notarize.sh <binary> [is-snapshot]
#
# MACOS_SIGN_IDENTITY overrides the certificate (default: the first Developer
# ID Application identity); MACOS_NOTARY_PROFILE the profile (default:
# ampworth-notary).
set -eu

bin=$1
if [ "${2:-false}" = true ]; then
  echo "notarize: snapshot, not signing $bin"
  exit 0
fi

identity=${MACOS_SIGN_IDENTITY:-$(security find-identity -v -p codesigning |
  sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -1)}
profile=${MACOS_NOTARY_PROFILE:-ampworth-notary}
if [ -z "$identity" ]; then
  echo "notarize: no Developer ID Application certificate in the keychain" >&2
  exit 1
fi

echo "notarize: signing $bin as $identity"
codesign --force --options runtime --timestamp --sign "$identity" "$bin"
codesign --verify --strict --verbose=2 "$bin"

# notarytool takes a zip, dmg or pkg; a bare binary goes in a zip. Apple
# cannot staple a ticket to a bare binary, so Gatekeeper checks it online.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
ditto -c -k --keepParent "$bin" "$tmp/submission.zip"

echo "notarize: submitting to Apple with keychain profile $profile"
xcrun notarytool submit "$tmp/submission.zip" --keychain-profile "$profile" \
  --wait --timeout 20m --output-format json > "$tmp/result.json" || true
status=$(plutil -extract status raw -o - "$tmp/result.json" 2>/dev/null || echo unknown)
id=$(plutil -extract id raw -o - "$tmp/result.json" 2>/dev/null || echo "")
if [ "$status" != Accepted ]; then
  echo "notarize: Apple returned status $status" >&2
  cat "$tmp/result.json" >&2
  if [ -n "$id" ]; then
    xcrun notarytool log "$id" --keychain-profile "$profile" >&2 || true
  fi
  exit 1
fi
echo "notarize: accepted ($id)"
