# Station configuration

`djpi serve --config /etc/djpi.json` runs the wired ingest station. The file is
strict JSON: unknown fields, duplicate destination names, incomplete
credentials, and destination-specific fields on the wrong type are rejected.

## Local archive

```json
{
  "state_directory": "/var/lib/djpi",
  "poll_interval": "2s",
  "destination_timeout": "6h",
  "status_listen": "127.0.0.1:8787",
  "destinations": [
    {
      "name": "local-archive",
      "type": "local",
      "path": "/srv/djpi/archive"
    }
  ]
}
```

`status_listen` is optional. When set, the long-running service exposes a
read-only operator page at `/`, JSON evidence at `/api/v1/status`, liveness at
`/healthz`, and readiness at `/readyz`. The recommended loopback address keeps
the unauthenticated endpoint local; expose it through a trusted VPN or an
authenticated reverse proxy if remote access is required. Omitting the field
disables HTTP entirely. There are no action, upload, cleanup, or control routes.
The API combines live daemon lifecycle state (`idle`, `ingesting`, `retrying`,
`reconciling`, `blocked`, or `complete`) with the durable ingest journal. During ingest it also
reports the current item number/path, destination, and phase (`source-check`,
`delivering`, `verifying`, `verified`, `skipped`, or `failed`). These progress
fields are observational and never substitute for journaled verification. The
CLI `status` command runs in a separate process and therefore reports durable
and hardware evidence without claiming to know the daemon's in-memory lifecycle.
Both views calculate pending deliveries against the current destination list,
report the most recent successful verification time, and include up to 20
deterministically sorted journaled failure details. A failed delivery remains
pending until it independently verifies.

`destination_timeout` bounds each individual delivery or verification call.
It defaults to six hours when omitted, which accommodates large video objects
while ensuring a stalled filesystem or network request cannot hold the station
forever. Cancellation is journaled as a failed delivery and enters the normal
retry backoff.

Completion also requires a post-delivery re-inventory with the same metadata
evidence ID. If the source changed, the daemon reports `reconciling` and ingests
the newly observed complete snapshot; if the re-inventory fails or is incomplete,
completion remains blocked. This protects files added or changed during a long
delivery without treating metadata identity as content or cleanup proof.

The archive must not be inside the mounted camera source. Files are stored at
`<camera-id>/sha256/<digest>/<camera-relative-path>` so two files with the same
camera path but different contents cannot overwrite each other.

Before staging each file, djpi reads the destination filesystem's available
capacity and requires the file size plus headroom: the larger of 16 MiB or 1%
of that file. If capacity cannot be measured or is insufficient, delivery fails
before any media bytes enter staging and follows the normal journal/backoff
path. This is a per-file guard, so operators should still size the archive for
the complete camera plus normal filesystem and retention overhead.

Each local archive uses an advisory `.djpi.lock` while delivering. After it
holds that lock, djpi removes only regular `.staging/.djpi-*.tmp` files created
by an earlier interrupted djpi process, then synchronizes the staging
directory. It refuses unexpected non-regular entries and never sweeps other
names. Committed content-addressed objects and camera storage are outside this
reclamation boundary.

## S3-compatible archive

```json
{
  "state_directory": "/var/lib/djpi",
  "poll_interval": "2s",
  "destination_timeout": "6h",
  "status_listen": "127.0.0.1:8787",
  "destinations": [
    {
      "name": "object-store",
      "type": "s3",
      "endpoint": "https://objects.example.com",
      "region": "us-east-1",
      "bucket": "camera-media",
      "prefix": "djpi",
      "use_path_style": true,
      "upload_concurrency": 4,
      "access_key_env": "DJPI_S3_ACCESS_KEY",
      "secret_key_env": "DJPI_S3_SECRET_KEY"
    }
  ]
}
```

`endpoint` is optional for AWS S3. `use_path_style` is commonly needed by
S3-compatible services. When the key environment names are omitted, the AWS
SDK default credential chain is used (for example, an attached instance role).
When they are present, both named variables must contain values before the
station starts. Credential values never belong in this JSON file and are not
written to station events or the ingest journal. Temporary session credentials
can additionally name `session_token_env`.

Each object carries `djpi-sha256` and `djpi-size` metadata. After upload, djpi
checks that metadata and object length using `HeadObject`, then streams the
stored object back through SHA-256 with `GetObject`. The read-back is deliberate:
metadata and transport checksums alone do not independently prove that the
remote bytes equal the inventoried source. Multipart uploads use 16 MiB parts,
four workers by default, and abort incomplete multipart state on failure.
`upload_concurrency` may be set from 1 through 32 per S3 Destination to match
Station CPU, memory, disk, and network capacity.

Multiple destinations may be listed. Every item must verify at every listed
destination before the station emits `ingest_complete`. Failures remain in the
durable journal and are retried while the same camera stays attached or after a
reconnect. In-process retries back off from 10 seconds to a maximum of 5 minutes;
removal, a new attachment, or success resets that delay.

## Immich catalog

Immich is configured alongside at least one local or S3-compatible archive;
the strict configuration validator rejects Immich as the only preserved copy.

```json
{
  "state_directory": "/var/lib/djpi",
  "destinations": [
    {
      "name": "archive",
      "type": "local",
      "path": "/srv/djpi/archive"
    },
    {
      "name": "photos",
      "type": "immich",
      "server_url": "https://photos.example.com/api",
      "api_key_env": "DJPI_IMMICH_API_KEY"
    }
  ]
}
```

`server_url` is the Immich API base URL, including `/api`. Create an Immich API
key with asset upload and asset read permissions, then expose it through the
named environment variable. djpi computes SHA-1 in addition to its canonical
SHA-256, asks Immich's bulk-upload check whether the content already exists,
streams new assets as multipart data, and finally reads the returned asset back
through the API. The delivery verifies only when the live, non-trashed,
non-offline asset reports the same base64 SHA-1. Its asset ID and checksum are
stored in the journal; the API key is not.

There is deliberately no cleanup setting. Delivery verification and camera
cleanup authorization are separate safety mechanisms.

## Independent destination audit

Run `djpi audit --config /etc/djpi.json` to re-check durable deliveries while
the camera is absent. The audit evaluates every journaled item against every
currently configured destination. It does not read camera storage or modify the
journal. `--json` emits machine-readable output; either form exits nonzero for
pending delivery evidence, invalid journal evidence, destination errors, or byte
mismatches. Each verification uses `destination_timeout` independently.

An empty journal has nothing to audit and succeeds with zero expected items.
Removing a destination from configuration removes it from the audit set; adding
one makes every older item pending until normal ingest delivers and verifies it.
