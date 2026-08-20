# djpi project brief

## Product promise

Connect an Action 6 or Pocket 3 to a Raspberry Pi and walk away. djpi discovers
the media, delivers it to the chosen services, makes progress and failures
visible, and clears the camera only after it can prove the configured copies
are intact.

This is intentionally a project for these two cameras. Shared code is welcome,
but a generic camera-plugin framework is not an MVP goal.

## Capability map

| Surface | Action 6 | Pocket 3 | Initial use |
| --- | --- | --- | --- |
| USB media export | Officially supported | Officially supported | Wired ingest |
| microSD reader | Supported storage | Supported storage | Fastest fallback ingest |
| UVC webcam | Officially supported | Officially supported | Later capture/streaming |
| Wireless media | Osmosis hardware-verified | Osmosis hardware-verified | Phase 2 ingest |
| Camera-file deletion | Osmosis hardware-verified with safeguards | Hardware-verified, but some stills lack safe handles | Cleanup candidate |
| Whole-storage format | Not proven remotely | Not proven remotely | Hardware spike required |
| BLE control/status | Official DJI R SDK | Community protocol only | Later control |
| Gimbal control | Not applicable | Community implementation with caveats | Later experimentation |
| RTMP configuration | Community implementations | Community implementations | Later streaming |

## Components

- **Action 6 integration**: USB discovery, storage inventory, DJI R SDK control,
  and later Osmosis-compatible wireless media access.
- **Pocket 3 integration**: USB discovery, storage inventory, wireless media,
  and later gimbal/control experiments.
- **Ingest engine**: resumable transfer journal, content identity, delivery
  verification, deduplication, and cleanup gating.
- **S3 destination**: S3-compatible multipart delivery with checksums and
  idempotent object naming.
- **Immich destination**: upload through Immich's supported API without making
  Immich's catalog state the only preserved copy.
- **Operator surface**: structured service events, human and JSON status
  commands, and an optional read-only local web UI with health checks;
  notifications remain later work.

## Runtime model

djpi is primarily an always-running Raspberry Pi station service. Its steady
state is idle observation; connecting a camera starts detection, Source
inventory, Delivery, and verification without requiring an operator command.
The `probe` and `inventory` commands are diagnostic surfaces for hardware
characterization and service troubleshooting, not the routine product flow.

The service must fail closed on unknown or ambiguous hardware and remain
non-destructive unless the cleanup safety gate and the independently proven
camera-specific cleanup mechanism both permit an operation.

The `djpi serve` loop polls the Linux evidence boundary and emits structured
events for Source detection, completed inventory, removal, and blocked states.
The NixOS module uses hardware-specific udev events to request a safe read-only
mount, after which the same polling state machine detects the Source. A later
event-driven wake-up may reduce idle polling without changing that state
machine.

## Delivery phases

### Phase 0: hardware characterization

- Record USB identities and Linux modes for both cameras.
- Determine whether each storage area appears as block storage, MTP, or another
  interface.
- Measure read behavior while the camera is charging and screen-locked.
- Prove what cleanup operations are available over wired and wireless paths.
- Exercise the NixOS read-only automount and full ingest on the Raspberry Pi.

Observed hardware notes are kept under [`docs/hardware`](hardware/), including
the USB mass-storage presentations for the [Action 6](hardware/action-6-usb.md)
and [Pocket 3](hardware/pocket-3-usb.md).

### Phase 1: wired, non-destructive ingest

- React to USB or SD-reader attachment.
- Build a complete Source Snapshot.
- Transfer to one or more local or S3-compatible destinations with a durable
  journal.
- Resume safely after disconnect, destination outage, or process restart.
- Verify content and report ingest completion without treating that report as
  Cleanup authorization or changing the source.

### Phase 2: destinations and controlled cleanup

- Add multiple required/optional destinations.
- Add Immich delivery without allowing its catalog to be the only preserved
  copy. (Implemented for the wired service.)
- Implement the proven cleanup mechanism behind an explicit configuration flag.
- Re-snapshot immediately before cleanup and refuse on any change or ambiguity.

### Phase 3: wireless ingest

- Port the required Osmosis BLE/Wi-Fi/DUML/HTTP behavior to the Pi service.
  The CRC-validated DUML codec and hardware-fixture-backed manifest decoder are
  implemented offline; live BLE, Wi-Fi, UDP, and HTTP orchestration remain.
- Preserve the same journal and cleanup invariants as wired ingest.
- Handle the camera AP displacing normal Wi-Fi, preferably with Ethernet or a
  second Wi-Fi interface for destination connectivity.

See [`docs/wireless.md`](wireless.md) for the exact implemented safety boundary
and upstream revision.

### Phase 4: use-the-camera features

- Action 6 status, record, modes, sleep/wake, and optional GPS through R SDK.
- Pocket 3 gimbal and camera-control experiments.
- UVC capture, previews, RTMP, thumbnails, and event-driven automation.

## Upstream references

- [DJI Osmo Action 6 downloads](https://www.dji.com/downloads/products/osmo-action-6)
- [DJI Osmo Pocket 3 downloads](https://www.dji.com/downloads/products/osmo-pocket-3)
- [DJI Osmo GPS Controller Demo](https://github.com/dji-sdk/Osmo-GPS-Controller-Demo)
- [Osmosis](https://github.com/KonradIT/osmosis)
- [Immich OpenAPI specification](https://github.com/immich-app/immich/blob/main/open-api/immich-openapi-specs.json)
- [node-osmo](https://github.com/datagutt/node-osmo)
- [lib-osmo-ble](https://github.com/yigitkonur/lib-osmo-ble)
- [djictl](https://github.com/xaionaro-go/djictl)
