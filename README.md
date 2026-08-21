# djpi

`djpi` is an x86_64 Linux camera station for the DJI Osmo Action 6 and DJI Osmo
Pocket 3.

The target experience is simple: connect a camera, ingest every media item,
verify delivery to the configured destinations, and report when the source is
ready for a separately proven cleanup operation.

The intended runtime is an always-running service on a NAS, server, or VM.
Plugging in a camera should be the only routine operator action; the CLI
commands below are diagnostics and development tools for the same detection and
inventory boundaries used by that service.

The project will also expose the useful control surfaces already available for
these cameras: Action 6 BLE control/status through DJI's R SDK, Pocket 3
community control, wireless browsing and transfer, and USB/UVC capture.

## Status

The wired, non-destructive ingest path recognizes hardware-observed Action 6
and Pocket 3 USB mass-storage presentations, inventories an already mounted
camera, and durably delivers every regular file to one or more local or
S3-compatible archives, with optional verified Immich catalog delivery. It
records full SHA-256 evidence and verifies every destination before reporting
completion. Local archive staging is process-locked and safely reclaims djpi's
own incomplete temp files after abrupt termination. An offline, fixture-backed
wireless protocol foundation can decode
captured media manifests for both cameras, but does not yet connect to a camera.
An offline Action 6 R SDK codec validates status frames and constructs the
official status subscription, while intentionally providing no BLE transport.
The daemon also serves an optional read-only status page and health endpoints.
djpi never deletes or formats camera storage.

The Pocket 3 local archive path is [hardware-verified end to
end](docs/hardware/pocket-3-wired-ingest.md): 9 files totaling 8.57 GB were
copied and independently verified, and an unchanged second pass skipped all 9.

## First vertical slice

1. Detect an Action 6 or Pocket 3 connected over USB.
2. Inventory all media without changing it.
3. Copy to every configured local, S3-compatible, and Immich destination.
4. verify the copied size and content digest.
5. Persist the result so reconnecting is idempotent.
6. Re-inventory the Source and reconcile any change before completion.
7. Report whether cleanup is safe, without performing cleanup yet.

See [docs/PROJECT.md](docs/PROJECT.md) for the capability map and roadmap.

## Development

```console
go test ./...
go run ./cmd/djpi version
go run ./cmd/djpi probe
go run ./cmd/djpi probe --json
go run ./cmd/djpi inventory
go run ./cmd/djpi inventory --source /path/to/existing/source
go run ./cmd/djpi inventory --source /path/to/existing/source --json
go run ./cmd/djpi serve
go run ./cmd/djpi serve --once
go run ./cmd/djpi serve --config djpi.example.json
go run ./cmd/djpi status --config djpi.example.json
go run ./cmd/djpi status --config djpi.example.json --json
go run ./cmd/djpi audit --config djpi.example.json
go run ./cmd/djpi audit --config djpi.example.json --json
```

Run the complete local CI gate through the pinned Nix development shell:

```console
nix develop --command ./scripts/check.sh
```

## Releases

Merges to `main` are validated and released automatically from Conventional
Commit history. Semantic release creates `vMAJOR.MINOR.PATCH` tags and Forgejo
release notes, and commits the matching version to `VERSION` and the Nix
package metadata. `feat` commits produce minor releases, `fix` commits produce
patch releases, and Conventional Commit breaking changes produce major
releases. Pull requests run the same validation without publishing.

Nix consumers should use the repository as a normal flake input and let their
`flake.lock` pin the resolved commit. They should not hard-code a `rev` query in
the input URL, because that prevents `nix flake update` from advancing it.

`djpi probe` performs a read-only inventory of Linux USB descriptors, block
devices, mounted filesystems, and MTP-related evidence. It does not mount,
copy, delete, or format anything. The JSON form is intended for recording one
camera at a time during hardware characterization; djpi does not guess camera
identity from unverified USB IDs.

`djpi inventory` correlates repository-verified camera USB evidence with Linux
block-device serial evidence and automatically selects exactly one mounted
Source. It fails closed if no Source is mounted or more than one is eligible;
`--source` remains available for explicit characterization. It walks the
already-mounted Source without opening file contents
and records every regular file's relative path, size, and modification time.
Its metadata evidence ID can detect changes between observations, but it is not
a content digest, Delivery verification, or permission to perform Cleanup. The
command does not mount a Source and reports an incomplete inventory if it finds
an unreadable or unsupported entry such as a symlink.

`djpi serve` is the long-running station process. It polls the same fail-closed
hardware boundary, emits newline-delimited JSON state events, inventories a
Source once per attachment, and resets after removal. The current service is
read-only unless a configuration is supplied. With configured Destinations, it
stores immutable objects under a camera and content-digest namespace and
records the evidence in a crash-safe journal. Local files are atomically
committed and independently re-hashed, with a fail-fast destination-capacity
check before staging. S3-compatible objects use multipart
upload with SDK transfer checksums plus djpi SHA-256 and size metadata verified
through `HeadObject`, followed by a streamed `GetObject` SHA-256 read-back.
Reconnects skip a Delivery only after both Source and Destination evidence
still match the journal. See [`djpi.example.json`](djpi.example.json) and
[`docs/configuration.md`](docs/configuration.md).

Destination failures are journaled and retried with exponential backoff from
10 seconds to 5 minutes while the camera remains attached. A successful ingest,
camera removal, or new attachment resets the backoff. Each destination call
also has a configurable finite deadline (six hours by default), so a stalled
network connection cannot wedge the daemon indefinitely.

After every apparently successful ingest, the station performs a fresh metadata
inventory. It emits `ingest_complete` only when that evidence still matches;
otherwise it adopts the new complete snapshot and reconciles it on the next
cycle. A failed or incomplete re-inventory blocks completion.

When `status_listen` is configured, `djpi serve` exposes a read-only dashboard,
JSON status, and health checks. The example binds to loopback because this
surface intentionally has no application-level authentication or mutation
routes. The daemon view includes its live lifecycle state, current camera and
snapshot size, current item/destination/phase, last problem, and scheduled retry
time while the durable journal continues to supply delivery evidence. Durable
status also counts missing configured deliveries and shows up to 20 sorted
item/destination failure details for remote troubleshooting.

`djpi audit --config ...` independently re-verifies every journaled item at
every currently configured destination without requiring an attached camera.
It reads but never rewrites the journal, emits deterministic human or JSON
results, and exits nonzero if delivery evidence is missing, inconsistent, no
longer reachable, or no longer matches the stored bytes. Local archives are
re-hashed; S3 objects are streamed back through SHA-256; Immich assets are
checked through its live asset API.

Journal Source identities use a one-way digest of the hardware-correlated
camera serial; raw camera serials are not written into correlated station
events or journal keys.

Cleanup is not implemented or enabled by any configuration. A completed ingest
event means the configured deliveries verified; it is not authorization to
delete or format the camera.

For host, VM, and container placement guidance, see
[`docs/deployment.md`](docs/deployment.md). For NixOS read-only automount,
secret injection, and operator commands, see
[`docs/deployment-nixos.md`](docs/deployment-nixos.md).
The implemented and intentionally absent wireless behavior is recorded in
[`docs/wireless.md`](docs/wireless.md).
The corresponding Action 6 control boundary is in
[`docs/action6-control.md`](docs/action6-control.md).
