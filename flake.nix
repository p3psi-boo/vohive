{
  description = "VoHive development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_26
            nodejs_24
            gnumake
            git
            gcc
            pkg-config
            upx
            jq
            curl
          ];

          env = {
            GOTOOLCHAIN = "auto";
            GOWORK = "off";
            CGO_ENABLED = "0";
            NPM_CONFIG_FUND = "false";
            NPM_CONFIG_AUDIT = "false";
          };

          shellHook = ''
            echo "VoHive devShell: Go $(go version | awk '{print $3}'), Node $(node --version)"
            echo "Common targets: make verify, make build-local, make build-all ENABLE_UPX=0"
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
