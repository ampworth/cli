#!/bin/sh
# Release the ampworth CLI with GoReleaser: macOS signing and notarisation, a GitHub
# release, and a cask in the Homebrew tap.
#
#   scripts/release.sh check     verify the prerequisites only
#   scripts/release.sh publish   verify, then release
#
# Signing and notarisation credentials live in the keychain (make
# notary-setup). Optional overrides come from the environment or .env.release
# (gitignored; see .env.release.example). GITHUB_TOKEN defaults to
# `gh auth token`, and the tap token defaults to GITHUB_TOKEN.
set -eu
cd "$(dirname "$0")/.."

mode=${1:-check}
problems=0
fail() { printf '  \033[31m✗\033[0m %s\n' "$1"; problems=$((problems + 1)); }
ok() { printf '  \033[32m✓\033[0m %s\n' "$1"; }

if [ -f .env.release ]; then
  set -a
  . ./.env.release
  set +a
fi

echo "Repository"
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  fail "not a git repository"
else
  if [ -n "$(git status --porcelain)" ]; then
    fail "working tree has uncommitted changes"
  else
    ok "working tree is clean"
  fi

  tag=$(git describe --tags --exact-match HEAD 2>/dev/null || true)
  if printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
    ok "HEAD is tagged $tag"
    if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then
      ok "tag $tag is pushed to origin"
    else
      fail "tag $tag is not on origin (git push origin $tag)"
    fi
  else
    fail "HEAD has no vX.Y.Z tag (git tag -a v0.1.0 -m 'ampworth v0.1.0')"
  fi

  case "$(git remote get-url origin 2>/dev/null || true)" in
    *github.com[:/]ampworth/cli*) ok "origin is github.com/ampworth/cli" ;;
    "") fail "no origin remote (expected github.com/ampworth/cli)" ;;
    *) fail "origin is $(git remote get-url origin), expected github.com/ampworth/cli" ;;
  esac
fi

echo "GitHub"
# GoReleaser refuses to run with more than one forge token set.
unset GITLAB_TOKEN GITEA_TOKEN
: "${GITHUB_TOKEN:=$(gh auth token 2>/dev/null || true)}"
: "${HOMEBREW_TAP_GITHUB_TOKEN:=$GITHUB_TOKEN}"
export GITHUB_TOKEN HOMEBREW_TAP_GITHUB_TOKEN
if [ -n "$GITHUB_TOKEN" ]; then ok "GITHUB_TOKEN is set"; else fail "GITHUB_TOKEN is not set and gh is not logged in"; fi
if GH_TOKEN=$HOMEBREW_TAP_GITHUB_TOKEN gh api repos/ampworth/homebrew-tap --jq .permissions.push 2>/dev/null | grep -q true; then
  ok "can push to ampworth/homebrew-tap"
else
  fail "HOMEBREW_TAP_GITHUB_TOKEN cannot push to ampworth/homebrew-tap"
fi

echo "macOS signing and notarisation (keychain)"
profile=${MACOS_NOTARY_PROFILE:-ampworth-notary}
if [ "$(uname)" != Darwin ]; then
  fail "releases sign with codesign and notarytool, so they must run on macOS"
else
  identity=${MACOS_SIGN_IDENTITY:-$(security find-identity -v -p codesigning |
    sed -n 's/.*"\(Developer ID Application: [^"]*\)".*/\1/p' | head -1)}
  if [ -n "$identity" ]; then ok "signing as $identity"; else fail "no Developer ID Application certificate in the keychain"; fi
  if xcrun notarytool history --keychain-profile "$profile" >/dev/null 2>&1; then
    ok "notarytool keychain profile $profile works"
  else
    fail "notarytool keychain profile $profile is missing or rejected (make notary-setup)"
  fi
fi

echo "Tools"
if command -v goreleaser >/dev/null 2>&1; then ok "goreleaser $(goreleaser --version 2>/dev/null | awk '/GitVersion/ { print $2 }')"; else fail "goreleaser is not installed (brew install goreleaser)"; fi
if goreleaser check >/dev/null 2>&1; then ok ".goreleaser.yaml is valid"; else fail ".goreleaser.yaml does not validate (goreleaser check)"; fi

if [ "$problems" -gt 0 ]; then
  printf '\n%d problem(s); not releasing.\n' "$problems"
  exit 1
fi

if [ "$mode" = publish ]; then
  printf '\nReleasing %s\n' "$tag"
  goreleaser release --clean
else
  printf '\nReady to release %s: run make release.\n' "$tag"
fi
