# djpi

This context names the concepts used to move media off a camera without
mistaking a successful copy for permission to destroy the source.

## Language

**Station**:
The always-running environment that observes Camera connections and coordinates
Ingest. It may be a physical host or an isolated guest.
_Avoid_: Raspberry Pi, appliance, worker

**Camera**:
An enrolled DJI Osmo Action 6 or DJI Osmo Pocket 3 that can provide media or
camera-control capabilities.
_Avoid_: Device, backend

**Source**:
One camera storage area presented during a connection, such as an SD card or
built-in storage.
_Avoid_: Card, drive

**Media Item**:
One photo, video, or associated camera file discovered on a Source.
_Avoid_: Asset, object, clip

**Ingest**:
The preservation of a Media Item at a Destination with matching identity and
content.
_Avoid_: Upload, import, sync

**Destination**:
A configured system that receives and preserves Media Items, such as S3 or
Immich.
_Avoid_: Target, route, sink

**Delivery**:
The state of one Media Item at one Destination.
_Avoid_: Copy, upload

**Source Snapshot**:
The complete set and identity of Media Items observed on one Source at a point
in time.
_Avoid_: Scan, manifest

**Cleanup**:
An irreversible operation that frees Source storage after every required
Delivery is verified against an unchanged Source Snapshot.
_Avoid_: Delete, format

**Ready for Cleanup**:
The state in which the cleanup policy is satisfied; it is not evidence that a
particular cleanup mechanism is supported by the Camera.
_Avoid_: Done, uploaded, safe

**Selection**:
The operator's recorded judgment of one Media Item, identified by content
digest: selected, rejected, or unexamined.
_Avoid_: Rating, flag, star, cull

**Review**:
The operator surface that presents already-ingested Media Items so Selections
can be made. It is not part of the Ingest path and never holds the only copy.
_Avoid_: Gallery, library, browser
