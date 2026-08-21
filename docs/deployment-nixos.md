# NixOS x86_64 deployment

The repository is a standalone `x86_64-linux` flake with a
`nixosModules.default` module. The module supplies the `djpi` binary, strict
JSON configuration, an unprivileged hardened systemd service, and optional
read-only camera automounting.

## Import

Add djpi as a flake input and import its module in the x86_64 NAS host or VM. A
local path is appropriate while this repository has no remote; a later Git
input can replace it without changing the module configuration.

```nix
{
  inputs.djpi = {
    url = "path:/path/to/djpi";
    inputs.nixpkgs.follows = "nixpkgs";
  };
}
```

```nix
{
  imports = [ inputs.djpi.nixosModules.default ];

  services.djpi = {
    enable = true;
    auditOnCalendar = "weekly";
    environmentFiles = [ config.age.secrets.djpi-destinations.path ];
    settings = {
      poll_interval = "2s";
      destination_timeout = "6h";
      status_listen = "127.0.0.1:8787";
      destinations = [
        {
          name = "archive";
          type = "local";
          path = "/var/lib/djpi/archive";
        }
        {
          name = "objects";
          type = "s3";
          endpoint = "https://objects.example.com";
          region = "us-east-1";
          bucket = "camera-media";
          prefix = "djpi";
          use_path_style = true;
          upload_concurrency = 4;
          access_key_env = "DJPI_S3_ACCESS_KEY";
          secret_key_env = "DJPI_S3_SECRET_KEY";
        }
        {
          name = "photos";
          type = "immich";
          server_url = "https://photos.example.com/api";
          api_key_env = "DJPI_IMMICH_API_KEY";
        }
      ];
    };
  };
}
```

The root-readable environment file uses systemd `NAME=value` syntax:

```text
DJPI_S3_ACCESS_KEY=...
DJPI_S3_SECRET_KEY=...
DJPI_IMMICH_API_KEY=...
```

Provision the S3 bucket and narrowly scoped upload, head, and object-read
credentials separately; object read is required for byte-level verification.
The Immich key needs asset upload and asset read permissions. Do not put secret
values in Nix settings because they would enter the world-readable Nix store.

## Camera mounting

With `autoMount = true` (the default), udev starts a templated mount unit only
for the hardware-observed Action 6 (`2ca3:8002`) and Pocket 3 (`2ca3:0020`)
USB filesystem LUNs. The rule additionally requires a filesystem-bearing exFAT
disk, so it ignores each camera's observed zero-size second LUN and unrelated
USB drives. It mounts under `/run/djpi/media` with
`ro,nosuid,nodev,noexec,noatime`; djpi itself runs without mount capabilities.

This automount path is [hardware-verified end to end on the Station
`srv-nas-ix`](hardware/action-6-wired-ingest.md): an Action 6 was automounted
read-only and 293 files totaling 8.43 GB were ingested and independently
verified. Set `autoMount = false` if another trusted component provides mounts.

A camera can enumerate in a non-storage USB mode that presents no filesystem,
in which case the rule correctly does not match and no source appears. See
[the Action 6 USB evidence](hardware/action-6-usb.md) for an observed example.

Separately, the [full Pocket 3 local ingest and idempotent second
pass](hardware/pocket-3-wired-ingest.md) succeeded on `nixtop` through its
existing desktop-managed read-write mount. djpi only read that source; this is
not a substitute for proving the module's read-only mount on the target Station.

## Operations

```console
systemctl status djpi
journalctl -u djpi -f
sudo -u djpi djpi status --config /etc/djpi/config.json
sudo -u djpi djpi status --config /etc/djpi/config.json --json
sudo -u djpi djpi audit --config /etc/djpi/config.json
curl --fail http://127.0.0.1:8787/healthz
curl --fail http://127.0.0.1:8787/api/v1/status
```

The audit is read-only but can be I/O intensive because it independently reads
every configured copy. Run it when archive and network load are acceptable. Use
the same service environment when S3 or Immich credentials are configured.
Setting `auditOnCalendar` enables a hardened systemd timer with a randomized
delay of up to 15 minutes; it is disabled by default. The audit runs at idle I/O
priority and reduced CPU priority, while each destination operation retains its
configured timeout. Audit failures make the oneshot `djpi-audit.service` fail
visibly without stopping the ingest daemon.

The journal and default local archive live under `/var/lib/djpi`. The service
restarts after failures, rechecks durable destination evidence after a process
restart or camera reconnect, and emits newline-delimited JSON events to the
system journal. Destination failures include a `retry_at` timestamp and use
bounded exponential backoff, while systemd still restarts the process after a
fatal service or listener error.

Local delivery checks free space before staging every item and retains at least
16 MiB or 1% per-file headroom, whichever is larger. Capacity failures are
journaled and visible in status; size the configured archive for the whole
camera rather than relying on this final guard.

An `ingest_complete` event is emitted only after a second complete Source
inventory matches the delivered snapshot. `source_changed` means the daemon
adopted newer metadata and will reconcile it; post-ingest inventory failures
remain blocked and retry rather than claiming the camera is ready.

`djpi status` reports attachment evidence, aggregate verified/failed delivery
records, missing/pending deliveries, the latest verification time, bounded
item-level failure details, and per-snapshot readiness for every currently
configured destination. Its cleanup state is always explicitly unauthorized
because cleanup is not implemented.

The daemon's HTTP status additionally reports the current lifecycle event,
camera, source snapshot size, current item/destination phase, failure message,
and `retry_at` deadline. This remains available while an ingest call is busy
because HTTP serving and the station loop have coordinated lifecycles but run
concurrently.

The module enables the read-only HTTP status page on loopback by default. It
does not open a firewall port. Keep it on loopback and use a trusted VPN or an
authenticated reverse proxy for remote access; binding a non-loopback address
directly exposes operational metadata without application-level authentication.
