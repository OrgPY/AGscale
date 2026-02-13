#!/usr/bin/env bash
set -euo pipefail

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "Not inside a git repository. Initialize git first."
  exit 1
fi

chmod +x .githooks/pre-commit
git config core.hooksPath .githooks
echo "Installed pre-commit hook via core.hooksPath=.githooks"
