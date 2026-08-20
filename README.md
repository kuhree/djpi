# djpi

`djpi` is a Raspberry Pi camera station for the DJI Osmo Action 6 and
DJI Osmo Pocket 3.

The target experience is simple: connect a camera, ingest every media item,
verify delivery to the configured destinations, and safely clear the source so
the camera is ready for the next shoot.

The intended runtime is an always-running service on the Raspberry Pi. Plugging
in a camera should be the only routine operator action; the CLI commands below
are diagnostics and development tools for the same detection and inventory
boundaries used by that service.

The project will also expose the useful control surfaces already available for
these cameras: Action 6 BLE control/status through DJI's R SDK, Pocket 3
community control, wireless browsing and transfer, and USB/UVC capture.

## Status

The repository is newly scaffolded. It currently contains the domain safety
gate and the researched implementation plan; it does not yet connect to or
modify a camera.

## First vertical slice

1. Detect an Action 6 or Pocket 3 connected over USB.
2. Inventory all media without changing it.
3. Copy to one configured S3-compatible destination.
4. verify the copied size and content digest.
5. Persist the result so reconnecting is idempotent.
6. Report whether cleanup is safe, without performing cleanup yet.

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
```

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
read-only: it has no Destination delivery or Cleanup implementation yet.
