#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: ./scripts/release.sh <patch|minor|major> [--push]

Creates the next SemVer git tag from the latest stable v* tag.
Examples:
  ./scripts/release.sh patch
  ./scripts/release.sh minor --push
EOF
  exit 1
}

bump=""
push_tag=false

for arg in "$@"; do
  case "$arg" in
    patch|minor|major)
      if [[ -n "$bump" ]]; then
        usage
      fi
      bump="$arg"
      ;;
    --push)
      push_tag=true
      ;;
    *)
      usage
      ;;
  esac
done

if [[ -z "$bump" ]]; then
  usage
fi

if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "release.sh must be run inside a git repository." >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "Working tree must be clean before creating a release tag." >&2
  exit 1
fi

latest_tag="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-version:refname | head -n 1)"
if [[ -z "$latest_tag" ]]; then
  current_version="0.0.0"
else
  current_version="${latest_tag#v}"
fi

IFS=. read -r major minor patch <<<"$current_version"

if [[ -z "${major:-}" || -z "${minor:-}" || -z "${patch:-}" ]]; then
  echo "Latest version is not valid SemVer: ${current_version}" >&2
  exit 1
fi

case "$bump" in
  patch)
    patch=$((patch + 1))
    ;;
  minor)
    minor=$((minor + 1))
    patch=0
    ;;
  major)
    major=$((major + 1))
    minor=0
    patch=0
    ;;
esac

next_version="${major}.${minor}.${patch}"
tag="v${next_version}"

if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
  echo "Tag already exists: ${tag}" >&2
  exit 1
fi

git tag -a "${tag}" -m "${tag}"
echo "Created tag ${tag}"

if [[ -n "$latest_tag" ]]; then
  echo "Previous release was ${latest_tag}"
else
  echo "No previous release tags found; started from 0.0.0"
fi

if [[ "$push_tag" == true ]]; then
  git push origin "${tag}"
  echo "Pushed tag ${tag}"
else
  echo "Run 'git push origin ${tag}' to publish the release."
fi
