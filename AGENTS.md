# AGENTS.md

djpi copies media off a DJI Osmo Action 6 or Pocket 3 to local, S3 and Immich archives, verifies every copy, and never deletes anything. Go, Nix flake. Read [`README.md`](README.md) first, then [`CONTEXT.md`](CONTEXT.md) for the vocabulary: use those words (Source, Media Item, Delivery, Ingest), not the ones it says to avoid.

## Check

- `nix develop -c ./scripts/check.sh` is the whole gate and what CI runs: gofmt, nixfmt, shellcheck, actionlint, `go test` (also `-race`), `go vet`, the `.forgejo/scripts` tests and `nix flake check`. Run it before pushing.
- Run one package with `nix develop -c go test ./internal/<pkg>/...`.
- Try it: `go run ./cmd/djpi serve --config djpi.example.json`.

## Laws

- Nothing deletes or formats a camera unless the cleanup gate and a proven camera-specific mechanism both allow it. There is no cleanup setting: do not add one except for [#11](https://git.kuhree.com/kuhree.com/djpi/issues/11).
- Only a content-verified Delivery at every required Destination, against an unchanged Source Snapshot, makes a Source ready for cleanup. Selection, Immich scans and "the transfer returned" do not.
- Fail closed on unknown hardware. Identity is by SHA-256, not path.
- `internal/action6` and `internal/wireless` do no I/O. Keep them offline until a camera has validated the live path.

## Commits and releases

- Conventional Commits, `type(scope): summary`. semantic-release cuts versions from `main`; a breaking change bumps minor (pre-1.0). Do not edit `VERSION` by hand.
- Third-party protocol code or fixtures: record provenance in `THIRD_PARTY_NOTICES.md`.

## Docs

- [`docs/ingest.md`](docs/ingest.md) is the product and its laws; [`docs/review.md`](docs/review.md) is the Immich review surface. Operating notes: `usage`, `configuration`, `deployment*`, `wireless`, `action6-control`, `hardware/`.
- Plans are issues, not files. Docs follow the `agent-docs` skill.
