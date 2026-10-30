#!/usr/bin/env bash
# Publish the version in types.go as a Go module tag and GitHub release.
# Usage: ./release.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

repo="openfluke/snat"
version="$(sed -nE 's/^const Version = "([0-9]+\.[0-9]+\.[0-9]+)"$/v\1/p' types.go)"
[[ -n "$version" ]] || { echo "Cannot read Version from types.go" >&2; exit 1; }
[[ "$(git branch --show-current)" == main ]] || { echo "Release from main." >&2; exit 1; }

# Never move an existing version tag to different source.
if git show-ref --verify --quiet "refs/tags/$version"; then
  [[ "$(git rev-parse "$version^{commit}")" == "$(git rev-parse HEAD)" && -z "$(git status --porcelain)" ]] || {
    echo "$version already exists; bump Version before releasing changed source." >&2
    exit 1
  }
fi

gh auth status
go test -mod=readonly ./...

if [[ -n "$(git status --porcelain)" ]]; then
  git add -A
  git commit -m "Release SNAT $version"
fi
if ! git show-ref --verify --quiet "refs/tags/$version"; then
  git tag -a "$version" -m "SNAT $version"
fi

git push origin HEAD:main
git push origin "refs/tags/$version"
if gh release view "$version" --repo "$repo" >/dev/null 2>&1; then
  echo "GitHub release $version already exists."
else
  gh release create "$version" --repo "$repo" --verify-tag \
    --title "SNAT $version" --notes-file CHANGELOG.md
fi
printf '\nRelease: https://github.com/%s/releases/tag/%s\n' "$repo" "$version"
printf 'Import:  go get github.com/%s@%s\n' "$repo" "$version"
