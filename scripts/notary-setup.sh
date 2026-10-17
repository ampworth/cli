#!/bin/sh
# Stores notarisation credentials in the keychain as a notarytool profile, so
# releases never see the password. notarytool prompts for the app-specific
# password itself (create one at https://account.apple.com, Sign-In and
# Security > App-Specific Passwords).
set -eu

profile=${MACOS_NOTARY_PROFILE:-ampworth-notary}
team=$(security find-identity -v -p codesigning |
  sed -n 's/.*"Developer ID Application: [^"]*(\([A-Z0-9]*\))".*/\1/p' | head -1)

printf 'Apple ID (email): '
read -r apple_id
printf 'Team ID [%s]: ' "$team"
read -r answer
team=${answer:-$team}

xcrun notarytool store-credentials "$profile" --apple-id "$apple_id" --team-id "$team"
echo "Stored keychain profile $profile. Check it with: make release-check"
