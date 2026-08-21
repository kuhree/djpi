# Use an Immich staging library as the Review surface

The operator needs to find, scrub, and pull one Media Item out of a large
archive, from a phone away from home and from a desk before editing. Immich
already provides every primitive that requires: thumbnails and transcodes, a
mobile application, favourites and archive states, shared links, and exclusion
patterns for unwanted file types. djpi will not build a browsing UI. Instead a
second Immich account owns a read-only external library pointed at djpi's local
archive, keeping raw footage out of the operator's photography timeline while
sharing one Immich deployment.

Building the surface into djpi was the alternative. It was rejected because
ADR-0002 already establishes that a Selection is a view concern that never
authorizes Cleanup, which removes the only reason the judgment needed to live
in djpi's journal. A purpose-built UI would cost a video-scrubbing front end, an
ffmpeg dependency, and a derivative-generation pipeline in exchange for
capabilities Immich ships today. This is explicitly a cheap experiment: it is
expected to reveal what the operator actually wants before any UI code is
written, and it is meant to be discarded if its ceiling becomes limiting.

## Consequences

An Immich external library requires a POSIX filesystem path, so a local archive
Destination on the NAS pool becomes required for Review. The S3 destination
remains supported and tested but is no longer the primary path; it is an
optional offsite copy. Immich's staging library is a view over the archive and
never holds the only copy, so ADR-0001's cleanup gate is unaffected: an Immich
scan is not a Delivery and does not contribute to Ready for Cleanup.

Grouping by Ingest does not come free, because the local archive layout mirrors
the Source and accumulates across connections. It requires djpi to create an
album per Ingest through the Immich API, which is the only new code this
decision implies.

Promotion of finished work into the operator's primary Immich library stays
manual until an editing workflow actually exists.
