# Osmo Action 6 full wired ingest evidence

Observed on 2026-08-21 using djpi commit `60024b5` on the NixOS Station
`srv-nas-ix`. This is the first end-to-end ingest performed by the deployed
systemd service against its own read-only automount, rather than by a
foreground command against a desktop-managed mount.

It validates the module's udev automount, the S3 destination, and the
idempotent second pass on real hardware. It does not authorize camera cleanup.

## Source and destination

- Hardware-correlated source: Action 6 USB `2ca3:8002`, filesystem LUN
  `/dev/sdi`, exFAT, label `OsmoAction`, 54,028,767,232 bytes.
- The camera's observed zero-size second LUN `/dev/sdj` was correctly ignored.
- Station-mounted read-only by `djpi-mount@sdi.service` at
  `/run/djpi/media/sdi` with `ro,nosuid,nodev,noexec,noatime`. djpi itself has
  no mount capabilities.
- Camera identity: `camera-sha256:81fa9cdac0594f420f7b90ed004a4765e094cf45061eeca9cb9641a92db4fa0c`.
- Snapshot evidence:
  `metadata-sha256:57611b751bebc1ef71649191110b16ec7a51e61f0d22b994c505f55080f60f8e`.
- Snapshot contents: 293 regular files totaling 8,427,200,081 bytes.
- Destination: the `objects` S3 destination, RustFS at `127.0.0.1:9000`,
  bucket `apps`, prefix `djpi`, which held zero objects under that prefix
  before the run.

## First ingest

The service emitted `source_detected`, `inventory_complete`, `ingest_started`,
and then `ingest_complete` about ten minutes later. The completion event
reported:

```text
files=293 bytes=8427200081 verified=293
```

`ingest_complete` was emitted only after the mandatory post-ingest inventory
produced the same metadata evidence ID as the original snapshot.

Independent inspection after completion found:

- 293 verified, 0 failed, and 0 pending journal deliveries.
- 8.48 GB stored under the `apps/djpi` prefix.
- The source still mounted read-only and still holding exactly 293 files.
- Cleanup still reported `implemented=false` and `authorized=false`.

## Independent destination audit

`djpi-audit.service` re-read every object from S3 and re-verified every digest,
consuming 21s CPU over 25s wall clock and 7.8 GB of incoming traffic:

```text
expected=293 verified=293 pending=0 failed=0
```

## Idempotent second pass

Restarting `djpi.service` against the unchanged attached camera re-detected the
source, produced the same metadata evidence ID, re-verified all 293 deliveries
from durable journal evidence, and uploaded nothing. The destination object
count was unchanged.

## Multipart checksum warning

Both ingest and audit logged `Skipped validation of multipart checksum` from
the AWS SDK for larger items. That is the SDK declining to validate the
server's multipart checksum. It does not weaken djpi's guarantee, because djpi
independently re-reads and re-hashes every delivered object; the audit above is
that check performed a second time from a separate process.

## Correlation defect found by this run

The first ingest attempt did not start. The service reported:

```text
multiple mounted camera sources are ambiguous: /run/djpi/media/sdi, /run/djpi/media/sdi
```

The hardened service runs in its own mount namespace, where mount propagation
lists the camera mount twice: once under `/run` and once under a bind of its
parent directory. `/proc/<pid>/mountinfo` showed two entries for device `8:128`
inside the service namespace and one entry in the initial namespace, which is
why a foreground `djpi probe` disagreed with the daemon.

Correlation produced one candidate per mountpoint entry, so `SingleMountedSource`
saw two identical candidates and failed closed, naming the same path twice.
Every ingest attempt was blocked. Commit `60024b5` collapses candidates that
share a camera, block path, and mountpoint; genuinely distinct mountpoints
still fail closed.

## Remaining characterization

- Repeat after recording new media to test identity and path stability.
- Exercise a source that changes mid-ingest to observe `source_changed`
  reconciliation on this Station.
- Prove any cleanup operation independently on expendable media only after the
  repository safety gate passes.
