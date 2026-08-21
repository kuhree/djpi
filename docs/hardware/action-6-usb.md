# Osmo Action 6 USB storage evidence

Observed on 2026-08-20 with one DJI Osmo Action 6 connected to the NixOS host
`nixtop`. The camera was in the USB mode that presented its storage. This is
evidence from one unit and one connection mode, not an assertion that the IDs
or interfaces are stable across firmware versions or other USB modes.

No destructive operation was attempted. `djpi probe` did not mount the source;
the desktop had already auto-mounted it before the probe ran. Serial values are
intentionally omitted from this record.

## USB identity

| Property | Observed value |
| --- | --- |
| USB vendor/product | `2ca3:8002` |
| Manufacturer | `DJI` |
| Product descriptor | `OsmoAction6_SN:<suffix>` |
| Device class/subclass/protocol | `ef/02/01` |
| Interface 0 class/subclass/protocol | `ff/ff/30` |
| Interface 1 class/subclass/protocol | `08/06/50` (USB mass storage) |

The kernel and udev exposed both interfaces as
`ID_USB_INTERFACES=:ffff30:080650:`.

## Other observed USB modes

On 2026-08-21 the same camera model repeatedly enumerated on `srv-nas-ix` as
`2ca3:0025` instead, with product descriptor `OsmoAction6_SN:<suffix>` and
manufacturer `DJI`. That mode presented no mass-storage interface and created
no SCSI or block node, so no source was correlated and no mount was attempted.

Selecting the camera's storage USB mode changed the product ID to `2ca3:8002`
and produced the mass-storage presentation described above. Treat `2ca3:0025`
as a known non-storage mode rather than a detection failure.

In `2ca3:0025` mode the camera reported the placeholder USB serial
`123456789ABCDEF`. The storage mode `2ca3:8002` reported a stable per-unit
serial, which is the value correlation and camera identity depend on. Do not
rely on serials observed in non-storage modes.

## Linux storage presentation

The same USB serial associated the camera with two removable block nodes:

| Node | Capacity | Filesystem | Label | Observation |
| --- | ---: | --- | --- | --- |
| `/dev/sdd` | 54,028,767,232 bytes | exFAT | `OsmoAction` | Media source |
| `/dev/sde` | 0 bytes | none | none | Second File-Stor Gadget LUN; no media |

The desktop auto-mounted `/dev/sdd` at `/run/media/kuhree/OsmoAction` with
read-write, `noexec`, and `noatime` options. The NixOS station module now uses
the exact observed DJI USB identity plus filesystem evidence to request a
read-only, `nosuid,nodev,noexec` mount; this still requires validation on the
chosen NAS host or VM deployment.

No MTP/PTP USB interface, MTP mount, or installed MTP client was observed in
this mode.

## Source layout

Observed directories:

```text
DCIM/
  DJI_001/
LOST+FOUND/
MISC/
  IDX/
  THM/
    DJI_001/
```

Read-only filename/size aggregation reported 285 files totaling 8,380,705,966
bytes:

| Extension | Files | Bytes |
| --- | ---: | ---: |
| DNG | 62 | 4,354,702,112 |
| JPG | 62 | 215,830,528 |
| MP4 | 9 | 3,264,904,236 |
| LRF | 9 | 537,262,785 |
| SCR | 71 | 7,065,242 |
| THM | 71 | 855,047 |
| DB | 1 | 86,016 |

The DNG/JPG and MP4/LRF pair counts suggest that a later Source Snapshot must
define deliberately which DJI companion files are Media Items. This evidence
does not justify deleting any of them.

## Remaining characterization

- Repeat after recording new media and after reconnecting to test identity and
  path stability.
- Record the camera firmware version and behavior while charging or locked.
- Characterize other Action 6 USB modes separately, including UVC.
- Prove any cleanup operation independently on expendable media only after the
  repository safety gate passes.
