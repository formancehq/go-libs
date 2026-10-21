{
  description = "A Nix-flake-based Go 1.27.3 development environment";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2511";
    nixpkgs-unstable.url = "https://flakehub.com/f/NixOS/nixpkgs/0.1";

    go-tooling = {
      url = "github:NixOS/nixpkgs/8cec5ef1131078ea76689e84c362b0ed6a1fb6a1";
      flake = false;
    };

    nur = {
      url = "github:nix-community/NUR";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, nixpkgs-unstable, nur, go-tooling }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      forEachSupportedSystem = f:
        nixpkgs.lib.genAttrs supportedSystems (system:
          let
            pkgs = import nixpkgs {
              inherit system;
            };
            pkgs-unstable = import nixpkgs-unstable {
              inherit system;
            };
          in
          f { pkgs = pkgs; pkgs-unstable = pkgs-unstable; system = system; }
        );
    in
    {

      devShells = forEachSupportedSystem ({ pkgs, pkgs-unstable, system }:
        let
          # Import recipes only: keep the existing package set and Darwin support.
          go = pkgs.callPackage "${go-tooling}/pkgs/development/compilers/go/1.27.nix" {
            buildGo127Module = buildGoModule;
          };
          buildGoModule = pkgs.buildGoModule.override { inherit go; };
          go27 = pkgs.callPackage "${go-tooling}/pkgs/development/compilers/go/1.27.nix" {
            buildGo127Module = pkgs.buildGoModule.override { go = go27; };
          };
          golangci-lint = pkgs.callPackage "${go-tooling}/pkgs/by-name/go/golangci-lint/package.nix" {
            buildGo127Module = pkgs.buildGoModule.override { go = go27; };
          };
          mockgen = pkgs.mockgen.override { inherit buildGoModule; };
          stablePackages = with pkgs; [
            ginkgo
            gomarkdoc
            goperf
            gotools
            just
          ];
          unstablePackages = [];
          otherPackages = [ go golangci-lint mockgen ];
        in
        {
          default = pkgs.mkShell {
            GOROOT = "${go}/share/go";
            GOTOOLCHAIN = "local";
            packages = stablePackages ++ unstablePackages ++ otherPackages;
          };
        }
      );
    };
}
