# Ingest

Connect an Action 6 or Pocket 3 to an x86_64 Linux Station and walk away. djpi finds the media, delivers it to the configured destinations, shows progress and failures, and says the camera is safe to clear only when it can prove the copies are intact. It is for these two cameras: shared code is fine, a camera-plugin framework is not a goal. Vocabulary (Station, Source, Media Item, Delivery, Cleanup) is in `CONTEXT.md`.

## How it works now

`djpi serve` is a long-running service on a NAS, server or VM. Idle is the steady state; a camera connecting starts detection, Source inventory, Delivery and verification with no command.

- **Detect.** The loop polls the Linux evidence boundary and emits structured events for Source detection, finished inventory, removal and blocked states. On NixOS, hardware-specific udev events request a safe read-only mount and the same loop then sees the Source.
- **Deliver.** Each Media Item goes to local, S3-compatible (multipart, checksums, idempotent names) and Immich destinations through a durable journal. It resumes after a disconnect, an outage or a restart. Failed deliveries retry with bounded exponential backoff, so an outage never makes a tight hashing loop.
- **Verify.** Content is checked by SHA-256 against every destination. A second complete Source inventory must match the one delivered. A changed snapshot is reconciled; a failed or incomplete one blocks completion.
- **Report.** Structured events, human and JSON status commands, and an optional read-only local web UI. `probe` and `inventory` are diagnostics, not the product flow.
- **Cleanup.** Not implemented. There is deliberately no cleanup setting ([#11](https://git.kuhree.com/kuhree.com/djpi/issues/11)).

| Surface | Action 6 | Pocket 3 | State |
|---|---|---|---|
| USB media export, microSD reader | supported | supported | wired ingest, working |
| Wireless media | Osmosis-verified | Osmosis-verified | offline codec only ([#12](https://git.kuhree.com/kuhree.com/djpi/issues/12)) |
| Per-file deletion | verified, with safeguards | verified; some stills lack a safe handle | not used |
| Whole-storage format | not proven remotely | not proven remotely | not used |
| BLE control | official R SDK | community protocol only | offline framing only ([#13](https://git.kuhree.com/kuhree.com/djpi/issues/13)) |
| UVC, RTMP, gimbal | | | later ([#13](https://git.kuhree.com/kuhree.com/djpi/issues/13)) |

Wired ingest is hardware-verified on `nixtop` with a Pocket 3: the complete local archive, reconnect idempotence on an unchanged snapshot, abrupt termination, stale staging reclamation and verified resume. Target-host automount and a mid-transfer disconnect are open ([#10](https://git.kuhree.com/kuhree.com/djpi/issues/10)). Hardware notes are in [`hardware/`](hardware/).

## Laws

- Never delete or format a Source unless the cleanup gate and an independently proven, camera-specific mechanism both allow it.
- Ready for Cleanup means every item in a complete Source Snapshot has a content-verified Delivery at every required Destination, and a fresh snapshot shows the Source unchanged. Nothing else counts: not a transfer that returned success, not a Selection, not an Immich scan.
- Fail closed on unknown or ambiguous hardware.
- Immich is never the only preserved copy; the config validator rejects it.
- Identity is by content (SHA-256), never by path or metadata.
- Camera control is never part of the automatic ingest path, and a mutating control is confirmed by a camera response.

## Why

| Ref | Decision | Reason |
|---|---|---|
| ADR-0001 | Gate cleanup behind an unchanged verified snapshot | A successful transfer is not proof of a copy. Per-file deletion is proven; a remote whole-card format is not. |
| ADR-0003 | djpi holds no editor-specific knowledge and generates no proxies | Getting originals into an editor is a Destination concern; format conversion is Destination configuration. |
| scope | Two cameras, no plugin framework | Shared code is welcome; a generic framework is not an MVP goal. |

## Code

`internal/`: `source` and `station` (detect and inventory) · `ingest` (engine, snapshot model) · `journal` · `destination` (local, S3, Immich) · `audit` · `config` · `status` and `operatorhttp` · `pathguard` · `probe` · `action6` and `wireless` (offline protocol codecs: [`action6-control.md`](action6-control.md), [`wireless.md`](wireless.md)). Entry point `cmd/djpi`. Operating it: [`usage.md`](usage.md), [`configuration.md`](configuration.md), [`deployment-nixos.md`](deployment-nixos.md).

References: DJI Osmo Action 6 and Pocket 3 downloads, the DJI Osmo GPS Controller Demo, [Osmosis](https://github.com/KonradIT/osmosis), [Immich OpenAPI](https://github.com/immich-app/immich/blob/main/open-api/immich-openapi-specs.json), [djictl](https://github.com/xaionaro-go/djictl).
