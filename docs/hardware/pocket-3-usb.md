# Osmo Pocket 3 USB storage evidence

Observed on 2026-08-20 with one DJI Osmo Pocket 3 connected to the NixOS host
`nixtop` in its USB storage mode. This record covers one unit and connection
mode; identifiers and interfaces must be rechecked across firmware and other
USB modes.

No destructive operation was attempted. `djpi probe` did not mount the Source;
the desktop auto-mounted it before inspection. Serial values are omitted.

## USB identity

| Property | Observed value |
| --- | --- |
| USB vendor/product | `2ca3:0020` |
| Manufacturer | `DJI` |
| Product descriptor | `DJIPocket3` |
| Device class/subclass/protocol | `00/00/00` |
| Interface 0 | `e0/01/03` |
| Interface 1 | `0a/00/00` |
| Interface 2 | `08/06/50` (USB mass storage) |
| Interfaces 3–7 | `ff/43/01` |

udev summarized the interfaces as
`ID_USB_INTERFACES=:e00103:0a0000:080650:ff4301:`.

## Linux storage presentation

The USB serial associated the camera with two removable File-Stor Gadget block
nodes:

| Node | Capacity | Filesystem | Label | Observation |
| --- | ---: | --- | --- | --- |
| `/dev/sdd` | 127,982,895,104 bytes | exFAT | none | Media Source |
| `/dev/sde` | 0 bytes | none | none | Second LUN; no media |

The desktop auto-mounted the Source at `/run/media/kuhree/3837-3461` with
read-write, `noexec`, and `noatime` options. The mount directory reflects the
filesystem UUID rather than a camera label and must not be used as camera
identity.

No MTP/PTP presentation was observed in this mode.

On the filesystem-bearing block node, udev reported `ID_BUS=usb`,
`ID_VENDOR_ID=2ca3`, `ID_MODEL_ID=0020`, `ID_FS_USAGE=filesystem`, and
`ID_FS_TYPE=exfat`. The zero-size LUN had the same USB IDs but no filesystem
usage or type. These hardware-observed properties are the complete match used
by the NixOS read-only automount rule; it does not match arbitrary removable
storage.

## Source layout

```text
DCIM/
  DJI_001/
MISC/
  IDX/
  THM/
    DJI_001/
```

Read-only metadata aggregation reported 9 files totaling 8,566,818,586 bytes:

| Extension | Files | Bytes |
| --- | ---: | ---: |
| MP4 | 2 | 7,581,966,612 |
| LRF | 2 | 983,904,737 |
| SCR | 2 | 903,807 |
| THM | 2 | 2,470 |
| DB | 1 | 40,960 |

The matching MP4/LRF and SCR/THM counts reinforce that companion and metadata
files must remain represented in a complete inventory even when later policy
distinguishes primary media for Delivery. This observation provides no cleanup
authority.

The same snapshot was subsequently exercised through the complete local archive
pipeline; see the [full wired ingest evidence](pocket-3-wired-ingest.md).

## Remaining characterization

- Repeat after reconnecting and after recording new photo and video modes.
- Record firmware version and charging/screen-lock behavior.
- Characterize UVC and any other USB modes separately.
- Test source stability during a long read on expendable media.
- Prove cleanup operations independently only after the safety gate passes.
