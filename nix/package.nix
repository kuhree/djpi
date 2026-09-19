{
  buildGoModule,
  lib,
}:
buildGoModule rec {
  pname = "djpi";
  version = lib.strings.trim (builtins.readFile ../VERSION);

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

  postInstall = ''
    install -Dm644 ${../THIRD_PARTY_NOTICES.md} \
      $out/share/doc/djpi/THIRD_PARTY_NOTICES.md
  '';

  meta = {
    description = "Always-running, non-destructive DJI camera ingest station";
    homepage = "https://git.kuhree.com/kuhree.com/platform.djpi";
    license = lib.licenses.mit;
    mainProgram = "djpi";
    platforms = [ "x86_64-linux" ];
  };
}
