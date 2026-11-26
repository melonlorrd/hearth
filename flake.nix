{
  description = "Hearth: HA coordinator for Prometheus";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (
        pkgs:
        let
          buildGoModule = pkgs.buildGoModule.override { go = pkgs.go_1_27; };
        in
        {
          default = buildGoModule {
            pname = "hearth";
            version = self.shortRev or "dirty";
            src = self;
            subPackages = [ "cmd/hearth" ];
            # No third-party deps yet. When go.sum appears, set this to
            # pkgs.lib.fakeHash, run `nix build`, and paste the hash it reports.
            vendorHash = null;
            env.CGO_ENABLED = "0";
          };
        }
      );

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_27
            gopls
            prometheus
            protobuf
            protoc-gen-go
          ];
        };
      });

      apps = forAllSystems (
        pkgs:
        let
          test = pkgs.writeShellApplication {
            name = "hearth-test";
            # -race needs cgo, hence the C compiler.
            # prometheus: for contract tests in test/contract.
            runtimeInputs = [
              pkgs.go_1_27
              pkgs.stdenv.cc
              pkgs.prometheus
            ];
            text = ''
              CGO_ENABLED=1 go test -race ./... "$@"
            '';
          };
        in
        {
          test = {
            type = "app";
            program = "${test}/bin/hearth-test";
          };
        }
      );

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
