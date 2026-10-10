#!/usr/bin/env bash
set -euo pipefail

# scripts/update-docs-version.sh
# Usage: ./scripts/update-docs-version.sh [VERSION] [DOCS_FILE]
# Example: ./scripts/update-docs-version.sh v0.2.0 docs/index.html

DOCS_FILE="${2:-docs/index.html}"
VERSION="${1:-}"

# If no version passed, resolve from latest git tag
if [ -z "$VERSION" ]; then
  if git rev-parse --git-dir > /dev/null 2>&1; then
    VERSION=$(git tag -l "v*.*.*" --sort=-v:refname 2>/dev/null | head -n 1 || true)
    if [ -z "$VERSION" ]; then
      VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    fi
  fi
fi

if [ -z "$VERSION" ]; then
  echo "Error: No version specified and could not determine tag from git." >&2
  exit 1
fi

# Ensure version begins with 'v' if it is semver
if [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
  VERSION="v${VERSION}"
fi

if [ ! -f "$DOCS_FILE" ]; then
  echo "Error: Docs file '$DOCS_FILE' not found." >&2
  exit 1
fi

echo "Updating $DOCS_FILE with version $VERSION..."

python3 - "$DOCS_FILE" "$VERSION" << 'EOF'
import sys, re

file_path = sys.argv[1]
version = sys.argv[2]
semver_num = version.lstrip("v")

with open(file_path, "r", encoding="utf-8") as f:
    content = f.read()

original = content

# 1. Replace <span id="release-version">...</span> or elements with class/id release-version
content = re.sub(
    r'(<(?:span|strong|code|div)[^>]*id=["\']release-version["\'][^>]*>)[^<]*(</(?:span|strong|code|div)>)',
    rf'\g<1>{version}\g<2>',
    content
)

# 2. Replace plain semver numbers in class="release-version-number" or id="release-version-number"
content = re.sub(
    r'(<(?:span|strong|code)[^>]*(?:class|id)=["\'][^"\']*release-version-number[^"\']*["\'][^>]*>)[^<]*(</(?:span|strong|code)>)',
    rf'\g<1>{semver_num}\g<2>',
    content
)

# 3. Replace releases/tag/v... link
content = re.sub(
    r'(releases/tag/)v[0-9]+\.[0-9]+\.[0-9]+',
    rf'\g<1>{version}',
    content
)

# 4. Replace releases/download/v... link
content = re.sub(
    r'(releases/download/)v[0-9]+\.[0-9]+\.[0-9]+',
    rf'\g<1>{version}',
    content
)

# 5. Replace binary archive & package names: onvif-camera-simulator_X.Y.Z_...
content = re.sub(
    r'onvif-camera-simulator_[0-9]+\.[0-9]+\.[0-9]+_',
    f'onvif-camera-simulator_{semver_num}_',
    content
)

# 6. Replace docker tag onvif-camera-simulator:vX.Y.Z
content = re.sub(
    r'onvif-camera-simulator:v[0-9]+\.[0-9]+\.[0-9]+',
    f'onvif-camera-simulator:{version}',
    content
)

# 7. Update elements with data-version attribute or class="ver-str"
content = re.sub(
    r'(<(?:span|code)[^>]*class=["\'][^"\']*ver-str[^"\']*["\'][^>]*>)[^<]*(</(?:span|code)>)',
    rf'\g<1>{version}\g<2>',
    content
)

content = re.sub(
    r'(<(?:span|code)[^>]*class=["\'][^"\']*ver-num[^"\']*["\'][^>]*>)[^<]*(</(?:span|code)>)',
    rf'\g<1>{semver_num}\g<2>',
    content
)

if content == original:
    print(f"[docs] No changes required in {file_path}. Already at {version}.")
else:
    with open(file_path, "w", encoding="utf-8") as f:
        f.write(content)
    print(f"[docs] Successfully updated {file_path} to {version}.")
EOF
