#!/usr/bin/env bash

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

mapfile -d '' -t go_files < <(git ls-files -z '*.go')
unformatted=$(gofmt -l "${go_files[@]}")
if [ -n "$unformatted" ]; then
  printf 'Go files require gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi

git ls-files '*.nix' -z | xargs -0 nixfmt --check
shellcheck scripts/*.sh
# Forgejo requires fully qualified action URLs. actionlint otherwise validates
# the GitHub-compatible workflow schema but does not recognize that URL form.
actionlint \
  -config-file .github/actionlint.yaml \
  -ignore 'specifying action ".+" in invalid format' \
  .forgejo/workflows/*.yml

go test ./...
go test -race ./...
go vet ./...
node --test .forgejo/scripts/*.test.mjs
nix flake check --print-build-logs
git diff --check
