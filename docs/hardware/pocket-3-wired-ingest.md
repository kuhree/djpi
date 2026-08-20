# Osmo Pocket 3 full wired ingest evidence

Observed on 2026-08-20 using djpi commit `7e2683f` on the NixOS host
`nixtop`. This validates the complete userspace ingest path against one real
Pocket 3 snapshot. It does not validate the target Station's automount unit or
authorize camera cleanup.

The desktop had already mounted the camera read-write at
`/run/media/kuhree/3837-3461`. djpi warned about that mount and performed only
read operations against it. No camera file, directory, filesystem, or setting
was modified.

## Source and destination

- Hardware-correlated source: Pocket 3 USB `2ca3:0020`, filesystem LUN
  `/dev/sdd`, exFAT.
- Snapshot evidence:
  `metadata-sha256:6e5c23cebc39bc00e89fe80bc3cb53610e3e07c9dce415e2fd1e171f53645e79`.
- Snapshot contents: 9 regular files totaling 8,566,818,586 bytes.
- Destination: a new content-addressed local archive on nixtop's btrfs home
  filesystem, which had 747,747,045,376 bytes available before the run.
- Journal: a separate new state directory on the same host.

The validation paths are intentionally retained for inspection:

```text
/home/kuhree/djpi-hardware-validation-archive
/home/kuhree/.local/state/djpi-hardware-validation
```

## First ingest

`djpi serve --config ... --once` emitted `source_detected`,
`inventory_complete`, `ingest_started`, and then `ingest_complete` after about
six minutes. The completion event reported:

```text
files=9 bytes=8566818586 verified=9 skipped=0 failed=0
```

For each local item the implementation staged atomically, synchronized it,
re-read the source to prove stability, committed the content-addressed path,
and independently re-hashed the destination. It then performed the mandatory
post-ingest metadata inventory, which matched the original evidence ID.

Independent inspection after process exit found:

- 9 committed archive files totaling exactly 8,566,818,586 bytes.
- An empty `.staging` directory.
- 9 verified, 0 failed, and 0 pending journal deliveries.
- `all_destinations_ready=true`.
- Cleanup still reported `implemented=false` and `authorized=false`.

## Idempotent second pass

Running the same command again against the unchanged attached camera completed
after about two minutes and reported:

```text
files=9 bytes=8566818586 verified=0 skipped=9 failed=0
```

The second pass re-hashed the Source and every committed destination file before
skipping delivery. It left the archive at the same 9 files and byte count, left
staging empty, and did not replace the original journal verification times.

## Independent archive audit

After adding the read-only `djpi audit` command, a development build audited the
retained archive without the Source being part of the operation. It independently
re-hashed all 9 configured local deliveries and reported:

```text
expected=9 verified=9 pending=0 failed=0
```

The journal SHA-256 was
`beb5c9ff8226fe1cf2ba623e949e6b8649ff371da95ea7157d6003e8312d2355`
both before and after the audit, confirming the audit did not rewrite its
evidence.

## Abrupt termination and resume

A second fresh archive was started against the same snapshot and the djpi
process was sent `SIGKILL` during delivery. At termination, one 20,769,397-byte
object was already committed and journaled, and the next item had left an
83,886,080-byte `.staging/.djpi-*.tmp` fragment. This models abrupt process or
host loss rather than graceful cancellation.

On restart, djpi acquired its local archive lock, removed only the stale private
temp, independently reverified and skipped the committed object, and delivered
the other eight items. Completion reported:

```text
files=9 bytes=8566818586 verified=8 skipped=1 failed=0
```

Independent inspection found an empty staging directory and exactly 9 committed
files totaling 8,566,818,586 bytes. A subsequent destination audit verified all
9. Its journal SHA-256 remained
`d41b5870bdb7782469e06d2264e7717fc4de264b25ddd5ad4f9c35fa21cf2efe`
before and after that audit. The validation paths remain on `nixtop`:

```text
/home/kuhree/djpi-interruption-validation-archive
/home/kuhree/.local/state/djpi-interruption-validation
```

## Remaining proof

- Repeat through the NixOS module's read-only udev/systemd automount on the
  chosen NAS host or VM.
- Interrupt a large transfer by physically disconnecting the camera, then prove
  the same staging cleanup and resume behavior.
- Validate configured S3-compatible and Immich destinations with narrowly
  scoped real credentials.
- Repeat with the Action 6 when it is available.
