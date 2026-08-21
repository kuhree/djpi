# Review

Review is how an operator finds one Media Item in a large archive, judges it,
and pulls it out to edit or share. djpi does not implement it. A second Immich
account owns a read-only external library over djpi's local archive, and that
library is the Review surface. See
[ADR-0004](adr/0004-immich-staging-library-as-review-surface.md) for why, and
[ADR-0002](adr/0002-selection-never-authorizes-cleanup.md) for the safety
boundary this must not cross.

## Boundary

Review reads the archive. It never writes to it, never touches a Source, and
never participates in Ingest. An Immich external-library scan is not a Delivery:
it produces no Evidence, and it contributes nothing to Ready for Cleanup. If the
staging library were lost entirely, no Media Item and no verification would be
lost with it.

This is the property that makes the whole arrangement safe to experiment with,
and it is the one that must not be traded away for convenience later.

## Shape

Two accounts on one Immich deployment.

The **primary account** is the operator's existing photography and finished
work. Nothing in this document changes it, and no raw camera media reaches it
automatically.

The **staging account** owns one external library whose import path is the
local archive root. It is read-only, so Immich indexes the files where they lie
and stores no second copy of the bytes. Its timeline is built from EXIF and
container metadata, so it is chronologically correct regardless of the archive's
directory layout.

## What the archive must provide

The local archive Destination becomes required rather than optional, because an
Immich external library can only index a POSIX filesystem path. It should live
on the same NAS pool as Immich's own data, which is already backed up.

The S3 destination remains supported and tested, and is now an optional offsite
copy rather than the primary path. It cannot serve Review.

The archive layout is unchanged. `internal/destination/local.go` writes
`<namespace>/DCIM/DJI_001/<stem>.<digest>.<ext>`, mirroring the Source, and that
is exactly what an external library wants. The content digest embedded in each
filename is cosmetic noise in a gallery and is otherwise harmless.

djpi's own bookkeeping must stay invisible to Immich. The `.staging` directory
holds partial transfers and the archive lock, and it must be excluded from the
library so half-written files are never indexed.

## Exclusions

The problem that started this work is unwanted files reaching a catalog. It is
solved in the staging library's exclusion patterns, not in djpi, because it is a
question about what the operator wants to look at rather than what is worth
preserving. djpi continues to archive and verify every regular file on the
Source without exception.

At minimum the library excludes djpi's `.staging` directory, camera low-
resolution proxy files, subtitle and telemetry sidecars, and camera-written
thumbnail files. The exact set is configuration and is expected to change as the
cameras are used.

## Album per Ingest

This is the only code djpi contributes, and the only part that is not
configuration.

The archive mirrors the Source, so `DCIM/DJI_001/` accumulates across every
connection and a single Ingest is not identifiable afterwards. Immich's date
timeline covers most of the need, but an album per Ingest preserves the answer
to "what came off the camera that time" independently of capture dates, which
matters when one connection carries media shot across several days.

After an Ingest completes and every required Delivery is verified, djpi creates
an album in the staging account and adds that Ingest's Media Items to it.

Three things make this harder than it looks and must be handled explicitly:

- **The scan is asynchronous.** Immich discovers external-library files on its
  own schedule, so the assets may not exist when the Ingest completes. Album
  creation must tolerate assets that are not yet indexed and converge later
  rather than failing the Ingest.
- **Identity must be by content, not path.** Media Items are matched to Immich
  assets by the SHA-256 djpi already computes, consistent with how djpi
  identifies everything else.
- **Failure must be inert.** Album bookkeeping is cosmetic. If the Immich API is
  unreachable, or the staging account is misconfigured, or a Media Item is
  excluded from the library, the Ingest is unaffected and reports success. This
  operation can never block, fail, or delay a Delivery.

## Selection

Selection is recorded in Immich, using states it already has. A selected Media
Item is a favourite. A rejected one is archived, which hides it from the
timeline while leaving the file untouched. Unexamined is the default and needs
no representation.

djpi does not read these states and stores nothing about them. That is a
deliberate limit of this design: Selections live in Immich's database rather
than djpi's journal, so they are not keyed by content digest and will not
survive a rebuild of the staging library. That was acceptable in exchange for
writing no UI, and it is the first thing that would push toward a purpose-built
surface if it starts to hurt.

## Pull, edit, share

Pulling an original for editing is an Immich download, from the web interface at
a desk or from the mobile application anywhere. On the Station itself the
archive path works directly.

Sharing a rough cut is an Immich shared link with an expiry.

Promotion of finished work into the primary library is manual. Immich cannot
move assets between accounts, and a finished edit is a new file that never
existed in the archive, so there is nothing sensible to automate until an
editing workflow actually exists. A watched edit-inbox Destination delivering to
the primary account is the obvious shape if it becomes worth building.

## Assumptions to verify before implementing

These are stated from knowledge of Immich rather than from testing against the
deployed version, and each should be confirmed:

- External libraries are owned per user, accept a filesystem import path, index
  read-only without copying, and support exclusion patterns.
- The Immich API can create an album in the staging account and add
  external-library assets to it, and assets can be located by checksum.
- Immich derives timeline dates from Action 6 and Pocket 3 video containers,
  not merely from filesystem timestamps.
- Immich tolerates unfamiliar DJI sidecar files it is asked to index, if any
  escape the exclusion patterns.

## When to abandon this

This design is deliberately cheap and deliberately disposable. It should be
replaced with a purpose-built Review surface in djpi if Selections start
mattering enough to need journal durability, if the operator needs to see djpi
state Immich has no concept of such as verification status or Delivery
coverage, or if operating the staging library becomes more work than the
browsing it provides.
