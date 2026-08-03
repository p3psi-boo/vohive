{
  description = "VoHive development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true; # Android SDK 构建组件为 unfree 许可
            android_sdk.accept_license = true;
          };
        };
        androidSdk = (pkgs.androidenv.composeAndroidPackages {
          cmdLineToolsVersion = "13.0";
          platformToolsVersion = "36.0.2";
          platformVersions = [ "37" ];
          buildToolsVersions = [ "36.0.0" ];
          includeEmulator = false;
          includeNDK = false;
          includeSystemImages = false;
          includeSources = false;
        }).androidsdk;
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

        # Android Agent 编译环境：SDK 37 + JDK 17 + adb
        devShells.android = pkgs.mkShell {
          packages = with pkgs; [ jdk17 androidSdk ];

          env = {
            ANDROID_HOME = "${androidSdk}/libexec/android-sdk";
            ANDROID_SDK_ROOT = "${androidSdk}/libexec/android-sdk";
          };

          shellHook = ''
            echo "Android devShell: SDK $ANDROID_HOME, JDK $(java -version 2>&1 | head -1)"
            echo "Build: cd android-agent && ./gradlew :app:assembleDebug"
          '';
        };

        formatter = pkgs.nixpkgs-fmt;
      });
}
