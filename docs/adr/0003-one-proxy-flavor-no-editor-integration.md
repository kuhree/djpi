---
status: superseded by ADR-0004
---

# Generate one Proxy flavor and keep editors out of the codebase

Superseded before implementation. This ADR assumed djpi would build the Review
surface itself and therefore had to generate its own viewable derivatives with
ffmpeg. ADR-0004 instead makes an Immich staging library the Review surface,
which generates thumbnails and transcodes itself, so djpi generates nothing and
has no ffmpeg dependency. The term Proxy has been removed from CONTEXT.md.

The reasoning worth keeping: djpi should hold no editor-specific knowledge.
Getting originals into an editing workflow is a Destination concern, and any
format conversion an editor wants is configuration on that Destination rather
than a code path in djpi. That still holds under ADR-0004.
