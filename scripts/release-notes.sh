#!/bin/bash
# Prints the CHANGELOG.md section for one version, for use as GitHub release notes.
# Usage: scripts/release-notes.sh v0.1.0
set -euo pipefail
v=${1#v}
awk -v v="$v" '
  $0 ~ "^## \\[" v "\\]" { on=1; next }
  on && /^## \[/ { exit }
  on && /^\[[^]]+\]: / { exit }
  on { print }
' "$(dirname "$0")/../CHANGELOG.md" | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}'
