{
  buildGoModule,
  lib,
}:
buildGoModule rec {
  pname = "djpi";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
    ];
  };
  vendorHash = "sha256-eJG52DOyg7Fxm8BtReL+9JEHOYXDDhovJC1b2bRDztE=";

  subPackages = [ "cmd/djpi" ];
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
  ];

  meta = {
    description = "Always-running, non-destructive DJI camera ingest station";
    homepage = "https://git.kuhree.com/kuhree.com/djpi";
    license = lib.licenses.mit;
    mainProgram = "djpi";
    platforms = lib.platforms.linux;
  };
}
