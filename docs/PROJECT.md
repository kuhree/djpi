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
- **Operator surface**: CLI first, then a small local web UI and notifications.

## Runtime model

djpi is primarily an always-running Raspberry Pi station service. Its steady
state is idle observation; connecting a camera starts detection, Source
inventory, Delivery, and verification without requiring an operator command.
The `probe` and `inventory` commands are diagnostic surfaces for hardware
characterization and service troubleshooting, not the routine product flow.

The service must fail closed on unknown or ambiguous hardware and remain
non-destructive unless the cleanup safety gate and the independently proven
camera-specific cleanup mechanism both permit an operation.

## Delivery phases

### Phase 0: hardware characterization

- Record USB identities and Linux modes for both cameras.
- Determine whether each storage area appears as block storage, MTP, or another
  interface.
- Measure read behavior while the camera is charging and screen-locked.
- Prove what cleanup operations are available over wired and wireless paths.

Observed hardware notes are kept under [`docs/hardware`](hardware/), including
the USB mass-storage presentations for the [Action 6](hardware/action-6-usb.md)
and [Pocket 3](hardware/pocket-3-usb.md).

### Phase 1: wired, read-only ingest

- React to USB or SD-reader attachment.
- Build a complete Source Snapshot.
- Transfer to one S3-compatible destination with a durable journal.
- Resume safely after disconnect, destination outage, or process restart.
- Verify content and report Ready for Cleanup without changing the source.

### Phase 2: destinations and controlled cleanup

- Add multiple required/optional destinations.
- Add Immich delivery.
- Implement the proven cleanup mechanism behind an explicit configuration flag.
- Re-snapshot immediately before cleanup and refuse on any change or ambiguity.

### Phase 3: wireless ingest

- Port the required Osmosis BLE/Wi-Fi/DUML/HTTP behavior to the Pi service.
- Preserve the same journal and cleanup invariants as wired ingest.
- Handle the camera AP displacing normal Wi-Fi, preferably with Ethernet or a
  second Wi-Fi interface for destination connectivity.

### Phase 4: use-the-camera features

- Action 6 status, record, modes, sleep/wake, and optional GPS through R SDK.
- Pocket 3 gimbal and camera-control experiments.
- UVC capture, previews, RTMP, thumbnails, and event-driven automation.

## Upstream references

- [DJI Osmo Action 6 downloads](https://www.dji.com/downloads/products/osmo-action-6)
- [DJI Osmo Pocket 3 downloads](https://www.dji.com/downloads/products/osmo-pocket-3)
- [DJI Osmo GPS Controller Demo](https://github.com/dji-sdk/Osmo-GPS-Controller-Demo)
- [Osmosis](https://github.com/KonradIT/osmosis)
- [node-osmo](https://github.com/datagutt/node-osmo)
- [lib-osmo-ble](https://github.com/yigitkonur/lib-osmo-ble)
- [djictl](https://github.com/xaionaro-go/djictl)
