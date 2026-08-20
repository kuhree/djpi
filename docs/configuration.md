# Station configuration

`djpi serve --config /etc/djpi.json` runs the wired ingest station. The file is
strict JSON: unknown fields, duplicate destination names, incomplete
credentials, and destination-specific fields on the wrong type are rejected.

## Local archive

```json
{
  "state_directory": "/var/lib/djpi",
  "poll_interval": "2s",
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

The archive must not be inside the mounted camera source. Files are stored at
`<camera-id>/sha256/<digest>/<camera-relative-path>` so two files with the same
camera path but different contents cannot overwrite each other.

## S3-compatible archive

```json
{
  "state_directory": "/var/lib/djpi",
  "poll_interval": "2s",
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
two workers, and abort incomplete multipart state on failure to keep Raspberry
Pi memory and remote garbage bounded.

Multiple destinations may be listed. Every item must verify at every listed
destination before the station emits `ingest_complete`. Failures remain in the
durable journal and are retried while the same camera stays attached or after a
reconnect.

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
