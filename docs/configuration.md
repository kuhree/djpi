# Station configuration

`djpi serve --config /etc/djpi.json` runs the wired ingest station. The file is
strict JSON: unknown fields, duplicate destination names, incomplete
credentials, and destination-specific fields on the wrong type are rejected.

## Local archive

```json
{
  "state_directory": "/var/lib/djpi",
  "poll_interval": "2s",
  "destinations": [
    {
      "name": "local-archive",
      "type": "local",
      "path": "/srv/djpi/archive"
    }
  ]
}
```

The archive must not be inside the mounted camera source. Files are stored at
`<camera-id>/sha256/<digest>/<camera-relative-path>` so two files with the same
camera path but different contents cannot overwrite each other.

## S3-compatible archive

```json
{
  "state_directory": "/var/lib/djpi",
  "poll_interval": "2s",
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
checks that metadata and object length using `HeadObject`; SDK upload checksums
protect the transfer itself. Multipart uploads use 16 MiB parts, two workers,
and abort incomplete multipart state on failure to keep Raspberry Pi memory and
remote garbage bounded.

Multiple destinations may be listed. Every item must verify at every listed
destination before the station emits `ingest_complete`. Failures remain in the
durable journal and are retried while the same camera stays attached or after a
reconnect.

There is deliberately no cleanup setting. Delivery verification and camera
cleanup authorization are separate safety mechanisms.
