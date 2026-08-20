# x86_64 Linux deployment

djpi targets an always-running x86_64 Linux Station. The Go service needs Linux
USB sysfs evidence, `lsblk`, `findmnt`, an already mounted Camera Source,
persistent journal storage, and access to its configured Destinations.

## Recommended placement

| Placement | Plug-and-ingest fit | Isolation | Recommendation |
| --- | --- | --- | --- |
| NAS host service | Best when the NAS exposes normal Linux udev/systemd | Process sandbox | Prefer when host packages and USB rules are manageable |
| x86_64 Linux VM | Excellent with whole-USB passthrough | Strong | Preferred fallback for appliance-like NAS operating systems |
| Container | Possible, but mount/device propagation is host-specific | Medium | Use only after proving the exact NAS runtime evidence |

For a VM, pass the whole DJI USB device or a dedicated physical USB controller
through to the guest. Whole-device passthrough preserves enumeration, both
observed storage LUNs, and udev events. Run mounting and djpi inside the guest;
the [NixOS module](deployment-nixos.md) supplies the complete service path.

For a native NAS installation, let the host udev layer mount only the
hardware-observed filesystem LUN read-only, then run `djpi serve` as a dedicated
unprivileged account. The NixOS module is one implementation of this split.

## Container boundary

The current automatic detector correlates all of the following evidence before
it accepts a Source:

- `/sys/bus/usb/devices` descriptors and interfaces;
- Linux block-device serial, transport, filesystem, and size evidence;
- the container's own mount table.

A media-directory bind mount alone is therefore intentionally insufficient.
The container would also need read-only host USB/block evidence and propagation
of the host's read-only camera mount into the same mount namespace. Exact
Docker, Podman, and NAS appliance behavior differs, and broad `--privileged`
access is not an acceptable default. Until a specific container runtime is
hardware-verified, use a VM or native service for unattended plug-and-ingest.

Regardless of placement, persist the journal and local archive separately from
the Camera Source. Never make the camera mount writable for djpi. S3 and Immich
credentials belong in the host or guest secret mechanism, not the JSON file or
container image.
