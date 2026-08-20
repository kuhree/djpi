# NixOS Raspberry Pi deployment

The repository is a standalone flake with `x86_64-linux` and `aarch64-linux`
packages and a `nixosModules.default` module. The module supplies the `djpi`
binary, strict JSON configuration, an unprivileged hardened systemd service,
and optional read-only camera automounting.

## Import

Add djpi as a flake input and import its module in the Raspberry Pi system. A
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

This automount path has been evaluated as a NixOS module and its Pocket 3 udev
properties were matched on `nixtop`. The actual read-only mount and ingest must
still be exercised on the Raspberry Pi before calling deployment complete.
Set `autoMount = false` if another trusted component provides mounts.

## Operations

```console
systemctl status djpi
journalctl -u djpi -f
sudo -u djpi djpi status --config /etc/djpi/config.json
sudo -u djpi djpi status --config /etc/djpi/config.json --json
curl --fail http://127.0.0.1:8787/healthz
curl --fail http://127.0.0.1:8787/api/v1/status
```

The journal and default local archive live under `/var/lib/djpi`. The service
restarts after failures, rechecks durable destination evidence after a process
restart or camera reconnect, and emits newline-delimited JSON events to the
system journal. Destination failures include a `retry_at` timestamp and use
bounded exponential backoff, while systemd still restarts the process after a
fatal service or listener error.

`djpi status` reports attachment evidence, aggregate verified/failed delivery
records, and per-snapshot readiness for every currently configured destination.
Its cleanup state is always explicitly unauthorized because cleanup is not
implemented.

The daemon's HTTP status additionally reports the current lifecycle event,
camera, source snapshot size, current item/destination phase, failure message,
and `retry_at` deadline. This remains available while an ingest call is busy
because HTTP serving and the station loop have coordinated lifecycles but run
concurrently.

The module enables the read-only HTTP status page on loopback by default. It
does not open a firewall port. Keep it on loopback and use a trusted VPN or an
authenticated reverse proxy for remote access; binding a non-loopback address
directly exposes operational metadata without application-level authentication.
