#!/bin/bash
set -e
DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"
rm -f gopm

# Check for uncommitted changes
if ! git diff --quiet; then
  echo "Error: There are unstaged changes. Commit or stash them first." >&2
  git diff --name-only >&2
  exit 1
fi

FILE="$DIR/version.txt"
VERSION=$(cat "$FILE")

if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: '$VERSION' is not a valid version (expected X.Y.Z)" >&2
  exit 1
fi

MAJOR=$(echo "$VERSION" | cut -d. -f1)
MINOR=$(echo "$VERSION" | cut -d. -f2)
PATCH=$(echo "$VERSION" | cut -d. -f3)
NEWVERSION="${MAJOR}.${MINOR}.$((PATCH + 1))"
echo "$NEWVERSION" > "$FILE"
echo "Bumped to $NEWVERSION"

# Build check
make || { echo "Build failed, aborting release" >&2; git checkout "$FILE"; exit 1; }

git add version.txt
git commit -m "Bump version to $NEWVERSION"
git tag -a "v${NEWVERSION}" -m "Release v${NEWVERSION}"
git push origin main
git push origin "v${NEWVERSION}"