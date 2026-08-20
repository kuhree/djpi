{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.djpi;
  json = pkgs.formats.json { };
  generatedConfig = json.generate "djpi.json" (
    cfg.settings // { state_directory = cfg.stateDirectory; }
  );
  configPath = if cfg.configFile == null then generatedConfig else cfg.configFile;
  localArchivePaths =
    if cfg.configFile != null then
      [ ]
    else
      map (destination: destination.path) (
        builtins.filter (destination: (destination.type or "") == "local") (
          cfg.settings.destinations or [ ]
        )
      );
in
{
  options.services.djpi = {
    enable = lib.mkEnableOption "the djpi camera ingest station";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage ./nix/package.nix { }";
      description = "djpi package to run.";
    };

    stateDirectory = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/djpi";
      description = "Persistent journal and default archive directory.";
    };

    settings = lib.mkOption {
      inherit (json) type;
      default = {
        poll_interval = "2s";
        status_listen = "127.0.0.1:8787";
        destinations = [
          {
            name = "local-archive";
            type = "local";
            path = "/var/lib/djpi/archive";
          }
        ];
      };
      description = "djpi JSON settings; state_directory is set from stateDirectory.";
    };

    configFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      description = "Existing strict JSON config; overrides generated settings when set.";
    };

    environmentFiles = lib.mkOption {
      type = lib.types.listOf lib.types.path;
      default = [ ];
      description = "Root-managed systemd environment files containing destination credentials.";
    };

    readWritePaths = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Additional destination paths writable by the hardened service.";
    };

    autoMount = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Read-only mount hardware-observed Action 6 and Pocket 3 filesystem LUNs.";
    };

    mountRoot = lib.mkOption {
      type = lib.types.str;
      default = "/run/djpi/media";
      description = "Runtime directory for read-only camera mounts.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = lib.hasPrefix "/" cfg.stateDirectory && lib.hasPrefix "/" cfg.mountRoot;
        message = "services.djpi stateDirectory and mountRoot must be absolute paths";
      }
    ];

    users.groups.djpi = { };
    users.users.djpi = {
      isSystemUser = true;
      group = "djpi";
      description = "djpi camera station";
    };

    environment.systemPackages = [ cfg.package ];
    environment.etc."djpi/config.json".source = configPath;

    systemd.tmpfiles.rules = [
      "d ${cfg.stateDirectory} 0700 djpi djpi -"
    ]
    ++ lib.optionals cfg.autoMount [ "d ${cfg.mountRoot} 0750 djpi djpi -" ]
    ++ map (path: "d ${path} 0750 djpi djpi -") localArchivePaths;

    # IDs are limited to repository hardware observations. ID_FS_USAGE avoids
    # attempting to mount the cameras' second, zero-size File-Stor Gadget LUN.
    services.udev.extraRules = lib.mkIf cfg.autoMount ''
      ACTION=="add", SUBSYSTEM=="block", ENV{DEVTYPE}=="disk", ENV{ID_BUS}=="usb", ENV{ID_VENDOR_ID}=="2ca3", ENV{ID_MODEL_ID}=="8002", ENV{ID_FS_USAGE}=="filesystem", ENV{ID_FS_TYPE}=="exfat", TAG+="systemd", ENV{SYSTEMD_WANTS}+="djpi-mount@%k.service"
      ACTION=="add", SUBSYSTEM=="block", ENV{DEVTYPE}=="disk", ENV{ID_BUS}=="usb", ENV{ID_VENDOR_ID}=="2ca3", ENV{ID_MODEL_ID}=="0020", ENV{ID_FS_USAGE}=="filesystem", ENV{ID_FS_TYPE}=="exfat", TAG+="systemd", ENV{SYSTEMD_WANTS}+="djpi-mount@%k.service"
    '';

    systemd.services."djpi-mount@" = lib.mkIf cfg.autoMount {
      description = "Read-only mount for DJI camera storage %I";
      after = [ "dev-%i.device" ];
      bindsTo = [ "dev-%i.device" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = false;
      };
      path = [
        pkgs.coreutils
        pkgs.systemd
        pkgs.util-linux
      ];
      scriptArgs = "%i";
      script = ''
        set -eu
        device="/dev/$1"
        target="${cfg.mountRoot}/$1"
        if findmnt --source "$device" >/dev/null 2>&1; then
          exit 0
        fi
        install -d -m 0750 -o djpi -g djpi "$target"
        systemd-mount --no-block --collect \
          --options=ro,nosuid,nodev,noexec,noatime,uid=djpi,gid=djpi,fmask=0137,dmask=0027 \
          "$device" "$target"
      '';
    };

    systemd.services.djpi = {
      description = "djpi DJI camera ingest station";
      wantedBy = [ "multi-user.target" ];
      after = [
        "local-fs.target"
        "network-online.target"
      ];
      wants = [ "network-online.target" ];
      path = [ pkgs.util-linux ];
      environment = {
        HOME = cfg.stateDirectory;
      };
      serviceConfig = {
        Type = "simple";
        User = "djpi";
        Group = "djpi";
        ExecStart = "${lib.getExe cfg.package} serve --config /etc/djpi/config.json";
        EnvironmentFile = cfg.environmentFiles;
        Restart = "always";
        RestartSec = "2s";
        UMask = "0027";

        CapabilityBoundingSet = "";
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProtectSystem = "strict";
        ProcSubset = "pid";
        ReadOnlyPaths = lib.optional cfg.autoMount cfg.mountRoot;
        ReadWritePaths = lib.unique ([ cfg.stateDirectory ] ++ localArchivePaths ++ cfg.readWritePaths);
        RestrictAddressFamilies = [
          "AF_UNIX"
          "AF_INET"
          "AF_INET6"
        ];
        RestrictRealtime = true;
        SystemCallArchitectures = "native";
      };
    };
  };
}
