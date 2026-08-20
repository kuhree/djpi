{
  description = "DJI camera ingest station";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      overlays.default = final: _prev: {
        djpi = final.callPackage ./nix/package.nix { };
      };

      packages = forAllSystems (system: {
        default = nixpkgs.legacyPackages.${system}.callPackage ./nix/package.nix { };
      });

      nixosModules.default = import ./nix/module.nix;

      checks = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          evaluated = nixpkgs.lib.nixosSystem {
            inherit system;
            modules = [
              self.nixosModules.default
              {
                services.djpi.enable = true;
                services.djpi.auditOnCalendar = "weekly";
                system.stateVersion = "26.05";
              }
            ];
          };
        in
        {
          package = self.packages.${system}.default;
          module = pkgs.writeText "djpi-module-evaluation.json" (
            builtins.toJSON {
              execStart = evaluated.config.systemd.services.djpi.serviceConfig.ExecStart;
              mountScript = evaluated.config.systemd.services."djpi-mount@".script;
              auditExecStart = evaluated.config.systemd.services.djpi-audit.serviceConfig.ExecStart;
              auditSchedule = evaluated.config.systemd.timers.djpi-audit.timerConfig.OnCalendar;
              auditTimeout = evaluated.config.systemd.services.djpi-audit.serviceConfig.TimeoutStartSec;
              udevRules = evaluated.config.services.udev.extraRules;
            }
          );
        }
      );
    };
}
