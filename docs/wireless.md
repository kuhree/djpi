# Wireless protocol boundary

djpi's first wireless increment is deliberately offline and non-destructive.
It establishes tested protocol primitives that can later sit behind the same
inventory, delivery, verification, and journal boundaries as wired ingest.

## Implemented

- CRC-validated DUML frame parsing and encoding, including the 10-bit length
  field and the transport-specific sequence byte order (BLE big-endian, UDP
  little-endian).
- Length-prefixed UTF-8 string packing used by DJI wireless payloads.
- Decoding already reassembled `0x00/0x27` CompositePack media manifests for
  the Action 6 and Pocket 3.
- Model-specific Action 6 storage resolution between SD and internal storage.
- Fail-closed rejection of incomplete streams, unsupported profiles, malformed
  records, unsafe paths, duplicate paths, and colliding private handles.
- Construction of escaped download URLs on the fixed camera-local
  `http://192.168.2.1/v2` endpoint.

The decoder is tested against unmodified hardware captures from Osmosis for
both cameras. Parsed private file handles are used only for record validation
and Action 6 storage resolution; djpi does not expose them as deletion handles.

## Not implemented

- BLE discovery, GATT connection, app-level pairing, or camera wake.
- Joining or managing the camera Wi-Fi access point.
- The UDP datalink handshake, registration, sequencing, or media-list request.
- HTTP download integration with the ingest engine.
- Record, mode, gimbal, GPS, sleep, wake, or other camera controls.
- Per-file deletion or whole-storage format.

No code in this package performs network, Bluetooth, filesystem, or camera I/O.
Live wireless support will require camera interaction to validate pairing,
session behavior, interruption recovery, and complete stream reassembly before
it can feed media into the existing ingest engine.

## Provenance

The framing and manifest behavior is adapted from
[Osmosis](https://github.com/KonradIT/osmosis) revision
`a76123441f3c0d476de949da36afdcace310a83e` (v1.4.1). See
[`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md) for license and fixture
provenance.

The implementation intentionally takes only the protocol knowledge needed for
safe listing and download. Osmosis's guarded deletion work is evidence that a
future cleanup mechanism may be feasible; it is not proof that djpi's safety
gate passes, and it is not included here.
