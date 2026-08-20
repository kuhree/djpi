# Action 6 control boundary

DJI officially lists the Osmo Action 6 as supported by its R SDK. djpi now has
the first transport-independent pieces needed to use that surface safely.

## Implemented

- Parse and encode unencrypted R SDK frames with the 10-bit length field,
  little-endian sequence number, header CRC-16, and whole-frame CRC-32.
- Refuse unsupported versions, encryption, reserved-header variants, malformed
  lengths, trailing data, and either CRC mismatch.
- Build the inert periodic 2 Hz camera-status subscription frame.
- Decode numeric `0x1d/0x02` camera status fields including mode, recording
  state, elapsed time, resolution index, FPS index, and battery percentage.
- Decode the Action 6 text-mode `0x1d/0x06` status layout with strict ASCII and
  field-length validation.
- Verify framing byte-for-byte against golden vectors generated from DJI's
  official demo by Osmosis.

The package performs no Bluetooth or camera I/O. It cannot start or stop
recording, change modes, wake or sleep a camera, or push GPS data.

## Required hardware work

Before controls can become a user-facing djpi feature, the Action 6 must be
available to validate Linux BLE discovery, GATT notification/write behavior,
on-camera connection approval, sequence handling, disconnect recovery, and the
exact firmware's status layouts. Any future mutating control must be explicit
and independently confirmed from a camera response; it will not be part of the
automatic ingest path.

The R SDK framing is distinct from the `0x55` DUML media protocol described in
[`wireless.md`](wireless.md), even though both can use DJI's BLE GATT service.
See [`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md) for the Osmosis and
DJI provenance and license notices.
