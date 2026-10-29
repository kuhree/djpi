# Review

Review is how the operator finds one Media Item in a large archive, judges it and pulls it out to edit or share. djpi builds no browsing UI. A second Immich account owns a read-only external library over djpi's local archive, and that library is the Review surface.

## How it works now

- **Two accounts, one Immich.** The primary account holds finished photography; no raw camera media reaches it automatically. The staging account's external library imports the local archive root read-only, so Immich indexes files in place and stores no second copy. Its timeline comes from EXIF and container metadata.
- **The archive must be local.** An external library needs a POSIX path, so a local Destination on the NAS pool (backed up with Immich) is required for Review. S3 stays supported as an optional offsite copy and cannot serve Review.
- **Layout.** `internal/destination/local.go` writes `<namespace>/DCIM/DJI_001/<stem>.<digest>.<ext>`, mirroring the Source. `.staging` (partial transfers, the archive lock) must be excluded from the library.
- **Exclusions are Immich configuration.** djpi archives and verifies every regular file. What the operator wants to look at is the library's job: exclude `.staging`, low-resolution proxies, subtitle and telemetry sidecars and camera thumbnails.
- **Selection.** A selected item is an Immich favourite; a rejected one is archived (hidden, file untouched). djpi reads and stores neither. Selections are not keyed by digest and do not survive rebuilding the library.
- **Pull, edit, share.** An Immich download, or the archive path on the Station. A rough cut is a shared link with an expiry. Promotion to the primary library is manual: Immich cannot move assets between accounts.
- **Not built:** an album per Ingest ([#14](https://git.kuhree.com/kuhree.com/djpi/issues/14)).

## Laws

- Review reads the archive. It never writes to it, never touches a Source and never joins Ingest. A scan is not a Delivery and adds nothing to Ready for Cleanup.
- Losing the staging library loses no Media Item and no verification.
- Selection never participates in cleanup. Rejected items are still archived and verified: rejection saves attention, never storage.
- Album bookkeeping is cosmetic. It never blocks, fails or delays a Delivery.

## Why

| When | Decision | Reason |
|---|---|---|
| ADR-0002 | Selection never authorizes cleanup | Letting rejection free storage makes one click the difference between keeping and destroying media, and puts a momentary judgment on par with a SHA-256 check. Only evidence authorizes destruction. |
| ADR-0004 | Immich is the Review surface, not a djpi UI | It already has thumbnails, transcodes, a phone app, favourites, archive states, shared links and exclusion patterns. A UI would cost a video scrubber, ffmpeg and a derivative pipeline. A cheap experiment, to be dropped if its ceiling hurts. |

Replace it with a djpi surface if Selections need journal durability, if the operator needs state Immich lacks (verification status, Delivery coverage), or if running the library costs more than the browsing it gives.

## Code

Nothing yet beyond the local and Immich destinations in `internal/destination/`.
